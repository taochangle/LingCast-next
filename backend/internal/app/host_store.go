package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

func migrateHost(db *sql.DB) error {
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = tx.Exec(`CREATE TABLE IF NOT EXISTS host_topics(id TEXT PRIMARY KEY, avatar_id TEXT NOT NULL, topic_key TEXT NOT NULL, title TEXT NOT NULL, angles TEXT NOT NULL, state TEXT NOT NULL, created_at INTEGER NOT NULL);
 CREATE INDEX IF NOT EXISTS host_topics_avatar ON host_topics(avatar_id,created_at);
 CREATE TABLE IF NOT EXISTS host_recent_keys(avatar_id TEXT NOT NULL, key TEXT NOT NULL, seen_at INTEGER NOT NULL, PRIMARY KEY(avatar_id,key));
 CREATE TABLE IF NOT EXISTS host_runs(session_id TEXT PRIMARY KEY, avatar_id TEXT NOT NULL,topic_id TEXT NOT NULL DEFAULT '',angle_id TEXT NOT NULL DEFAULT '',last_seq INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS host_tasks(session_id TEXT NOT NULL,task_id TEXT NOT NULL,source TEXT NOT NULL,topic_id TEXT NOT NULL,angle_id TEXT NOT NULL,state TEXT NOT NULL DEFAULT 'pending',PRIMARY KEY(session_id,task_id));
 CREATE INDEX IF NOT EXISTS host_tasks_unresolved ON host_tasks(topic_id,session_id) WHERE state IN ('pending','unknown');
 CREATE TABLE IF NOT EXISTS host_turns(session_id TEXT NOT NULL,task_id TEXT NOT NULL,source TEXT NOT NULL DEFAULT 'spoken',text TEXT NOT NULL,completed_at INTEGER NOT NULL,seq INTEGER NOT NULL,PRIMARY KEY(session_id,task_id));
 DELETE FROM host_turns WHERE source!='host' AND NOT EXISTS(SELECT 1 FROM live_slot WHERE session_id=host_turns.session_id);
 UPDATE objects SET body=json_set(body,'$.hostMode','fixed','$.interests',json('[]'),'$.revision',1) WHERE kind='avatars' AND json_type(body,'$.hostMode') IS NULL`)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func applyHostConfig(o, in object, newAvatar bool) error {
	if persona, ok := in["persona"].(string); ok && utf8.RuneCountInString(persona) > maxPersonaRunes {
		return errors.New("Persona exceeds 4096 characters")
	}
	if o["hostMode"] == nil {
		o["hostMode"] = "fixed"
		o["interests"] = []string{}
		o["revision"] = float64(1)
	}
	if newAvatar {
		o["hostMode"] = "companion"
	}
	if v, ok := in["hostMode"]; ok {
		mode, ok := v.(string)
		if !ok || !enum(mode, "fixed", "companion") {
			return errors.New("Invalid host mode")
		}
		o["hostMode"] = mode
	}
	if v, ok := in["interests"]; ok {
		items, ok := v.([]any)
		if !ok || len(items) > 5 {
			return errors.New("Invalid interests")
		}
		clean := []string{}
		for _, item := range items {
			s, ok := item.(string)
			s = strings.TrimSpace(s)
			if !ok || s == "" || utf8.RuneCountInString(s) > 40 {
				return errors.New("Invalid interest")
			}
			clean = append(clean, s)
		}
		o["interests"] = clean
	}
	_, persona := in["persona"]
	_, interests := in["interests"]
	_, mode := in["hostMode"]
	if !newAvatar && (persona || interests || mode) {
		revision, _ := o["revision"].(float64)
		o["revision"] = revision + 1
	}
	return nil
}
func (a *App) LoadHostContext(avatarID, sessionID string) (HostInput, error) {
	in := HostInput{AvatarID: avatarID, SessionID: sessionID, ServerTime: a.c.Clock().In(a.loc), Timezone: a.c.Timezone}
	var persona sql.NullString
	var interests, suggestions string
	err := a.db.QueryRow(`SELECT json_extract(body,'$.persona'),COALESCE(json_extract(body,'$.interests'),'[]'),COALESCE(json_extract(body,'$.revision'),1),COALESCE(json_extract(body,'$.suggestedInterests'),'[]') FROM objects WHERE kind='avatars' AND id=?`, avatarID).Scan(&persona, &interests, &in.Revision, &suggestions)
	if err != nil {
		return in, err
	}
	in.Persona = persona.String
	if err = json.Unmarshal([]byte(interests), &in.Interests); err != nil {
		return in, err
	}
	if len(in.Interests) == 0 {
		if err = json.Unmarshal([]byte(suggestions), &in.SuggestedInterests); err != nil {
			return in, err
		}
	}
	if sessionID != "" {
		var owner string
		if err = a.db.QueryRow(`SELECT json_extract(body,'$.avatarId') FROM objects WHERE kind='sessions' AND id=?`, sessionID).Scan(&owner); err != nil {
			return in, err
		}
		if owner != avatarID {
			return in, errors.New("Session avatar mismatch")
		}
		var t HostTopic
		var angles string
		var created int64
		err = a.db.QueryRow(`SELECT t.id,t.title,t.angles,t.state,t.created_at,t.topic_key,r.angle_id FROM host_runs r JOIN host_topics t ON t.id=r.topic_id WHERE r.session_id=? AND r.avatar_id=? AND t.avatar_id=? AND t.state!='disabled'`, sessionID, avatarID, avatarID).Scan(&t.ID, &t.Title, &angles, &t.State, &created, &t.Key, &in.CurrentAngleID)
		if err == nil {
			t.AvatarID = avatarID
			t.CreatedAt = time.Unix(created, 0)
			if err = json.Unmarshal([]byte(angles), &t.Angles); err != nil {
				return in, err
			}
			in.CurrentTopic = &t
		} else if err != sql.ErrNoRows {
			return in, err
		}
		rows, err := a.db.Query(`SELECT task_id,source,text FROM (SELECT * FROM host_turns WHERE session_id=? ORDER BY seq DESC LIMIT 12) ORDER BY seq`, sessionID)
		if err != nil {
			return in, err
		}
		for rows.Next() {
			var turn SpokenTurn
			if err = rows.Scan(&turn.TaskID, &turn.Source, &turn.Text); err != nil {
				rows.Close()
				return in, err
			}
			in.SpokenTurns = append(in.SpokenTurns, turn)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return in, err
		}
	}
	rows, err := a.db.Query(`SELECT key FROM (SELECT topic_key AS key,created_at AS ts FROM host_topics WHERE avatar_id=? AND state!='disabled' UNION ALL SELECT key,seen_at FROM host_recent_keys WHERE avatar_id=?) WHERE ts>=? GROUP BY key ORDER BY MAX(ts) DESC,key LIMIT 200`, avatarID, avatarID, in.ServerTime.Add(-7*24*time.Hour).Unix())
	if err != nil {
		return in, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			return in, err
		}
		in.RecentTopicKeys = append(in.RecentTopicKeys, key)
	}
	return in, rows.Err()
}

