package app

import (
	"context"
	"fmt"
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

const directorSpeech = "如果把树叶放在白纸上，细细看它边缘的起伏，会发现一片叶子也有自己的轮廓。换个方向再看，也许能找到另一种有趣的形状。"

func approvedDirectorResponse(system string) string {
	if system == auditRules {
		return `{"allow":true,"category":"safe"}`
	}
	return `{"action":"speak","text":"` + directorSpeech + `"}`
}

func TestLateGenerationCannotCrossSession(t *testing.T) {
	for _, transition := range []string{"pause-resume", "replace"} {
		t.Run(transition, func(t *testing.T) {
			f, sid := directorFixture(t)
			entered, release := make(chan struct{}), make(chan struct{})
			modelFixture(t, f.a, func(system string) string {
				if system == auditRules {
					close(entered)
					<-release
				}
				return approvedDirectorResponse(system)
			})
			done := make(chan error, 1)
			go func() { done <- f.a.CompanionTick(context.Background(), sid) }()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				close(release)
				t.Fatal("valid speech did not reach audit")
			}
			if transition == "pause-resume" {
				for _, payload := range []string{`{"enabled":false}`, `{"enabled":true}`} {
					if r := f.req("PATCH", "/api/live/"+sid+"/host", payload, "a"); r.Code != 200 {
						close(release)
						t.Fatal(r.Body.String())
					}
				}
				if str(f.a.current(), "id") != sid {
					close(release)
					t.Fatal("session changed")
				}
			} else {
				if r := f.req("POST", "/api/live/"+sid+"/stop", "", "a"); r.Code != 200 {
					close(release)
					t.Fatal(r.Body.String())
				}
				r := f.req("POST", "/api/live", `{"avatarId":"a1","lookId":"l1","platform":"douyin","hostMode":"companion"}`, "a")
				if r.Code != 201 || str(f.a.current(), "id") == sid {
					close(release)
					t.Fatal(r.Body.String())
				}
				if r = f.req("PATCH", "/api/live/"+str(f.a.current(), "id")+"/host", `{"enabled":true}`, "a"); r.Code != 200 {
					close(release)
					t.Fatal(r.Body.String())
				}
			}
			close(release)
			<-done
			if len(f.tasks) != 0 {
				t.Fatal("stale draft submitted", f.tasks)
			}
		})
	}
}

func TestManualStopAfterHour(t *testing.T) {
	f, sid := directorFixture(t)
	later := f.a.c.Clock().Add(65 * time.Minute)
	f.a.c.Clock = func() time.Time { return later }
	modelFixture(t, f.a, approvedDirectorResponse)
	if err := f.a.CompanionTick(context.Background(), sid); err != nil {
		t.Fatal(err)
	}
	if len(f.tasks) != 1 || str(f.tasks[0], "text") != directorSpeech || f.a.current()["status"] != "live" {
		t.Fatal(f.tasks, f.a.current())
	}
	if r := f.req("POST", "/api/live/"+sid+"/stop", "", "a"); r.Code != 200 || f.a.current() != nil {
		t.Fatal(r.Body.String())
	}
}

func TestDirectorFailureBackoff(t *testing.T) {
	f, sid := directorFixture(t)
	now := f.a.c.Clock()
	f.a.c.Clock = func() time.Time { return now }
	modelFixture(t, f.a, func(string) string { return `{"action":"skip"}` })
	for _, seconds := range []int{5, 15, 30, 60, 60} {
		if err := f.a.CompanionTick(context.Background(), sid); err == nil {
			t.Fatal("missing generation failure")
		}
		status := f.a.CompanionStatus(sid)
		if status.State != "degraded" || status.LastError == "" || status.NextEligibleAt == nil || !status.NextEligibleAt.Equal(now.Add(time.Duration(seconds)*time.Second)) {
			t.Fatal(status)
		}
		if err := f.a.CompanionTick(context.Background(), sid); err != nil {
			t.Fatal("tick retried early", err)
		}
		now = *status.NextEligibleAt
	}
	r := f.req("GET", "/api/live/"+sid+"/host-status", "", "a")
	if r.Code != 200 || !strings.Contains(r.Body.String(), "degraded") || len(f.tasks) != 0 {
		t.Fatal(r.Body.String(), f.tasks)
	}
}

