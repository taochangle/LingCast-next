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
func (a *App) RecordPlayed(sessionID string, r TaskResult) error {
	if sessionID == "" || r.TaskID == "" || r.Seq <= 0 || !enum(r.State, "completed", "failed") {
		return errors.New("Invalid playback receipt")
	}
	if r.State == "failed" {
		return nil
	}
	if strings.TrimSpace(r.Text) == "" || r.CompletedAt.IsZero() {
		return errors.New("Incomplete playback receipt")
	}
	_, err := a.db.Exec(`INSERT INTO host_turns(session_id,task_id,text,completed_at,seq) VALUES(?,?,?,?,?) ON CONFLICT(session_id,task_id) DO NOTHING`, sessionID, r.TaskID, r.Text, r.CompletedAt.Unix(), r.Seq)
	return err
}