// Retire only the unchanged application-owned material that was audited.
func (a *App) quarantineMaterial(in HostInput) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.validHostInput(in) {
		return nil
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	t := in.CurrentTopic
	res, err := tx.Exec("UPDATE host_topics SET state='disabled' WHERE avatar_id=? AND id=? AND title=? AND topic_key=? AND state IN ('ready','active','done')", in.AvatarID, t.ID, t.Title, t.Key)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	// Keys derived from rejected titles or angles must also leave model-facing memory.
	if _, err = tx.Exec("DELETE FROM host_recent_keys WHERE avatar_id=? AND (key=? OR substr(key,1,length(?)+1)=?||'/')", in.AvatarID, t.Key, t.Key, t.Key); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.invalidateHostLocked(in.SessionID)
	return nil
}

type TaskResultsPage struct {
	Results   []TaskResult `json:"results"`
	NextSeq   int64        `json:"nextSeq"`
	Truncated bool         `json:"truncated"`
}

// RegisterPlayback stores only trusted routing metadata before submission.
func (a *App) RegisterPlayback(sessionID, taskID, source, topicID, angleID string) error {
	if taskID == "" || !enum(source, "manual", "fixed", "host", "interaction") || (source == "host") != (topicID != "" && angleID != "") || (topicID == "") != (angleID == "") {
		return errors.New("Invalid playback registration")
	}
	tx, e := a.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var owner string
	if e = tx.QueryRow(`SELECT json_extract(body,'$.avatarId') FROM objects WHERE kind='sessions' AND id=?`, sessionID).Scan(&owner); e != nil {
		return e
	}
	if topicID != "" {
		var exists int
		if e = tx.QueryRow(`SELECT 1 FROM host_topics t,json_each(t.angles) j WHERE t.id=? AND t.avatar_id=? AND json_extract(j.value,'$.id')=? AND t.state IN ('ready','active')`, topicID, owner, angleID).Scan(&exists); e != nil {
			return e
		}
	}
	_, e = tx.Exec(`INSERT INTO host_tasks(session_id,task_id,source,topic_id,angle_id) VALUES(?,?,?,?,?)`, sessionID, taskID, source, topicID, angleID)
	if e != nil {
		return e
	}
	return tx.Commit()
}