func TestDirectorRefillsQuarantinedReserve(t *testing.T) {
	for _, count := range []int{4, 36} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f, sid := directorFixture(t)
			for i := 4; i < count; i++ {
				k := fmt.Sprintf("held%d", i)
				_, e := f.a.db.Exec(`INSERT INTO host_topics SELECT ?,avatar_id,?,title,angles,state,created_at FROM host_topics WHERE id='a'`, k, k)
				if e != nil {
					t.Fatal(e)
				}
			}
			topics, e := f.a.listTopics("a1")
			if e != nil {
				t.Fatal(e)
			}
			for _, topic := range topics {
				if e := f.a.RegisterPlayback(sid, "lost-"+topic.ID, "host", topic.ID, "one"); e != nil {
					t.Fatal(e)
				}
			}
			if e := f.a.ApplyTaskResults(sid, TaskResultsPage{Results: []TaskResult{{Seq: 3, TaskID: "other", State: "failed"}}, NextSeq: 3, Truncated: true}); e != nil {
				t.Fatal(e)
			}
			modelFixture(t, f.a, func(system string) string {
				if strings.Contains(system, "topics 为12个") {
					return topicFixture()
				}
				return approvedDirectorResponse(system)
			})
			if e := f.a.CompanionTick(context.Background(), sid); e != nil {
				t.Fatal(e)
			}
			if len(f.tasks) != 1 || str(f.tasks[0], "text") != directorSpeech {
				t.Fatal(f.tasks)
			}
			var unknown int
			if e = f.a.db.QueryRow("SELECT count(*) FROM host_tasks WHERE session_id=? AND state='unknown'", sid).Scan(&unknown); e != nil || unknown != count {
				t.Fatal(unknown, e)
			}
			in, e := f.a.LoadHostContext("a1", sid)
			if e != nil || in.CurrentAngleID != "" {
				t.Fatal(in, e)
			}
			if e = f.a.RecordPlayed(sid, TaskResult{Seq: 4, TaskID: "lost-a", State: "completed", Text: "后来收到的完成回执", CompletedAt: f.a.c.Clock()}); e != nil {
				t.Fatal(e)
			}
			in, e = f.a.LoadHostContext("a1", sid)
			if e != nil || in.CurrentTopic == nil || in.CurrentTopic.ID != "a" || in.CurrentAngleID != "one" {
				t.Fatal(in, e)
			}
		})
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

func TestLateFailuresRespectReadyCap(t *testing.T) {
	f, sid := directorFixture(t)
	for i := 4; i < 36; i++ {
		k := fmt.Sprintf("quarantined%d", i)
		if _, err := f.a.db.Exec(`INSERT INTO host_topics SELECT ?,avatar_id,?,title,angles,state,? FROM host_topics WHERE id='a'`, k, k, f.a.c.Clock().Unix()); err != nil {
			t.Fatal(err)
		}
	}
	originals, err := f.a.listTopics("a1")
	if err != nil {
		t.Fatal(err)
	}
	for _, topic := range originals {
		if err := f.a.RegisterPlayback(sid, "lost-"+topic.ID, "host", topic.ID, "one"); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.a.ApplyTaskResults(sid, TaskResultsPage{Results: []TaskResult{{Seq: 3, TaskID: "other", State: "failed"}}, NextSeq: 3, Truncated: true}); err != nil {
		t.Fatal(err)
	}
	modelFixture(t, f.a, func(sys string) string {
		if sys == auditRules {
			return approvedDirectorResponse(sys)
		}
		return topicFixture()
	})
	in, err := f.a.LoadHostContext("a1", sid)
	if err != nil {
		t.Fatal(err)
	}
	added, err := f.a.BuildTopics(context.Background(), in)
	if err != nil || len(added) != 12 {
		t.Fatal(len(added), err)
	}
	page := TaskResultsPage{NextSeq: 39}
	for i, topic := range originals {
		page.Results = append(page.Results, TaskResult{Seq: int64(i + 4), TaskID: "lost-" + topic.ID, State: "failed"})
	}
	for i := 0; i < 2; i++ {
		if err := f.a.ApplyTaskResults(sid, page); err != nil {
			t.Fatal(err)
		}
		var usable, failed int
		if err := f.a.db.QueryRow(readyHostTopicCount, "a1").Scan(&usable); err != nil || usable != 36 {
			t.Fatal("usable", usable, err)
		}
		if err := f.a.db.QueryRow("SELECT count(*) FROM host_tasks WHERE session_id=? AND state='failed'", sid).Scan(&failed); err != nil || failed != 36 {
			t.Fatal("failure evidence", failed, err)
		}
		for _, topic := range added {
			var state string
			if err := f.a.db.QueryRow("SELECT state FROM host_topics WHERE id=?", topic.ID).Scan(&state); err != nil || state != "ready" {
				t.Fatal(state, err)
			}
		}
		in, err = f.a.LoadHostContext("a1", sid)
		if err != nil || in.CurrentTopic != nil || len(in.SpokenTurns) != 0 {
			t.Fatal(in, err)
		}
	}
}
