package app

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"
)

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
	outcomes := map[string]string{}
	for _, e := range b.Events {
		outcomes[e.ID] = "retry"
	}
	for _, e := range b.Events {
		a.mu.Unlock()
		a.mu.Lock()
		current := a.current()
		if current == nil || current["id"] != s["id"] || current["status"] != "live" {
			break
		}
		var state string
		err := a.db.QueryRow("SELECT state FROM events WHERE session_id=? AND id=?", s["id"], e.ID).Scan(&state)
		if err == nil {
			if state == "done" {
				outcomes[e.ID] = "processed"
			}
			continue
		}
		if err != sql.ErrNoRows {
			fail(w, 500, "Storage failure")
			return
		}
		var count int
		if a.db.QueryRow("SELECT COUNT(*) FROM events WHERE session_id=?", s["id"]).Scan(&count) != nil {
			fail(w, 500, "Storage failure")
			return
		}
		if count >= 10000 {
			continue
		}
		if _, err = a.db.Exec("INSERT INTO events(session_id,id,state) VALUES(?,?,'pending')", s["id"], e.ID); err != nil {
			continue
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
		if text == "" && kind == "comment" {
			avatar := a.get("avatars", str(s, "avatarId"))
			a.mu.Unlock()
			text, err = a.reply(context.Background(), str(avatar, "persona"), e.Nickname, e.Text)
			a.mu.Lock()
			current = a.current()
			if current == nil || current["id"] != s["id"] || current["status"] != "live" {
				_, _ = a.db.Exec("DELETE FROM events WHERE session_id=? AND id=? AND state='pending'", s["id"], e.ID)
				break
			}
		}
		if err == nil && text != "" {
			err = a.task(s, e.ID, kind, text)
		}
		if err == nil {
			var result sql.Result
			result, err = a.db.Exec("UPDATE events SET state='done' WHERE session_id=? AND id=? AND state='pending'", s["id"], e.ID)
			if err == nil {
				var n int64
				n, err = result.RowsAffected()
				if n != 1 && err == nil {
					err = fmt.Errorf("Lost event reservation")
				}
			}
		}
		if err != nil {
			_, _ = a.db.Exec("DELETE FROM events WHERE session_id=? AND id=? AND state='pending'", s["id"], e.ID)
			continue
		}
		accepted++
		outcomes[e.ID] = "processed"
	}
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
	var state struct {
		SessionID           string  `json:"sessionId"`
		Status              string  `json:"status"`
		Speaking            bool    `json:"speaking"`
		QueueDepth          int     `json:"queueDepth"`
		LastTaskCompletedAt float64 `json:"lastTaskCompletedAt"`
	}
	err := a.worker(context.Background(), "GET", "/status", nil, &state)
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
		if a.task(s, id(), "host", scripts[a.hostIndex%len(scripts)]) == nil {
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
