package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
)

func migrateCollectorEvents(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	const schema = `CREATE TABLE IF NOT EXISTS events(session_id TEXT,id TEXT,state TEXT NOT NULL DEFAULT 'done' CHECK(state IN ('pending','retry','done')),first_arrival INTEGER,fixed_hash TEXT,PRIMARY KEY(session_id,id))`
	if _, err = tx.Exec(schema); err != nil {
		return err
	}
	var hasArrival, hasState bool
	if err = tx.QueryRow("SELECT COUNT(*) FILTER (WHERE name='first_arrival'),COUNT(*) FILTER (WHERE name='state') FROM pragma_table_info('events')").Scan(&hasArrival, &hasState); err != nil {
		return err
	}
	if !hasArrival {
		// The old CHECK constraint admitted only pending/done. Preserve every ID,
		// including databases predating the state column, in the new schema.
		if _, err = tx.Exec("ALTER TABLE events RENAME TO collector_events_legacy"); err != nil {
			return err
		}
		if _, err = tx.Exec(schema); err != nil {
			return err
		}
		state := "'done'"
		if hasState {
			state = "state"
		}
		if _, err = tx.Exec("INSERT INTO events(session_id,id,state) SELECT session_id,id," + state + " FROM collector_events_legacy"); err != nil {
			return err
		}
		if _, err = tx.Exec("DROP TABLE collector_events_legacy"); err != nil {
			return err
		}
	}
	// Reservations lost on restart become claimable; retain their original age.
	if _, err = tx.Exec("UPDATE events SET state='retry' WHERE state='pending'"); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) collect(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		fail(w, 405, "Method not allowed")
		return
	}
	var b struct {
		SessionID string `json:"sessionId"`
		Platform  string `json:"platform"`
		Events    []struct {
			ID       string `json:"id"`
			Kind     string `json:"kind"`
			Nickname string `json:"nickname"`
			Text     string `json:"text"`
		} `json:"events"`
	}
	if !decode(w, r, &b) {
		return
	}
	if len(b.Events) > 100 {
		fail(w, 400, "Batch exceeds 100 events")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.current()
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if s == nil || s["status"] != "live" || s["id"] != b.SessionID || str(s, "collectorHash") == "" || !eq(hash(token), str(s, "collectorHash")) {
		fail(w, 401, "Expired collector token")
		return
	}
	if s["platform"] != b.Platform {
		fail(w, 400, "Platform mismatch")
		return
	}
	for _, e := range b.Events {
		if e.ID == "" || len(e.ID) > 200 || len(e.Text) > 10000 || len(e.Nickname) > 500 || !enum(e.Kind, "comment", "enter", "gift", "like") {
			fail(w, 400, "Invalid event")
			return
		}
	}
	var count int
	if a.db.QueryRow("SELECT COUNT(*) FROM events WHERE session_id=?", s["id"]).Scan(&count) != nil {
		fail(w, 500, "Storage failure")
		return
	}
	newIDs := map[string]bool{}
	for _, e := range b.Events {
		var existing int
		if err := a.db.QueryRow("SELECT COUNT(*) FROM events WHERE session_id=? AND id=?", s["id"], e.ID).Scan(&existing); err != nil {
			fail(w, 500, "Storage failure")
			return
		}
		if existing == 0 {
			newIDs[e.ID] = true
		}
	}
	if count+len(newIDs) > 10000 {
		fail(w, 409, "Session event capacity reached; stop and restart session")
		return
	}
	accepted := 0
	arrivals := a.c.Clock()
	waiting := map[string]<-chan bool{}
	outcomes := map[string]string{}
	for _, e := range b.Events {
		outcomes[e.ID] = "retry"
	}
	for _, e := range b.Events {
		current := a.current()
		if current == nil || current["id"] != s["id"] || current["status"] != "live" {
			break
		}
		var state string
		var arrival sql.NullInt64
		var fixedHash sql.NullString
		err := a.db.QueryRow("SELECT state,first_arrival,fixed_hash FROM events WHERE session_id=? AND id=?", s["id"], e.ID).Scan(&state, &arrival, &fixedHash)
		if err != nil && err != sql.ErrNoRows {
			fail(w, 500, "Storage failure")
			return
		}
		switch state {
		case "done":
			outcomes[e.ID] = "processed"
			continue
		case "pending":
			continue
		case "retry":
			_, err = a.db.Exec("UPDATE events SET state='pending',first_arrival=COALESCE(first_arrival,?) WHERE session_id=? AND id=? AND state='retry'", arrivals.UnixNano(), s["id"], e.ID)
		default:
			_, err = a.db.Exec("INSERT INTO events(session_id,id,state,first_arrival) VALUES(?,?,'pending',?)", s["id"], e.ID, arrivals.UnixNano())
		}
		if err != nil {
			continue
		}
		arrived := arrivals
		if arrival.Valid {
			arrived = time.Unix(0, arrival.Int64)
		}
		kind := e.Kind
		if kind == "enter" {
			kind = "welcome"
		}
		text := ""
		for _, script := range a.list("scripts") {
			if script["avatarId"] == s["avatarId"] && script["kind"] == kind {
				text = str(script, "text")
				break
			}
		}
		event := ViewerTurn{Kind: kind, Nickname: e.Nickname, Text: e.Text}
		// A Worker timeout may have delivered the old task. A changed source
		// or edited fixed script must never enqueue replacement content under its ID.
		if state == "retry" {
			var source string
			registeredErr := a.db.QueryRow("SELECT source FROM host_tasks WHERE session_id=? AND task_id=?", s["id"], e.ID).Scan(&source)
			if registeredErr != nil && registeredErr != sql.ErrNoRows {
				a.finishEventLocked(str(s, "id"), e.ID, registeredErr)
				continue
			}
			obsolete := registeredErr == nil && ((source == "fixed" && (text == "" || !fixedHash.Valid || hash(text) != fixedHash.String)) || (source == "interaction" && text != ""))
			if obsolete || kind == "comment" && a.c.Clock().Sub(arrived) > 30*time.Second {
				if a.finishEventLocked(str(s, "id"), e.ID, nil) {
					accepted++
					outcomes[e.ID] = "processed"
				}
				continue
			}
		}
		if state != "retry" {
			a.viewerArrivedLocked(s)
		}
		if text == "" && kind == "comment" {
			waiting[e.ID] = a.queueReplyLocked(s, e.ID, event, arrived)
			continue
		}
		if text != "" {
			_, err = a.db.Exec("UPDATE events SET fixed_hash=? WHERE session_id=? AND id=?", hash(text), s["id"], e.ID)
			if err == nil {
				err = a.submitEventLocked(s, e.ID, "fixed", kind, text)
			}
		}
		if !a.finishEventLocked(str(s, "id"), e.ID, err) {
			continue
		}
		accepted++
		outcomes[e.ID] = "processed"
	}
	a.mu.Unlock()
	for id, ready := range waiting {
		if <-ready {
			accepted++
			outcomes[id] = "processed"
		}
	}
	a.mu.Lock()
	processed, retry := []string{}, []string{}
	seen := map[string]bool{}
	for _, e := range b.Events {
		if seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		if outcomes[e.ID] == "processed" {
			processed = append(processed, e.ID)
		} else {
			retry = append(retry, e.ID)
		}
	}
	write(w, 200, object{"accepted": accepted, "processedIds": processed, "retryIds": retry, "serverTime": a.c.Clock().UTC().Format(time.RFC3339Nano)})
}

