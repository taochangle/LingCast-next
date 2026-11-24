package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHostPauseTiming(t *testing.T) {
	for _, tc := range []struct {
		sw, quiet bool
		lo, hi    int
	}{{false, false, 2, 5}, {true, false, 5, 10}, {true, true, 10, 20}} {
		for _, high := range []bool{false, true} {
			pick := func(n int) int {
				if high {
					return n - 1
				}
				return 0
			}
			want := tc.lo
			if high {
				want = tc.hi
			}
			if hostPause(tc.sw, tc.quiet, pick) != time.Duration(want)*time.Second {
				t.Fatal(tc)
			}
		}
	}
}
func directorFixture(t *testing.T) (*fixture, string) {
	t.Helper()
	f := newFixture(t)
	r := f.req("POST", "/api/live", `{"avatarId":"a1","lookId":"l1","platform":"douyin","hostMode":"companion"}`, "a")
	if r.Code != 201 {
		t.Fatal(r.Body.String())
	}
	sid := str(f.a.current(), "id")
	r = f.req("PATCH", "/api/live/"+sid+"/host", `{"enabled":true}`, "a")
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	for _, k := range []string{"a", "b", "c", "d"} {
		_, err := f.a.db.Exec(`INSERT INTO host_topics VALUES(?, 'a1',?,?,'[{"id":"one","text":"开头"},{"id":"two","text":"接下来"}]','ready',1)`, k, k, k)
		if err != nil {
			t.Fatal(err)
		}
	}
	return f, sid
}
func TestCompanionFlow(t *testing.T) {
	f, sid := directorFixture(t)
	var page TaskResultsPage
	now := f.a.c.Clock()
	var completed float64
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/task-results"):
			write(w, 200, page)
		case r.URL.Path == "/status":
			write(w, 200, object{"sessionId": sid, "status": "live", "lastTaskCompletedAt": completed})
		default:
			f.worker.Config.Handler.ServeHTTP(w, r)
		}
	}))
	defer service.Close()
	f.a.c.WorkerURL = service.URL
	picks := 0
	f.a.c.HostRandom = func(int) int { picks++; return 0 }
	speech := strings.Repeat("如果我们从不同角度一起欣赏窗外的树影", 3) + "。"
	modelFixture(t, f.a, func(sys string) string {
		if sys == auditRules {
			return `{"allow":true,"category":"safe"}`
		}
		return `{"action":"speak","text":"` + speech + `"}`
	})
	if err := f.a.CompanionTick(context.Background(), sid); err != nil {
		t.Fatal(err)
	}
	if len(f.tasks) != 1 {
		t.Fatal(f.tasks)
	}
	if err := f.a.CompanionTick(context.Background(), sid); err != nil {
		t.Fatal(err)
	}
	in, _ := f.a.LoadHostContext("a1", sid)
	if len(f.tasks) != 1 || in.CurrentAngleID != "" {
		t.Fatal(in)
	}
	page = TaskResultsPage{NextSeq: 1, Results: []TaskResult{{Seq: 1, TaskID: str(f.tasks[0], "id"), State: "completed", Text: speech, CompletedAt: now}}}
	completed = float64(now.Unix())
	for i := 0; i < 3; i++ {
		if err := f.a.CompanionTick(context.Background(), sid); err != nil {
			t.Fatal(err)
		}
		status := f.a.CompanionStatus(sid)
		if status.NextEligibleAt == nil || !status.NextEligibleAt.Equal(now.Add(2*time.Second)) || picks != 1 {
			t.Fatal(status, picks)
		}
	}
	in, _ = f.a.LoadHostContext("a1", sid)
	if in.CurrentAngleID != "one" {
		t.Fatal(in)
	}
	if err := f.a.selectHostAngle(&in); err != nil || in.CurrentAngleID != "two" {
		t.Fatal(in, err)
	}
}

func TestSlowLLMDoesNotBlockStop(t *testing.T) {
	f, sid := directorFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	modelFixture(t, f.a, func(string) string { close(entered); <-release; return `{"action":"skip"}` })
	defer close(release)
	done := make(chan error, 1)
	go func() { done <- f.a.CompanionTick(context.Background(), sid) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("model request never started")
	}
	stopped := make(chan int, 1)
	go func() { stopped <- f.req("POST", "/api/live/"+sid+"/stop", "", "a").Code }()
	select {
	case code := <-stopped:
		if code != 200 {
			t.Fatal(code)
		}
	case <-time.After(time.Second):
		t.Fatal("stop blocked on LLM")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("generation did not cancel")
	}
}
func TestLateGenerationCannotCrossSession(t *testing.T) {
	f, sid := directorFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	modelFixture(t, f.a, func(string) string { close(entered); <-release; return `{"action":"speak","text":"过期草稿"}` })
	done := make(chan error, 1)
	go func() { done <- f.a.CompanionTick(context.Background(), sid) }()
	<-entered
	f.req("PATCH", "/api/live/"+sid+"/host", `{"enabled":false}`, "a")
	f.req("POST", "/api/live/"+sid+"/stop", "", "a")
	if r := f.start(); r.Code != 201 {
		t.Fatal(r.Body.String())
	}
	close(release)
	<-done
	if len(f.tasks) != 0 || str(f.a.current(), "id") == sid {
		t.Fatal(f.tasks)
	}
}
func TestManualStopAfterHour(t *testing.T) {
	f, sid := directorFixture(t)
	later := f.a.c.Clock().Add(65 * time.Minute)
	f.a.c.Clock = func() time.Time { return later }
	modelFixture(t, f.a, func(string) string { return `{"action":"skip"}` })
	if err := f.a.CompanionTick(context.Background(), sid); err == nil {
		t.Fatal("missing generation failure")
	}
	status := f.a.CompanionStatus(sid)
	if status.State != "degraded" || status.LastError == "" || !status.NextEligibleAt.Equal(later.Add(5*time.Second)) || str(f.a.current(), "status") != "live" {
		t.Fatal(status)
	}
	r := f.req("GET", "/api/live/"+sid+"/host-status", "", "a")
	if r.Code != 200 || !strings.Contains(r.Body.String(), "degraded") {
		t.Fatal(r.Body.String())
	}
	if r = f.req("POST", "/api/live/"+sid+"/stop", "", "a"); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
}
func TestRestartDoesNotReplayUnknown(t *testing.T) {
	f, sid := directorFixture(t)
	if e := f.a.RegisterPlayback(sid, "lost", "host", "d", "one"); e != nil {
		t.Fatal(e)
	}
	page := TaskResultsPage{Results: []TaskResult{{Seq: 3, TaskID: "other", State: "failed"}}, NextSeq: 3, Truncated: true}
	if e := f.a.ApplyTaskResults(sid, page); e != nil {
		t.Fatal(e)
	}
	h, e := New(f.a.c)
	if e != nil {
		t.Fatal(e)
	}
	a := h.(*App)
	defer a.Close()
	in, e := a.LoadHostContext("a1", sid)
	if e != nil || in.CurrentAngleID != "" || len(in.SpokenTurns) > 0 {
		t.Fatal(in, e)
	}
	if e = a.selectHostAngle(&in); e != nil || in.CurrentTopic.ID == "d" && in.CurrentAngleID == "one" {
		t.Fatal(in, e)
	}
}
