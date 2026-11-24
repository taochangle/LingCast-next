package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func topicFixture() string {
	var ts []object
	for i := 0; i < 12; i++ {
		ts = append(ts, object{"title": fmt.Sprintf("生活话题%d", i), "angles": []string{"一种日常观察", "一种假设的场景", "从另一种观点讨论"}})
	}
	b, _ := json.Marshal(object{"topics": ts, "suggestedInterests": []string{"生活"}})
	return string(b)
}
func TestTopicReserveRefill(t *testing.T) {
	ts, _, e := parseTopics(topicFixture(), HostInput{})
	if e != nil || len(ts) != 12 {
		t.Fatal(ts, e)
	}
	if _, _, e = parseTopics(topicFixture(), HostInput{RecentTopicKeys: []string{ts[0].Key}}); e == nil {
		t.Fatal("accepted repeated title")
	}
}
func TestHostDraftBoundsAndDedup(t *testing.T) {
	s := strings.Repeat("山水河流丛林高原树木海岸", 4) + "河。"
	if !draftLength(s) || draftLength("太短") || draftLength(s+"。春。夏。秋。") || !nearSpeech(s, s+"呀") {
		t.Fatal("speech checks")
	}
}
func TestRecentTopicsExpire(t *testing.T) {
	f := newFixture(t)
	now := f.a.c.Clock()
	for _, owner := range []string{"a1", "other"} {
		_, e := f.a.db.Exec("INSERT INTO host_topics VALUES(?,?, 'key','title','[]','done',?)", owner, owner, now.Add(-8*24*time.Hour).Unix())
		if e != nil {
			t.Fatal(e)
		}
	}
	if e := f.a.pruneHostHistory(context.Background(), "a1"); e != nil {
		t.Fatal(e)
	}
	var n int
	f.a.db.QueryRow("SELECT COUNT(*) FROM host_topics WHERE id='other' OR avatar_id='a1'").Scan(&n)
	if n != 1 {
		t.Fatal(n)
	}
}

func modelFixture(t *testing.T, a *App, answer func(string) string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Messages []struct{ Content string } }
		if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
			t.Error(e)
			return
		}
		write(w, 200, object{"choices": []object{{"message": object{"content": answer(req.Messages[0].Content)}}}})
	}))
	t.Cleanup(server.Close)
	a.c.LLMURL = server.URL
}
func TestTopicReserveLimitsAndSingleflight(t *testing.T) {
	f := newFixture(t)
	in, e := f.a.LoadHostContext("a1", "")
	if e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	modelFixture(t, f.a, func(system string) string {
		if system == auditRules {
			return `{"allow":true,"category":"safe"}`
		}
		n := calls.Add(1)
		if n == 1 {
			close(entered)
			<-release
		}
		return strings.ReplaceAll(topicFixture(), "生活话题", fmt.Sprintf("批%d话题", n))
	})
	run := func() error { _, e := f.a.BuildTopics(context.Background(), in); return e }
	done := make(chan error, 2)
	go func() { done <- run() }()
	<-entered
	go func() { done <- run() }()
	close(release)
	for i := 0; i < 2; i++ {
		if e := <-done; e != nil {
			t.Fatal(e)
		}
	}
	if e := run(); e != nil || calls.Load() != 1 {
		t.Fatal(e, calls.Load())
	}
	if _, e = f.a.db.Exec("UPDATE host_topics SET state='done' WHERE rowid IN (SELECT rowid FROM host_topics LIMIT 9)"); e != nil {
		t.Fatal(e)
	}
	if e := run(); e != nil || calls.Load() != 2 {
		t.Fatal(e, calls.Load())
	}
	for i := 0; i < 4; i++ {
		if _, e = f.a.buildTopics(context.Background(), in, true); e != nil {
			t.Fatal(e)
		}
	}
	var n int
	f.a.db.QueryRow("SELECT COUNT(*) FROM host_topics WHERE state='ready'").Scan(&n)
	if n != 36 {
		t.Fatal(n)
	}
	if len(f.a.get("avatars", "a1")["suggestedInterests"].([]any)) != 1 {
		t.Fatal("missing suggestions")
	}
	if r := f.req("GET", "/api/avatars/a1/host-topics", "", ""); r.Code != 401 {
		t.Fatal(r.Code)
	}
	topics, e := f.a.listTopics("a1")
	if e != nil {
		t.Fatal(e)
	}
	if r := f.req("PATCH", "/api/host-topics/"+topics[0].ID, `{"state":"disabled"}`, "a"); r.Code != 200 {
		t.Fatal(r.Code, r.Body)
	}
	if f.a.validHostInput(in) {
		t.Fatal("edit did not invalidate old revision")
	}
}
func TestTopicReserveCancelled(t *testing.T) {
	f := newFixture(t)
	in, _ := f.a.LoadHostContext("a1", "")
	ctx, cancel := context.WithCancel(context.Background())
	modelFixture(t, f.a, func(s string) string { cancel(); return topicFixture() })
	if _, e := f.a.BuildTopics(ctx, in); e == nil {
		t.Fatal("expected cancellation")
	}
	ts, e := f.a.listTopics("a1")
	if e != nil || len(ts) != 0 {
		t.Fatal(e, ts)
	}
}
func TestUnverifiedFactsNotBroadcast(t *testing.T) {
	for _, content := range []string{"今日快讯，", "本地今天气温四十度，", "有研究表明百分之九十，", "请访问 https://example.invalid/"} {
		f := newFixture(t)
		in, _ := f.a.LoadHostContext("a1", "")
		in.CurrentTopic = &HostTopic{Angles: []HostAngle{{"x", "例子"}}}
		in.CurrentAngleID = "x"
		modelFixture(t, f.a, func(system string) string {
			if system == auditRules {
				return `{"allow":false,"category":"unsupported-factual-claim"}`
			}
			b, _ := json.Marshal(object{"action": "speak", "text": content + strings.Repeat("这是一段未经核实的内容", 4)})
			return string(b)
		})
		if d, e := f.a.BuildHostDraft(context.Background(), in); e == nil || d.Text != "" {
			t.Fatal(d, e)
		}
	}
}
func TestDraftRetriesOnceAndAudits(t *testing.T) {
	f := newFixture(t)
	in, _ := f.a.LoadHostContext("a1", "")
	in.CurrentTopic = &HostTopic{Angles: []HostAngle{{"x", "角度"}}}
	in.CurrentAngleID = "x"
	s := strings.Repeat("如果我们从不同角度一起欣赏窗外的树影", 3) + "。"
	var calls atomic.Int32
	modelFixture(t, f.a, func(sys string) string {
		if sys == auditRules {
			return `{"allow":true,"category":"safe"}`
		}
		if calls.Add(1) == 1 {
			return "bad"
		}
		b, _ := json.Marshal(object{"action": "speak", "text": s})
		return string(b)
	})
	d, e := f.a.BuildHostDraft(context.Background(), in)
	if e != nil || d.Text != s || calls.Load() != 2 {
		t.Fatal(d, e, calls.Load())
	}
	in.SpokenTurns = []SpokenTurn{{Text: s}}
	if _, e = f.a.BuildHostDraft(context.Background(), in); e == nil || calls.Load() != 4 {
		t.Fatal(e, calls.Load())
	}
}