type replyJob struct {
	id      string
	event   ViewerTurn
	arrived time.Time
	result  chan bool
}
type replyQueue struct {
	sid     string
	active  bool
	queue   []replyJob
	history []ViewerTurn
}

func (a *App) viewerArrivedLocked(s object) {
	if s["hostMode"] == "companion" {
		a.directorLocked(s)
		a.invalidateHostLocked(str(s, "id"))
		a.director.lastViewer = a.c.Clock()
	}
}
func (a *App) OnViewerTurn(sessionID string, event ViewerTurn) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s := a.current(); str(s, "id") == sessionID && str(s, "status") == "live" {
		a.viewerArrivedLocked(s)
	}
}
func (a *App) finishEventLocked(sid, id string, err error) bool {
	if err == nil {
		result, e := a.db.Exec("UPDATE events SET state='done' WHERE session_id=? AND id=? AND state='pending'", sid, id)
		if e == nil {
			n, e := result.RowsAffected()
			if e == nil && n == 1 {
				return true
			}
		}
	}
	_, _ = a.db.Exec("UPDATE events SET state='retry' WHERE session_id=? AND id=? AND state='pending'", sid, id)
	return false
}
func (a *App) submitEventLocked(s object, id, source, kind, text string) error {
	var existing string
	err := a.db.QueryRow("SELECT source FROM host_tasks WHERE session_id=? AND task_id=?", s["id"], id).Scan(&existing)
	if err == sql.ErrNoRows {
		return a.registeredTask(s, id, source, kind, text)
	}
	if err != nil {
		return err
	}
	if existing != source {
		return errors.New("Event playback source mismatch")
	}
	return a.task(s, id, kind, text)
}
func (a *App) queueReplyLocked(s object, id string, event ViewerTurn, arrival time.Time) <-chan bool {
	sid := str(s, "id")
	if a.replies == nil || a.replies.sid != sid {
		a.replies = &replyQueue{sid: sid}
	}
	q := a.replies
	job := replyJob{id, event, arrival, make(chan bool, 1)}
	if !q.active {
		q.active = true
		go a.runReplies(q, s, job)
	} else if len(q.queue) < 20 {
		q.queue = append(q.queue, job)
	} else {
		job.result <- a.finishEventLocked(sid, id, nil)
	}
	return job.result
}
func (a *App) runReplies(q *replyQueue, s object, job replyJob) {
	for {
		a.mu.Lock()
		var err error
		remember := false
		if str(a.current(), "id") != q.sid {
			err = errors.New("Session ended")
		} else if a.c.Clock().Sub(job.arrived) <= 30*time.Second {
			var in HostInput
			in, err = a.LoadHostContext(str(s, "avatarId"), q.sid)
			if err == nil {
				in.ViewerTurns = append([]ViewerTurn(nil), q.history...)
				a.mu.Unlock()
				var decision ReplyDecision
				decision, err = a.BuildInteractionReply(context.Background(), in, job.event)
				a.mu.Lock()
				if err == nil && decision.Action == "speak" && a.validHostInput(in) && a.c.Clock().Sub(job.arrived) <= 30*time.Second {
					err = a.submitEventLocked(s, job.id, "interaction", "comment", decision.Text)
					remember = err == nil
				}
			}
		}
		processed := a.finishEventLocked(q.sid, job.id, err)
		if processed && remember {
			q.history = appendViewerTurn(q.history, job.event)
		}
		job.result <- processed
		if len(q.queue) == 0 {
			q.active = false
			a.mu.Unlock()
			return
		}
		job = q.queue[0]
		q.queue[0] = replyJob{}
		q.queue = q.queue[1:]
		a.mu.Unlock()
	}
}

