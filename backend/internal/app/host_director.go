package app

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// Access director fields under App.mu. Each in-flight tick owns its director
// pointer until invalidation; late callbacks cannot change a replacement.
type hostDirector struct {
	sid         string
	cancel      context.CancelFunc
	status      HostStatus
	failures    int
	lastViewer  time.Time
	completed   time.Time
	submittedID string
	expiresAt   time.Time
}

func hostPause(sw, quiet bool, pick func(int) int) time.Duration {
	lo, hi := 2, 5
	if sw {
		lo, hi = 5, 10
		if quiet {
			lo, hi = 10, 20
		}
	}
	return time.Duration(lo+pick(hi-lo+1)) * time.Second
}
func (a *App) directorLocked(s object) *hostDirector {
	if a.closedLocked() {
		return nil
	}
	sid := str(s, "id")
	if a.director == nil || a.director.sid != sid {
		if a.director != nil && a.director.cancel != nil {
			a.director.cancel()
		}
		created, e := time.Parse(time.RFC3339Nano, str(s, "createdAt"))
		if e != nil {
			created = a.c.Clock()
		}
		a.director = &hostDirector{sid: sid, lastViewer: created, status: HostStatus{Mode: "companion", State: "waiting"}}
	}
	return a.director
}
func (a *App) invalidateHostLocked(sid string) {
	d := a.director
	if d == nil || d.sid != sid {
		return
	}
	if d.cancel != nil {
		d.cancel()
	}
	a.director = &hostDirector{sid: sid, lastViewer: d.lastViewer, completed: d.completed, submittedID: d.submittedID, expiresAt: d.expiresAt, status: HostStatus{Mode: "companion", State: "waiting", NextEligibleAt: d.status.NextEligibleAt}}
}
func (a *App) InvalidateHostDraft(sid, reason string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.current()
	if str(s, "id") != sid || s["hostMode"] != "companion" {
		return
	}
	a.directorLocked(s)
	a.invalidateHostLocked(sid)
	if reason == "viewer" && a.director != nil && a.director.sid == sid {
		a.director.lastViewer = a.c.Clock()
	}
}
func (a *App) hostStatusLocked(s object) HostStatus {
	mode := str(s, "hostMode")
	if mode == "" {
		mode = "fixed"
	}
	status := HostStatus{Mode: mode, State: "waiting"}
	if mode == "companion" && a.director != nil && a.director.sid == str(s, "id") {
		status = a.director.status
	}
	if s["hostEnabled"] != true || s["status"] != "live" {
		status.State = "paused"
		status.NextEligibleAt = nil
	}
	return status
}
func (a *App) CompanionStatus(sid string) HostStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.current()
	if str(s, "id") != sid {
		return HostStatus{State: "paused"}
	}
	return a.hostStatusLocked(s)
}
func (a *App) selectHostAngle(in *HostInput) error {
	currentID := ""
	if in.CurrentTopic != nil {
		currentID = in.CurrentTopic.ID
	}
	ts, e := a.queryTopics(in.AvatarID,
		"AND (state='ready' OR (state='active' AND id=?)) AND "+selectableHostTopic+
			" ORDER BY (state='active') DESC,created_at DESC,rowid DESC", currentID)
	if e != nil {
		return e
	}
	for _, t := range ts {
		for _, angle := range t.Angles {
			var used bool
			e = a.db.QueryRow("SELECT EXISTS(SELECT 1 FROM host_tasks WHERE session_id=? AND topic_id=? AND angle_id=? AND state IN ('pending','completed','unknown') UNION ALL SELECT 1 FROM host_recent_keys WHERE avatar_id=? AND key=? AND seen_at>=?)", in.SessionID, t.ID, angle.ID, in.AvatarID, t.Key+"/"+speechKey(angle.Text), in.ServerTime.Add(-7*24*time.Hour).Unix()).Scan(&used)
			if e != nil {
				return e
			}
			if !used {
				in.CurrentTopic = &t
				in.CurrentAngleID = angle.ID
				return nil
			}
		}
	}
	return sql.ErrNoRows
}

type hostWorkerState struct {
	SessionID, Status, RelayState string
	Speaking                      bool
	QueueDepth                    int
	LastTaskCompletedAt           float64
}