func TestBoundedHostHistory(t *testing.T) {
	f := newFixture(t)
	now := f.a.c.Clock().Unix()
	tx, e := f.a.db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	for _, owner := range []string{"a1", "other"} {
		if _, e = tx.Exec("INSERT INTO host_runs(session_id,avatar_id) VALUES(?,?)", owner, owner); e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 205; i++ {
			if _, e = tx.Exec("INSERT INTO host_recent_keys VALUES(?,?,?)", owner, fmt.Sprint(i), now); e != nil {
				t.Fatal(e)
			}
			source := "host"
			if i >= 150 {
				source = "manual"
			}
			if _, e = tx.Exec("INSERT INTO host_turns VALUES(?,?,?,?,?,?)", owner, fmt.Sprint(i), source, "段", now, i); e != nil {
				t.Fatal(e)
			}
		}
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	if e = f.a.pruneHostHistory(context.Background(), "a1"); e != nil {
		t.Fatal(e)
	}
	for q, want := range map[string]int{"SELECT count(*) FROM host_recent_keys WHERE avatar_id='a1'": 200, "SELECT count(*) FROM host_turns WHERE session_id='a1' AND source='host'": 100, "SELECT count(*) FROM host_turns WHERE session_id='a1' AND source='manual'": 12, "SELECT count(*) FROM host_turns WHERE session_id='other'": 205} {
		var got int
		if e = f.a.db.QueryRow(q).Scan(&got); e != nil || got != want {
			t.Fatal(q, got, e)
		}
	}
}

func TestTopicBackoffAndStaleRevision(t *testing.T) {
	f := newFixture(t)
	in, _ := f.a.LoadHostContext("a1", "")
	now := f.a.c.Clock()
	f.a.c.Clock = func() time.Time { return now }
	var calls atomic.Int32
	modelFixture(t, f.a, func(s string) string { calls.Add(1); return "bad" })
	for _, delay := range []int{5, 15, 30, 60, 60} {
		before := calls.Load()
		if _, e := f.a.BuildTopics(context.Background(), in); e == nil || calls.Load() != before+2 {
			t.Fatal(e, calls.Load())
		}
		now = now.Add(time.Duration(delay)*time.Second - time.Millisecond)
		if _, e := f.a.BuildTopics(context.Background(), in); e != errBackoff || calls.Load() != before+2 {
			t.Fatal(e)
		}
		now = now.Add(time.Millisecond)
	}
	modelFixture(t, f.a, func(s string) string {
		if s == auditRules {
			return `{"allow":true,"category":"safe"}`
		}
		if r := f.req("PATCH", "/api/avatars/a1", `{"persona":"新的人设","interests":["明确指定"]}`, "a"); r.Code != 200 {
			t.Error(r.Body)
		}
		return topicFixture()
	})
	if _, e := f.a.BuildTopics(context.Background(), in); e != errStale {
		t.Fatal(e)
	}
	ts, _ := f.a.listTopics("a1")
	if len(ts) != 0 || f.a.get("avatars", "a1")["persona"] != "新的人设" {
		t.Fatal(ts)
	}
}