func (a *App) PlaybackCursor(sessionID string) (int64, error) {
	var n int64
	err := a.db.QueryRow(`SELECT COALESCE((SELECT last_seq FROM host_runs WHERE session_id=?),0)`, sessionID).Scan(&n)
	return n, err
}

func (a *App) RecordPlayed(sessionID string, r TaskResult) error {
	return a.ApplyTaskResults(sessionID, TaskResultsPage{Results: []TaskResult{r}, NextSeq: r.Seq})
}

func (a *App) ApplyTaskResults(sessionID string, page TaskResultsPage) error {
	if len(page.Results) > 256 || page.NextSeq < 0 {
		return errors.New("Invalid playback page")
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO host_runs(session_id,avatar_id) SELECT id,json_extract(body,'$.avatarId') FROM objects WHERE kind='sessions' AND id=? ON CONFLICT DO NOTHING`, sessionID)
	if err != nil {
		return err
	}
	var seq int64
	if err = tx.QueryRow(`SELECT last_seq FROM host_runs WHERE session_id=?`, sessionID).Scan(&seq); err != nil {
		return err
	}
	if page.NextSeq <= seq {
		return nil
	}
	gap := false
	for i, r := range page.Results {
		if r.Seq <= 0 || r.TaskID == "" || !enum(r.State, "completed", "failed") || (i > 0 && r.Seq != page.Results[i-1].Seq+1) {
			return errors.New("Invalid playback receipt")
		}
		if r.Seq <= seq {
			continue
		}
		if r.Seq != seq+1 {
			if !page.Truncated || i != 0 {
				return errors.New("Missing playback receipts")
			}
			gap = true
		}
		if err = applyPlayback(tx, sessionID, r); err != nil {
			return err
		}
		seq = r.Seq
	}
	if seq != page.NextSeq {
		return errors.New("Invalid playback cursor")
	}
	if gap {
		_, err = tx.Exec(`UPDATE host_tasks SET state='unknown' WHERE session_id=? AND state='pending'`, sessionID)
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(`UPDATE host_runs SET last_seq=? WHERE session_id=?`, seq, sessionID)
	if err != nil {
		return err
	}
	var owner string
	if err = tx.QueryRow("SELECT avatar_id FROM host_runs WHERE session_id=?", sessionID).Scan(&owner); err != nil {
		return err
	}
	if err = pruneHostTx(tx, owner, a.c.Clock()); err != nil {
		return err
	}
	return tx.Commit()
}

func applyPlayback(tx *sql.Tx, sid string, r TaskResult) error {
	var source, topic, angle, state string
	err := tx.QueryRow(`SELECT source,topic_id,angle_id,state FROM host_tasks WHERE session_id=? AND task_id=?`, sid, r.TaskID).Scan(&source, &topic, &angle, &state)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if state != "pending" && state != "unknown" {
		return nil
	}
	if _, err = tx.Exec(`UPDATE host_tasks SET state=? WHERE session_id=? AND task_id=?`, r.State, sid, r.TaskID); err != nil {
		return err
	}
	if r.State == "failed" {
		if source != "host" {
			return nil
		}
		// Releasing a pending or unknown task can put its unused topic back
		// into a reserve that was replenished in the meantime. Retire only
		// the returning topic if there is no slot, in the receipt transaction.
		_, err = tx.Exec(`UPDATE host_topics AS recovered SET state='disabled'
   WHERE recovered.id=? AND recovered.state='ready' AND EXISTS (
    SELECT 1 FROM host_topics t WHERE t.avatar_id=recovered.avatar_id
     AND t.state='ready' AND `+selectableHostTopic+`
    LIMIT 1 OFFSET ?
   )`, topic, maxReadyHostTopics)
		return err
	}
	if strings.TrimSpace(r.Text) == "" || len(r.Text) > 10000 || r.CompletedAt.IsZero() {
		return errors.New("Incomplete playback receipt")
	}
	if _, err = tx.Exec(`INSERT INTO host_turns(session_id,task_id,source,text,completed_at,seq) VALUES(?,?,?,?,?,?)`, sid, r.TaskID, source, r.Text, r.CompletedAt.Unix(), r.Seq); err != nil {
		return err
	}
	if source != "host" {
		return nil
	}
	res, err := tx.Exec(`UPDATE host_topics SET state=CASE WHEN json_extract(angles,'$[#-1].id')=? THEN 'done' ELSE 'active' END WHERE id=? AND state IN ('active','ready') AND EXISTS (SELECT 1 FROM json_each(angles) WHERE json_extract(value,'$.id')=?)`, angle, topic, angle)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	var owner, key, angleText string
	if err = tx.QueryRow(`SELECT t.avatar_id,t.topic_key,json_extract(j.value,'$.text') FROM host_topics t,json_each(t.angles) j WHERE t.id=? AND json_extract(j.value,'$.id')=?`, topic, angle).Scan(&owner, &key, &angleText); err != nil {
		return err
	}
	for _, k := range []string{key, key + "/" + speechKey(angleText)} {
		if _, err = tx.Exec("INSERT INTO host_recent_keys VALUES(?,?,?) ON CONFLICT(avatar_id,key) DO UPDATE SET seen_at=MAX(seen_at,excluded.seen_at)", owner, k, r.CompletedAt.Unix()); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`UPDATE host_runs SET topic_id=?,angle_id=? WHERE session_id=?`, topic, angle, sid)
	return err
}

func (a *App) pruneHostHistory(ctx context.Context, avatar string) error {
	tx, e := a.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = pruneHostTx(tx, avatar, a.c.Clock()); e != nil {
		return e
	}
	return tx.Commit()
}
func pruneHostTx(tx *sql.Tx, avatar string, now time.Time) error {
	cutoff := now.Add(-7 * 24 * time.Hour).Unix()
	_, e := tx.Exec(`UPDATE host_topics SET state='disabled'
		WHERE avatar_id=? AND state='ready' AND EXISTS (
			SELECT 1 FROM host_tasks h
			WHERE h.topic_id=host_topics.id AND h.state IN ('pending','unknown')
			AND NOT EXISTS (SELECT 1 FROM live_slot WHERE session_id=h.session_id)
		)`, avatar)
	if e != nil {
		return e
	}
	_, e = tx.Exec(`DELETE FROM host_recent_keys WHERE avatar_id=? AND (seen_at<? OR rowid NOT IN(SELECT rowid FROM host_recent_keys WHERE avatar_id=? ORDER BY seen_at DESC,rowid DESC LIMIT 200))`, avatar, cutoff, avatar)
	if e != nil {
		return e
	}
	_, e = tx.Exec(`DELETE FROM host_topics WHERE avatar_id=? AND state!='ready' AND id NOT IN (SELECT r.topic_id FROM host_runs r JOIN objects o ON o.id=r.session_id AND o.kind='sessions' WHERE r.avatar_id=host_topics.avatar_id AND json_extract(o.body,'$.status')='live') AND (created_at<? OR id NOT IN(SELECT id FROM host_topics WHERE avatar_id=? ORDER BY created_at DESC,rowid DESC LIMIT 200))`, avatar, cutoff, avatar)
	if e != nil {
		return e
	}
	_, e = tx.Exec(`DELETE FROM host_turns WHERE session_id IN(SELECT session_id FROM host_runs WHERE avatar_id=?) AND (completed_at<? OR (source='host' AND rowid NOT IN(SELECT t.rowid FROM host_turns t JOIN host_runs r USING(session_id) WHERE r.avatar_id=? AND source='host' ORDER BY completed_at DESC,t.rowid DESC LIMIT 100)) OR (source!='host' AND (NOT EXISTS(SELECT 1 FROM live_slot WHERE session_id=host_turns.session_id) OR rowid NOT IN(SELECT t.rowid FROM host_turns t JOIN host_runs r USING(session_id) WHERE r.avatar_id=? AND source!='host' AND t.session_id=host_turns.session_id ORDER BY completed_at DESC,t.rowid DESC LIMIT 12))))`, avatar, cutoff, avatar, avatar)
	return e
}

func clearSessionSpeech(tx *sql.Tx, sessionID string) error {
	_, err := tx.Exec("DELETE FROM host_turns WHERE session_id=? AND source!='host'", sessionID)
	return err
}
