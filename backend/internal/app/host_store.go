package app

import (
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
 CREATE TABLE IF NOT EXISTS host_runs(session_id TEXT PRIMARY KEY, avatar_id TEXT NOT NULL,topic_id TEXT NOT NULL DEFAULT '',angle_id TEXT NOT NULL DEFAULT '',last_seq INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS host_tasks(session_id TEXT NOT NULL,task_id TEXT NOT NULL,source TEXT NOT NULL,topic_id TEXT NOT NULL,angle_id TEXT NOT NULL,state TEXT NOT NULL DEFAULT 'pending',PRIMARY KEY(session_id,task_id));
 CREATE TABLE IF NOT EXISTS host_turns(session_id TEXT NOT NULL,task_id TEXT NOT NULL,source TEXT NOT NULL DEFAULT 'spoken',text TEXT NOT NULL,completed_at INTEGER NOT NULL,seq INTEGER NOT NULL,PRIMARY KEY(session_id,task_id));
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
	var interests string
	err := a.db.QueryRow(`SELECT json_extract(body,'$.persona'),COALESCE(json_extract(body,'$.interests'),'[]'),COALESCE(json_extract(body,'$.revision'),1) FROM objects WHERE kind='avatars' AND id=?`, avatarID).Scan(&persona, &interests, &in.Revision)
	if err != nil {
		return in, err
	}
	in.Persona = persona.String
	if err = json.Unmarshal([]byte(interests), &in.Interests); err != nil {
		return in, err
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
		err = a.db.QueryRow(`SELECT t.id,t.title,t.angles,t.state,t.created_at,t.topic_key,r.angle_id FROM host_runs r JOIN host_topics t ON t.id=r.topic_id WHERE r.session_id=? AND r.avatar_id=? AND t.avatar_id=?`, sessionID, avatarID, avatarID).Scan(&t.ID, &t.Title, &angles, &t.State, &created, &t.Key, &in.CurrentAngleID)
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
	rows, err := a.db.Query(`SELECT topic_key FROM host_topics WHERE avatar_id=? AND created_at>=? ORDER BY created_at DESC LIMIT 200`, avatarID, in.ServerTime.Add(-7*24*time.Hour).Unix())
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
		return nil
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
	_, err = tx.Exec(`UPDATE host_runs SET topic_id=?,angle_id=? WHERE session_id=?`, topic, angle, sid)
	return err
}
