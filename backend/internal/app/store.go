package app

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (a *App) put(k string, o object) error {
	b, e := json.Marshal(o)
	if e != nil {
		return e
	}
	_, e = a.db.Exec("INSERT INTO objects VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", o["id"], k, string(b))
	return e
}
func (a *App) get(k, i string) object {
	var b string
	if a.db.QueryRow("SELECT body FROM objects WHERE kind=? AND id=?", k, i).Scan(&b) != nil {
		return nil
	}
	var o object
	_ = json.Unmarshal([]byte(b), &o)
	return o
}
func (a *App) list(k string) []object {
	rows, e := a.db.Query("SELECT body FROM objects WHERE kind=? ORDER BY rowid", k)
	out := []object{}
	if e != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var b string
		_ = rows.Scan(&b)
		var o object
		_ = json.Unmarshal([]byte(b), &o)
		out = append(out, o)
	}
	return out
}
func (a *App) current() object {
	var i string
	if a.db.QueryRow("SELECT session_id FROM live_slot WHERE slot=1").Scan(&i) != nil {
		return nil
	}
	return a.get("sessions", i)
}
func (a *App) crud(w http.ResponseWriter, r *http.Request, p []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	k := p[1]
	parentKey, parentID := "", ""
	if len(p) == 4 {
		parentID = p[2]
		switch {
		case k == "avatars" && p[3] == "looks":
			parentKey = "avatarId"
		case k == "avatars" && p[3] == "scripts":
			parentKey = "avatarId"
		case k == "looks" && p[3] == "idles":
			parentKey = "lookId"
		default:
			fail(w, 404, "Not found")
			return
		}
		if a.get(k, parentID) == nil {
			fail(w, 404, "Parent not found")
			return
		}
		k = p[3]
	}
	if k != "avatars" && k != "looks" && k != "idles" && k != "scripts" {
		fail(w, 404, "Not found")
		return
	}
	collection := len(p) == 2 || len(p) == 4
	if collection && r.Method == "GET" {
		out := []object{}
		for _, o := range a.list(k) {
			if parentKey == "" || o[parentKey] == parentID {
				out = append(out, o)
			}
		}
		write(w, 200, out)
		return
	}
	var o object
	if !collection {
		o = a.get(k, p[2])
		if o == nil {
			fail(w, 404, "Not found")
			return
		}
	}
	if r.Method == "GET" && !collection {
		write(w, 200, o)
		return
	}
	if r.Method != "POST" && r.Method != "PATCH" && r.Method != "DELETE" {
		fail(w, 405, "Method not allowed")
		return
	}
	if r.Method == "POST" && !collection || r.Method != "POST" && collection {
		fail(w, 405, "Method not allowed")
		return
	}
	liveAvatarPatch := false
	if s := a.current(); s != nil && !collection {
		blocked := k == "avatars" && s["avatarId"] == o["id"] || k == "looks" && s["lookId"] == o["id"] || k == "idles" && s["lookId"] == o["lookId"]
		liveAvatarPatch = k == "avatars" && blocked && r.Method == "PATCH"
		if blocked && !liveAvatarPatch {
			fail(w, 409, "Live resources are locked")
			return
		}
	}
	if r.Method == "DELETE" {
		for _, child := range a.list("looks") {
			if child["avatarId"] == o["id"] {
				fail(w, 409, "Remove looks first")
				return
			}
		}
		for _, child := range a.list("scripts") {
			if child["avatarId"] == o["id"] {
				fail(w, 409, "Remove scripts first")
				return
			}
		}
		for _, child := range a.list("idles") {
			if child["lookId"] == o["id"] {
				fail(w, 409, "Remove idles first")
				return
			}
		}
		if k == "idles" {
			look := a.get("looks", str(o, "lookId"))
			if look != nil && look["defaultIdleId"] == o["id"] {
				look["defaultIdleId"] = ""
				_ = a.put("looks", look)
			}
		}
		_, e := a.db.Exec("DELETE FROM objects WHERE id=?", o["id"])
		if e != nil {
			fail(w, 500, "Storage failure")
			return
		}
		write(w, 200, object{"deleted": true})
		return
	}
	var in object
	if !decode(w, r, &in) {
		return
	}
	if liveAvatarPatch {
		for key := range in {
			if key != "interests" && key != "persona" {
				fail(w, 409, "Live resources are locked")
				return
			}
		}
	}
	if o == nil {
		o = object{"id": id()}
		if parentKey != "" {
			o[parentKey] = parentID
		}
	}
	if k == "avatars" {
		if err := applyHostConfig(o, in, collection); err != nil {
			fail(w, 400, err.Error())
			return
		}
	}
	fields := map[string][]string{"avatars": {"name", "persona", "voiceId"}, "looks": {"name", "imageId", "defaultIdleId"}, "idles": {"mediaId"}, "scripts": {"kind", "text"}}
	for _, f := range fields[k] {
		if v, ok := in[f]; ok {
			if _, valid := v.(string); !valid {
				fail(w, 400, "Fields must be strings")
				return
			}
			o[f] = v
		}
	}
	required := map[string][]string{"avatars": {"name", "voiceId"}, "looks": {"name", "imageId", "avatarId"}, "idles": {"mediaId", "lookId"}, "scripts": {"kind", "text", "avatarId"}}
	for _, f := range required[k] {
		if strings.TrimSpace(str(o, f)) == "" {
			fail(w, 400, "Missing "+f)
			return
		}
	}
	if k == "looks" {
		m := a.get("media", str(o, "imageId"))
		if m == nil || m["kind"] != "image" {
			fail(w, 400, "Image required")
			return
		}
		if d := str(o, "defaultIdleId"); d != "" {
			i := a.get("idles", d)
			if i == nil || i["lookId"] != o["id"] {
				fail(w, 400, "Default idle belongs to another look")
				return
			}
		}
		if o["defaultIdleId"] == nil {
			o["defaultIdleId"] = ""
		}
	}
	if k == "idles" {
		m := a.get("media", str(o, "mediaId"))
		if m == nil || m["kind"] != "video" {
			fail(w, 400, "Silent video required")
			return
		}
	}
	if k == "scripts" && !enum(str(o, "kind"), "host", "welcome", "gift", "like", "comment") {
		fail(w, 400, "Invalid script kind")
		return
	}
	if e := a.put(k, o); e != nil {
		fail(w, 500, "Storage failure")
		return
	}
	code := 200
	if r.Method == "POST" {
		code = 201
	}
	write(w, code, o)
}