func (a *App) hostWorker(ctx context.Context, sid string) (hostWorkerState, error) {
	var w hostWorkerState
	e := a.worker(ctx, "GET", "/status", nil, &w)
	if e == nil && w.SessionID == sid && w.Status == "error" {
		e = a.markWorkerFailed(sid)
	}
	if e == nil && (w.SessionID != sid || w.Status != "live") {
		e = errStale
	}
	return w, e
}
func (a *App) pollPlayed(ctx context.Context, sid string) error {
	seq, e := a.PlaybackCursor(sid)
	if e != nil {
		return e
	}
	var page TaskResultsPage
	if e = a.worker(ctx, "GET", fmt.Sprintf("/sessions/%s/task-results?after=%d", sid, seq), nil, &page); e != nil {
		return e
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if str(a.current(), "id") != sid {
		return errStale
	}
	return a.ApplyTaskResults(sid, page)
}
func (a *App) CompanionTick(parent context.Context, sid string) (err error) {
	a.mu.Lock()
	s := a.current()
	if a.closedLocked() || str(s, "id") != sid || s["status"] != "live" || s["hostMode"] != "companion" {
		a.mu.Unlock()
		return nil
	}
	d := a.directorLocked(s)
	if a.hostTickActive {
		a.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	d.cancel = cancel
	a.hostTickActive = true
	a.mu.Unlock()
	defer func() {
		cancel()
		a.mu.Lock()
		defer a.mu.Unlock()
		a.hostTickActive = false
		if a.director != d {
			return
		}
		d.cancel = nil
		if err != nil && parent.Err() == nil {
			at := a.c.Clock().Add(generationBackoff(d.failures))
			d.failures++
			d.status.NextEligibleAt = &at
			d.status.State = "degraded"
			d.status.LastError = "主持暂时不可用，稍后自动重试"
		}
	}()
	receiptErr := a.pollPlayed(ctx, sid)
	state, e := a.hostWorker(ctx, sid)
	if e != nil {
		return e
	}
	if receiptErr != nil {
		return receiptErr
	}
	if s["hostEnabled"] != true {
		return nil
	}
	a.mu.Lock()
	if a.replies != nil && a.replies.sid == sid && a.replies.active || a.director != d || d.status.NextEligibleAt != nil && a.c.Clock().Before(*d.status.NextEligibleAt) {
		a.mu.Unlock()
		return nil
	}
	a.mu.Unlock()
	if state.Speaking || state.QueueDepth > 0 {
		a.mu.Lock()
		d.status.State = "waiting"
		if state.Speaking {
			d.status.State = "speaking"
		}
		a.mu.Unlock()
		return nil
	}
	a.mu.Lock()
	if d.submittedID != "" && !a.c.Clock().Before(d.expiresAt) {
		_, e = a.db.Exec("UPDATE host_tasks SET state='unknown' WHERE session_id=? AND task_id=? AND state='pending'", sid, d.submittedID)
		if e == nil {
			d.submittedID = ""
		}
	}
	a.mu.Unlock()
	if e != nil {
		return e
	}
	var pending bool
	if e = a.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM host_tasks WHERE session_id=? AND source='host' AND state='pending')", sid).Scan(&pending); e != nil || pending {
		return e
	}
	in, e := a.LoadHostContext(str(s, "avatarId"), sid)
	if e != nil {
		return e
	}
	a.mu.Lock()
	if a.replies != nil && a.replies.sid == sid {
		in.ViewerTurns = append([]ViewerTurn(nil), a.replies.history...)
	}
	a.mu.Unlock()
	// Set each pause once, measured from Worker completion even if polling is late.
	completed := time.UnixMilli(int64(state.LastTaskCompletedAt * 1000))
	if state.LastTaskCompletedAt > 0 {
		a.mu.Lock()
		if completed.After(d.completed) {
			sw := in.CurrentTopic != nil && in.CurrentTopic.State == "done"
			pick := a.c.HostRandom
			if pick == nil {
				pick = rand.Intn
			}
			at := completed.Add(hostPause(sw, a.c.Clock().Sub(d.lastViewer) >= 5*time.Minute, pick))
			d.completed = completed
			d.status.NextEligibleAt = &at
			d.status.State = "waiting"
		}
		wait := d.status.NextEligibleAt != nil && a.c.Clock().Before(*d.status.NextEligibleAt)
		a.mu.Unlock()
		if wait {
			return nil
		}
	}
	a.mu.Lock()
	d.status.State = "preparing"
	d.status.NextEligibleAt = nil
	a.mu.Unlock()
	_, reserveErr := a.BuildTopics(ctx, in)
	if e = a.selectHostAngle(&in); e != nil {
		if reserveErr != nil {
			return reserveErr
		}
		return e
	}
	a.mu.Lock()
	d.status.TopicTitle = in.CurrentTopic.Title
	a.mu.Unlock()
	draft, e := a.BuildHostDraft(ctx, in)
	if e != nil {
		return e
	}
	if strings.ContainsAny(draft.Text, "?？") {
		n := 0
		for i := len(in.SpokenTurns) - 1; i >= 0 && n < 2; i-- {
			turn := in.SpokenTurns[i]
			if turn.Source == "host" {
				n++
				if strings.ContainsAny(turn.Text, "?？") {
					return errGeneration
				}
			}
		}
	}
	state, e = a.hostWorker(ctx, sid)
	if e != nil {
		return e
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.director != d || ctx.Err() != nil || a.current()["hostEnabled"] != true || !a.validHostInput(in) {
		return nil
	}
	d.status.State = "waiting"
	if state.Speaking || state.QueueDepth > 0 || time.UnixMilli(int64(state.LastTaskCompletedAt*1000)).After(completed) {
		return nil
	}
	if e = a.RegisterPlayback(sid, draft.TaskID, "host", draft.TopicID, draft.AngleID); e != nil {
		return e
	}
	if e = a.task(s, draft.TaskID, "host", draft.Text); e != nil {
		_, markErr := a.db.Exec("UPDATE host_tasks SET state='unknown' WHERE session_id=? AND task_id=? AND state='pending'", sid, draft.TaskID)
		if markErr != nil {
			return markErr
		}
		return e
	}
	d.submittedID = draft.TaskID
	d.expiresAt = a.c.Clock().Add(120 * time.Second)
	d.failures = 0
	d.status.LastError = ""
	return nil
}
