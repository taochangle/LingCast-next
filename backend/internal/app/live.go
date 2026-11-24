package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

func safeSession(s object) object {
	if s == nil {
		return nil
	}
	o := object{}
	for k, v := range s {
		if k != "collectorHash" {
			o[k] = v
		}
	}
	return o
}
func (a *App) live(w http.ResponseWriter, r *http.Request, p []string) {
	if len(p) == 2 && r.Method == "GET" {
		write(w, 200, object{"session": safeSession(a.current())})
		return
	}
	if len(p) == 2 && r.Method == "POST" {
		a.start(w, r)
		return
	}
	if len(p) != 4 {
		fail(w, 404, "Not found")
		return
	}
	a.mu.Lock()
	s := a.current()
	if s == nil || s["id"] != p[2] {
		a.mu.Unlock()
		fail(w, 409, "Stale session")
		return
	}
	if p[3] == "stop" && r.Method == "POST" {
		if a.cancel != nil {
			a.cancel()
		}
		s["status"] = "stopping"
		s["collectorHash"] = ""
		_ = a.put("sessions", s)
		a.mu.Unlock()
		e := a.worker(context.Background(), "POST", "/sessions/"+p[2]+"/stop", nil, nil)
		a.mu.Lock()
		defer a.mu.Unlock()
		if e != nil {
			s["status"] = "error"
			s["error"] = "Worker stop unconfirmed; retry stop"
			_ = a.put("sessions", s)
			fail(w, 503, str(s, "error"))
			return
		}
		s["status"] = "stopped"
		_ = a.put("sessions", s)
		_, _ = a.db.Exec("DELETE FROM live_slot WHERE session_id=?", p[2])
		write(w, 200, safeSession(s))
		return
	}
	if s["status"] != "live" {
		a.mu.Unlock()
		fail(w, 409, "Session is not live")
		return
	}
	switch p[3] {
	case "collector-token":
		if r.Method != "POST" {
			break
		}
		token := id() + id()
		s["collectorHash"] = hash(token)
		_ = a.put("sessions", s)
		a.mu.Unlock()
		write(w, 200, object{"collectorToken": token})
		return
	case "host":
		if r.Method != "PATCH" {
			break
		}
		var b struct {
			Enabled bool `json:"enabled"`
		}
		if !decode(w, r, &b) {
			a.mu.Unlock()
			return
		}
		s["hostEnabled"] = b.Enabled
		_ = a.put("sessions", s)
		a.mu.Unlock()
		write(w, 200, safeSession(s))
		return
	case "speak":
		if r.Method != "POST" {
			break
		}
		var b struct {
			Text string `json:"text"`
		}
		if !decode(w, r, &b) {
			a.mu.Unlock()
			return
		}
		if strings.TrimSpace(b.Text) == "" || len(b.Text) > 10000 {
			a.mu.Unlock()
			fail(w, 400, "Text required, maximum 10000 bytes")
			return
		}
		e := a.task(s, id(), "manual", b.Text)
		a.mu.Unlock()
		if e != nil {
			fail(w, 503, "Worker unavailable")
		} else {
			write(w, 200, object{"queued": true})
		}
		return
	case "relay":
		if r.Method != "PUT" {
			break
		}
		var b struct {
			Enabled   bool   `json:"enabled"`
			Server    string `json:"server"`
			StreamKey string `json:"streamKey"`
		}
		if !decode(w, r, &b) {
			a.mu.Unlock()
			return
		}
		if b.Enabled && (!strings.HasPrefix(b.Server, "rtmp://") && !strings.HasPrefix(b.Server, "rtmps://") || b.StreamKey == "") {
			a.mu.Unlock()
			fail(w, 400, "RTMP server and stream key required")
			return
		}
		e := a.worker(r.Context(), "PUT", "/sessions/"+p[2]+"/relay", b, nil)
		a.mu.Unlock()
		if e != nil {
			fail(w, 503, "Relay update failed")
		} else {
			write(w, 200, object{"enabled": b.Enabled})
		}
		return
	case "preview":
		a.mu.Unlock()
		req, e := http.NewRequestWithContext(r.Context(), "GET", strings.TrimRight(a.c.WorkerURL, "/")+"/preview/"+p[2]+".flv", nil)
		if e != nil {
			fail(w, 503, "Preview unavailable")
			return
		}
		req.Header.Set("Authorization", "Bearer "+a.c.WorkerToken)
		streamClient := *a.c.HTTPClient
		streamClient.Timeout = 0
		resp, e := streamClient.Do(req)
		if e != nil {
			fail(w, 503, "Preview unavailable")
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "video/x-flv")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return
	}
	a.mu.Unlock()
	fail(w, 405, "Method not allowed")
}
func (a *App) start(w http.ResponseWriter, r *http.Request) {
	var b struct {
		AvatarID string `json:"avatarId"`
		LookID   string `json:"lookId"`
		Platform string `json:"platform"`
	}
	if !decode(w, r, &b) {
		return
	}
	a.mu.Lock()
	if a.current() != nil {
		a.mu.Unlock()
		fail(w, 409, "A session already occupies the live slot")
		return
	}
	avatar := a.get("avatars", b.AvatarID)
	look := a.get("looks", b.LookID)
	if avatar == nil || look == nil || look["avatarId"] != b.AvatarID || b.Platform != "douyin" && b.Platform != "xiaohongshu" {
		a.mu.Unlock()
		fail(w, 400, "Invalid avatar, look or platform")
		return
	}
	defaultIdle := a.get("idles", str(look, "defaultIdleId"))
	if defaultIdle == nil || defaultIdle["lookId"] != b.LookID {
		a.mu.Unlock()
		fail(w, 400, "Default idle required")
		return
	}
	token := id() + id()
	s := object{"id": id(), "avatarId": b.AvatarID, "lookId": b.LookID, "platform": b.Platform, "status": "starting", "hostEnabled": false, "previewUrl": "", "error": "", "createdAt": a.c.Clock().UTC().Format(time.RFC3339Nano), "collectorHash": hash(token)}
	tx, e := a.db.Begin()
	if e == nil {
		_, e = tx.Exec("INSERT INTO live_slot VALUES(1,?)", s["id"])
		if e == nil {
			raw, _ := json.Marshal(s)
			_, e = tx.Exec("INSERT INTO objects VALUES(?,?,?)", s["id"], "sessions", string(raw))
		}
		if e == nil {
			e = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
	}
	if e != nil {
		a.mu.Unlock()
		fail(w, 409, "Live slot busy")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.hostIndex = 0
	a.lastHost = time.Time{}
	idles := []object{}
	for _, i := range a.list("idles") {
		if i["lookId"] == b.LookID {
			m := a.get("media", str(i, "mediaId"))
			idles = append(idles, object{"id": i["id"], "url": a.mediaURL(str(i, "mediaId")), "sha256": m["sha256"]})
		}
	}
	payload := object{"sessionId": s["id"], "avatarId": b.AvatarID, "lookId": b.LookID, "voiceId": avatar["voiceId"], "imageUrl": a.mediaURL(str(look, "imageId")), "idles": idles, "defaultIdleId": look["defaultIdleId"], "platform": b.Platform}
	a.mu.Unlock()
	var health struct {
		Ready bool `json:"ready"`
	}
	e = a.worker(ctx, "GET", "/healthz", nil, &health)
	if e == nil && !health.Ready {
		e = context.Canceled
	}
	var result object
	startAttempted := false
	if e == nil {
		startAttempted = true
		e = a.worker(ctx, "POST", "/sessions", payload, &result)
	}
	a.mu.Lock()
	current := a.current()
	stale := current == nil || current["id"] != s["id"] || current["status"] != "starting"
	a.mu.Unlock()
	if e != nil || stale { // Always reconcile a timed-out start before releasing the slot.
		var cleanup error
		if startAttempted {
			cleanup = a.worker(context.Background(), "POST", "/sessions/"+str(s, "id")+"/stop", nil, nil)
		}
		a.mu.Lock()
		if cur := a.current(); cur != nil && cur["id"] == s["id"] && cur["status"] == "starting" {
			s["status"] = "error"
			s["error"] = "Worker start failed"
			s["collectorHash"] = ""
			_ = a.put("sessions", s)
			if cleanup == nil {
				_, _ = a.db.Exec("DELETE FROM live_slot WHERE session_id=?", s["id"])
			}
		}
		a.mu.Unlock()
		fail(w, 503, "Worker start failed or cancelled")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	current = a.current()
	if current == nil || current["id"] != s["id"] || current["status"] != "starting" {
		fail(w, 409, "Start cancelled")
		return
	}
	s["status"] = "live"
	s["previewUrl"] = strings.TrimRight(a.c.PublicBaseURL, "/") + "/api/live/" + str(s, "id") + "/preview"
	_ = a.put("sessions", s)
	write(w, 201, object{"session": safeSession(s), "collectorToken": token})
}
func (a *App) task(s object, i, k, text string) error {
	now := a.c.Clock().UTC()
	return a.worker(context.Background(), "POST", "/sessions/"+str(s, "id")+"/tasks", object{"id": i, "kind": k, "text": text, "createdAt": now.Format(time.RFC3339Nano), "expiresAt": now.Add(120 * time.Second).Format(time.RFC3339Nano)}, nil)
}
