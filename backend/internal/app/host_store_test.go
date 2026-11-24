package app

import (
	"strings"
	"testing"
)

func TestHostConfigMigration(t *testing.T) {
	f := newFixture(t)
	if err := f.a.put("scripts", object{"id": "old", "avatarId": "a1", "kind": "host", "text": "  原文  "}); err != nil {
		t.Fatal(err)
	}
	h, err := New(f.a.c)
	if err != nil {
		t.Fatal(err)
	}
	a := h.(*App)
	defer a.Close()
	if a.get("avatars", "a1")["hostMode"] != "fixed" || a.get("scripts", "old")["text"] != "  原文  " {
		t.Fatal("old avatar/script migration")
	}
	res := f.req("POST", "/api/avatars", `{"name":"新","voiceId":"v"}`, "a")
	if res.Code != 201 || !strings.Contains(res.Body.String(), `"hostMode":"companion"`) {
		t.Fatal(res.Body)
	}
	for _, invalid := range []string{`["1","2","3","4","5","6"]`, `["` + strings.Repeat("鸟", 41) + `"]`, `[1]`, `null`} {
		if r := f.req("PATCH", "/api/avatars/a1", `{"interests":`+invalid+`}`, "a"); r.Code != 400 {
			t.Fatal(r.Code)
		}
	}
	if r := f.start(); r.Code != 201 {
		t.Fatal(r.Code, r.Body)
	}
	for _, key := range []string{"name", "voiceId", "hostMode"} {
		if r := f.req("PATCH", "/api/avatars/a1", `{"`+key+`":"fixed"}`, "a"); r.Code != 409 {
			t.Fatal(key, r.Code)
		}
	}
	if r := f.req("PATCH", "/api/avatars/a1", `{"persona":"new","interests":["读书"]}`, "a"); r.Code != 200 {
		t.Fatal(r.Code)
	}
	if a.get("avatars", "a1")["revision"] != float64(2) {
		t.Fatal("revision was not persisted")
	}
}

func TestHostPlayedContext(t *testing.T) {
	f := newFixture(t)
	if r := f.start(); r.Code != 201 {
		t.Fatal(r.Code)
	}
	sid := str(f.a.current(), "id")
	now := f.a.c.Clock()
	result := TaskResult{Seq: 1, TaskID: "task1", Text: "已播", CompletedAt: now, State: "completed"}
	if e := f.a.RecordPlayed(sid, result); e != nil {
		t.Fatal(e)
	}
	result.Text = "重复回执"
	if e := f.a.RecordPlayed(sid, result); e != nil {
		t.Fatal(e)
	}
	result.Seq = 2
	result.TaskID = "task2"
	result.State = "failed"
	if e := f.a.RecordPlayed(sid, result); e != nil {
		t.Fatal(e)
	}
	in, e := f.a.LoadHostContext("a1", sid)
	if e != nil || len(in.SpokenTurns) != 1 || in.SpokenTurns[0].Text != "已播" {
		t.Fatal(in, e)
	}
	if _, e = f.a.LoadHostContext("unknown", sid); e == nil {
		t.Fatal("unowned context")
	}
	if _, e = f.a.db.Exec(`INSERT INTO host_topics VALUES('t','a1','key','题目','[{"id":"a","text":"角度"}]','active',?)`, now.Unix()); e != nil {
		t.Fatal(e)
	}
	if _, e = f.a.db.Exec(`INSERT INTO host_runs(session_id,avatar_id,topic_id,angle_id) VALUES(?,'a1','t','a')`, sid); e != nil {
		t.Fatal(e)
	}
	in, e = f.a.LoadHostContext("a1", sid)
	if e != nil || in.CurrentTopic == nil || in.CurrentAngleID != "a" || len(in.RecentTopicKeys) != 1 {
		t.Fatal(in, e)
	}
}
