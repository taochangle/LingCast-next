package app

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Each minute is a scheduling observation, not a minute of measured audio.
func TestCompanionSoak90Minutes(t *testing.T) {
	f := newFixture(t)
	sid, token := sessionForReplies(t, f, "companion")
	toggle := func(on bool) {
		t.Helper()
		r := f.req("PATCH", "/api/live/"+sid+"/host", fmt.Sprintf(`{"enabled":%t}`, on), "a")
		if r.Code != 200 {
			t.Fatal(r.Body.String())
		}
	}
	toggle(true)
	start := f.a.c.Clock()
	now := start
	f.a.c.Clock = func() time.Time { return now }
	f.a.c.HostRandom = func(int) int { return 0 }
	var page TaskResultsPage
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/task-results") {
			after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
			if after < page.NextSeq {
				write(w, 200, page)
			} else {
				write(w, 200, TaskResultsPage{NextSeq: after})
			}
			return
		}
		f.worker.Config.Handler.ServeHTTP(w, r)
	}))
	defer service.Close()
	f.a.c.WorkerURL = service.URL
	var active, calls, batches atomic.Int64
	var unavailable atomic.Bool
	modelFixture(t, f.a, func(sys string) string {
		if active.Add(1) > 1 {
			t.Error("overlapping model requests")
		}
		defer active.Add(-1)
		if sys == auditRules {
			return `{"allow":true,"category":"safe"}`
		}
		if strings.Contains(sys, "topics 为") {
			return strings.ReplaceAll(topicFixture(), "生活话题", fmt.Sprintf("生活主题%d组", batches.Add(1)))
		}
		n := calls.Add(1)
		if unavailable.Load() {
			return `malformed model response`
		}
		// The second candidate deliberately repeats the first completed turn.
		if n == 2 {
			n = 1
		}
		rng := rand.New(rand.NewSource(n))
		alphabet := []rune("溪水草木窗纸日月山川星空桌椅砖瓦沙石青蓝")
		var words strings.Builder
		for j := 0; j < 48; j++ {
			words.WriteRune(alphabet[rng.Intn(len(alphabet))])
		}
		return fmt.Sprintf(`{"action":"speak","text":%q}`, words.String()+"。")
	})
	var consumed int
	var seq int64
	completed := map[string]bool{}
	spoken := map[string]bool{}
	for minute := 0; minute <= 90; minute++ {
		now = start.Add(time.Duration(minute) * time.Minute)
		if minute == 30 {
			toggle(false)
		}
		if minute == 35 {
			toggle(true)
		}
		if minute == 45 {
			unavailable.Store(true)
		}
		if minute == 46 {
			unavailable.Store(false)
		}
		if minute == 20 {
			events := []object{}
			for i := 0; i < 25; i++ {
				events = append(events, object{"id": fmt.Sprint(i), "kind": "comment", "text": "今天聊什么"})
			}
			ack := collectTurns(t, f, sid, token, events)
			if len(ack.Processed) != 25 {
				t.Fatal(ack)
			}
		}
		page = TaskResultsPage{NextSeq: seq}
		for ; consumed < len(f.tasks); consumed++ {
			task := f.tasks[consumed]
			id := str(task, "id")
			seq++
			if minute == 60 {
				seq++
				page.Truncated = true
				page.Results = append(page.Results, TaskResult{Seq: seq, TaskID: "unregistered", State: "failed"})
				continue
			}
			state := "completed"
			if minute == 55 {
				state = "failed"
			} else {
				completed[id] = true
			}
			page.Results = append(page.Results, TaskResult{Seq: seq, TaskID: id, Text: str(task, "text"), State: state, CompletedAt: now})
		}
		page.NextSeq = seq
		before := len(f.tasks)
		err := f.a.CompanionTick(context.Background(), sid)
		if minute == 45 {
			status := f.a.CompanionStatus(sid)
			if err != nil || status.State != "waiting" || status.NextEligibleAt == nil || !status.NextEligibleAt.Equal(now.Add(2*time.Second)) {
				t.Fatal(err, status)
			}
			n := calls.Load()
			if f.a.CompanionTick(context.Background(), sid) != nil || calls.Load() != n {
				t.Fatal("early retry")
			}
		} else if err != nil {
			t.Fatal(minute, err)
		}
		if minute == 1 && calls.Load() != 3 {
			t.Fatal("duplicated candidate did not trigger one replacement", calls.Load())
		}
		want := 1
		if minute >= 30 && minute < 35 || minute == 45 {
			want = 0
		}
		if len(f.tasks)-before != want {
			t.Fatal(minute, len(f.tasks)-before, want)
		}
		if want == 1 {
			line := str(f.tasks[before], "text")
			if spoken[line] {
				t.Fatal("repeated output")
			}
			spoken[line] = true
		}
		in, e := f.a.LoadHostContext("a1", sid)
		if e != nil || len(in.SpokenTurns) > 12 || len(in.RecentTopicKeys) > 200 {
			t.Fatal(minute, e, in)
		}
		for _, turn := range in.SpokenTurns {
			if !completed[turn.TaskID] {
				t.Fatal("unconfirmed memory", turn.TaskID)
			}
		}
		if replies := f.a.replies; replies != nil && (len(replies.queue) > 20 || len(replies.history) > 20) {
			t.Fatal("unbounded interactions")
		}
		var ready, pending, invalid int
		if e = f.a.db.QueryRow("SELECT COUNT(*) FROM host_topics WHERE state='ready'").Scan(&ready); e != nil || ready > 36 {
			t.Fatal(ready, e)
		}
		if e = f.a.db.QueryRow("SELECT COUNT(*) FROM host_tasks WHERE source='host' AND state='pending'").Scan(&pending); e != nil || pending > 1 {
			t.Fatal(pending, e)
		}
		if e = f.a.db.QueryRow("SELECT COUNT(*) FROM host_turns t JOIN host_tasks h USING(session_id,task_id) WHERE h.state!='completed'").Scan(&invalid); e != nil || invalid != 0 {
			t.Fatal(invalid, e)
		}
	}
	var topics, unknown, failed int
	if err := f.a.db.QueryRow("SELECT COUNT(DISTINCT topic_id),SUM(state='unknown'),SUM(state='failed') FROM host_tasks WHERE source='host'").Scan(&topics, &unknown, &failed); err != nil || topics < 12 || unknown != 1 || failed != 1 || batches.Load() < 2 || str(f.a.current(), "status") != "live" {
		t.Fatal(topics, unknown, failed, batches.Load(), err)
	}
	t.Logf("90 simulated minutes: %d autonomous submissions, %d distinct topics, %d missing receipt, %d failed receipt", len(spoken), topics, unknown, failed)
}