// terminal persists the terminal record and releases only this session's slot atomically.
func (a *App) terminal(s object) error {
	if a.replies != nil && a.replies.sid == str(s, "id") {
		a.replies.history = nil
		a.replies = nil
	}
	tx, e := a.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	raw, e := json.Marshal(s)
	if e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE objects SET body=? WHERE id=? AND kind='sessions'", string(raw), s["id"]); e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM live_slot WHERE session_id=?", s["id"]); e != nil {
		return e
	}
	if e = pruneHostTx(tx, str(s, "avatarId"), a.c.Clock()); e != nil {
		return e
	}
	if e = clearSessionSpeech(tx, str(s, "id")); e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM events WHERE session_id=?", s["id"]); e != nil {
		return e
	}
	return tx.Commit()
}
func (a *App) lastSession() object {
	var raw string
	e := a.db.QueryRow(`SELECT body FROM objects WHERE kind='sessions' AND json_extract(body,'$.status') IN ('stopped','error') AND NOT EXISTS(SELECT 1 FROM live_slot WHERE session_id=objects.id) ORDER BY rowid DESC LIMIT 1`).Scan(&raw)
	if e != nil {
		return nil
	}
	var s object
	_ = json.Unmarshal([]byte(raw), &s)
	return s
}
func enum(value string, allowed ...string) bool {
	for _, v := range allowed {
		if value == v {
			return true
		}
	}
	return false
}