func (a *App) hostLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.done:
			return
		case <-ticker.C:
		}
		a.hostTick()
	}
}
func (a *App) hostTick() {
	a.mu.Lock()
	s := a.current()
	if s == nil || s["status"] != "live" {
		a.mu.Unlock()
		return
	}
	if str(s, "hostMode") == "companion" {
		a.mu.Unlock()
		_ = a.CompanionTick(context.Background(), str(s, "id"))
		return
	}
	var state struct {
		SessionID           string  `json:"sessionId"`
		Status              string  `json:"status"`
		Speaking            bool    `json:"speaking"`
		QueueDepth          int     `json:"queueDepth"`
		LastTaskCompletedAt float64 `json:"lastTaskCompletedAt"`
	}
	a.mu.Unlock()
	_ = a.pollPlayed(context.Background(), str(s, "id"))
	err := a.worker(context.Background(), "GET", "/status", nil, &state)
	a.mu.Lock()
	current := a.current()
	if current == nil || current["id"] != s["id"] || current["status"] != "live" {
		a.mu.Unlock()
		return
	}
	s = current
	if err != nil || state.SessionID != s["id"] || state.Status != "live" {
		s["status"] = "error"
		s["error"] = "Worker unavailable"
		s["collectorHash"] = ""
		_ = a.put("sessions", s)
		a.mu.Unlock()
		return
	}
	if s["hostEnabled"] != true {
		a.mu.Unlock()
		return
	}
	if state.Speaking || state.QueueDepth > 0 {
		a.lastHost = a.c.Clock()
		a.mu.Unlock()
		return
	}
	if state.LastTaskCompletedAt > 0 {
		completed := time.UnixMilli(int64(state.LastTaskCompletedAt * 1000))
		if completed.After(a.lastHost) {
			a.lastHost = completed
		}
	}
	if a.c.Clock().Sub(a.lastHost) < 5*time.Second {
		a.mu.Unlock()
		return
	}
	scripts := []string{}
	for _, v := range a.list("scripts") {
		if v["avatarId"] == s["avatarId"] && v["kind"] == "host" {
			scripts = append(scripts, str(v, "text"))
		}
	}
	if len(scripts) > 0 {
		if a.registeredTask(s, id(), "fixed", "host", scripts[a.hostIndex%len(scripts)]) == nil {
			a.hostIndex++
			a.lastHost = a.c.Clock()
		}
	}
	a.mu.Unlock()
}

func (a *App) settings(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		write(w, 200, object{"timezone": a.c.Timezone, "workerUrl": a.c.WorkerURL, "llmUrl": a.c.LLMURL, "llmModel": a.c.LLMModel, "configured": object{"worker": a.c.WorkerURL != "" && a.c.WorkerToken != "", "llm": a.c.LLMURL != "" && a.c.LLMToken != "", "media": a.c.MediaToken != ""}})
		return
	}
	if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/check") {
		var health object
		we := a.worker(r.Context(), "GET", "/healthz", nil, &health)
		var models object
		le := a.remote(r.Context(), a.c.LLMURL, a.c.LLMToken, "GET", "/models", nil, &models)
		write(w, 200, object{"worker": we == nil && health["ready"] == true, "llm": le == nil})
		return
	}
	fail(w, 405, "Method not allowed")
}
