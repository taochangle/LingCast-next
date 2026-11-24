package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

func (a *App) hostTopicsAPI(w http.ResponseWriter, r *http.Request, p []string) {
	if p[1] == "host-topics" {
		if len(p) == 3 && r.Method == "PATCH" {
			a.patchTopic(w, r, p[2])
			return
		}
		fail(w, 405, "Method not allowed")
		return
	}
	in, e := a.LoadHostContext(p[2], "")
	if e != nil {
		fail(w, 404, "人物不存在")
		return
	}
	if len(p) == 4 && r.Method == "GET" {
		ts, e := a.listTopics(p[2])
		if e != nil {
			fail(w, 500, "读取主题失败")
			return
		}
		write(w, 200, object{"topics": ts, "suggestedInterests": in.SuggestedInterests, "generation": a.topicJobStatus(in.AvatarID)})
		return
	}
	if len(p) == 5 && p[4] == "generate" && r.Method == "POST" {
		job, start, e := a.reserveJob(in.AvatarID)
		if e != nil {
			write(w, 429, a.topicJobStatus(in.AvatarID))
			return
		}
		if start {
			go func() {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() {
					select {
					case <-a.done:
						cancel()
					case <-ctx.Done():
					}
				}()
				a.runTopicJob(ctx, in, true, job)
			}()
		}
		write(w, 202, object{"avatarId": in.AvatarID, "jobId": job.id})
		return
	}
	fail(w, 405, "Method not allowed")
}
func (a *App) listTopics(avatar string) ([]HostTopic, error) {
	return a.queryTopics(avatar, "ORDER BY created_at DESC,rowid DESC LIMIT 200")
}

// suffix is an application-owned SQL clause. Candidate selection and the
// admin list share decoding, but apply their own eligibility and limits.
func (a *App) queryTopics(avatar, suffix string, args ...any) ([]HostTopic, error) {
	rows, e := a.db.Query("SELECT id,title,angles,state,created_at,topic_key FROM host_topics t WHERE avatar_id=? "+suffix, append([]any{avatar}, args...)...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	ts := []HostTopic{}
	for rows.Next() {
		var t HostTopic
		var angles string
		var created int64
		if e = rows.Scan(&t.ID, &t.Title, &angles, &t.State, &created, &t.Key); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(angles), &t.Angles); e != nil {
			return nil, e
		}
		t.CreatedAt = time.Unix(created, 0)
		t.AvatarID = avatar
		ts = append(ts, t)
	}
	return ts, rows.Err()
}
func (a *App) patchTopic(w http.ResponseWriter, r *http.Request, topicID string) {
	b, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
	m, e2 := modelFields(string(b), "title", "angles", "state")
	if e != nil || e2 != nil || len(m) == 0 {
		fail(w, 400, "主题格式无效")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	tx, e := a.db.Begin()
	if e != nil {
		fail(w, 500, "保存失败")
		return
	}
	defer tx.Rollback()
	var avatar, title, angles, state, key string
	e = tx.QueryRow("SELECT avatar_id,title,angles,state,topic_key FROM host_topics WHERE id=?", topicID).Scan(&avatar, &title, &angles, &state, &key)
	if e == sql.ErrNoRows {
		fail(w, 404, "主题不存在")
		return
	}
	if e != nil {
		fail(w, 500, "读取失败")
		return
	}
	if v := m["title"]; v != nil {
		if json.Unmarshal(v, &title) != nil || !cleanMaterial(title, 80) {
			fail(w, 400, "标题无效")
			return
		}
		key = speechKey(title)
	}
	if v := m["state"]; v != nil {
		if json.Unmarshal(v, &state) != nil || state != "disabled" {
			fail(w, 400, "状态无效")
			return
		}
	}
	if v := m["angles"]; v != nil {
		var ss []string
		if json.Unmarshal(v, &ss) != nil {
			fail(w, 400, "角度格式无效")
			return
		}
		items, e := makeAngles(ss)
		if e != nil {
			fail(w, 400, "角度无效")
			return
		}
		var old []HostAngle
		if json.Unmarshal([]byte(angles), &old) != nil {
			fail(w, 500, "读取失败")
			return
		}
		for i, item := range items {
			for _, o := range old {
				if speechKey(item.Text) == speechKey(o.Text) {
					items[i].ID = o.ID
					break
				}
			}
		}
		raw, _ := json.Marshal(items)
		angles = string(raw)
	}
	_, e = tx.Exec("UPDATE host_topics SET title=?,topic_key=?,angles=?,state=? WHERE id=?", title, key, angles, state, topicID)
	if e == nil {
		_, e = tx.Exec("UPDATE objects SET body=json_set(body,'$.revision',COALESCE(json_extract(body,'$.revision'),1)+1) WHERE kind='avatars' AND id=?", avatar)
	}
	if e != nil || tx.Commit() != nil {
		fail(w, 500, "保存失败")
		return
	}
	write(w, 200, object{"id": topicID})
}
