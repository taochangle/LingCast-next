package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type collectorAck struct {
	Accepted  int      `json:"accepted"`
	Processed []string `json:"processedIds"`
	Retry     []string `json:"retryIds"`
}

func readAck(t *testing.T, w *httptest.ResponseRecorder) collectorAck {
	t.Helper()
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out collectorAck
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func TestCollectorPartialFailuresAndDurableAcknowledgement(t *testing.T) {
	f := newFixture(t)
	_ = f.a.Close()
	w := f.start()
	var started object
	_ = json.Unmarshal(w.Body.Bytes(), &started)
	sid := str(f.a.current(), "id")
	var fail atomic.Bool
	fail.Store(true)
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b object
		_ = json.NewDecoder(r.Body).Decode(&b)
		raw, _ := json.Marshal(b)
		if fail.Load() && strings.Contains(string(raw), "bad-llm") {
			failHTTP(w)
			return
		}
		replyFixture(w, strings.Contains(string(raw), auditRules))
	}))
	defer llm.Close()
	f.a.c.LLMURL = llm.URL
	var mu sync.Mutex
	calls := map[string]int{}
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var task object
		_ = json.NewDecoder(r.Body).Decode(&task)
		i := str(task, "id")
		mu.Lock()
		calls[i]++
		mu.Unlock()
		if fail.Load() && i == "bad-worker" {
			failHTTP(w)
			return
		}
		write(w, 200, object{})
	}))
	defer worker.Close()
	f.a.c.WorkerURL = worker.URL
	body := fmt.Sprintf(`{"sessionId":%q,"platform":"douyin","events":[{"id":"good","kind":"comment","text":"good"},{"id":"bad-llm","kind":"comment","text":"bad-llm"},{"id":"bad-worker","kind":"comment","text":"worker"}]}`, sid)
	token := str(started, "collectorToken")
	ack := readAck(t, f.req("POST", "/api/collector/events", body, token))
	if ack.Accepted != 1 || strings.Join(ack.Processed, ",") != "good" || strings.Join(ack.Retry, ",") != "bad-llm,bad-worker" {
		t.Fatal(ack)
	}
	fail.Store(false)
	ack = readAck(t, f.req("POST", "/api/collector/events", body, token))
	if ack.Accepted != 2 || strings.Join(ack.Processed, ",") != "good,bad-llm,bad-worker" || len(ack.Retry) != 0 {
		t.Fatal(ack)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["good"] != 1 || calls["bad-worker"] != 2 || calls["bad-llm"] != 1 {
		t.Fatal(calls)
	}
}
func failHTTP(w http.ResponseWriter) { write(w, 503, object{"error": "temporary"}) }
func TestCollectorPendingDuplicateRetries(t *testing.T) {
	f := newFixture(t)
	_ = f.a.Close()
	w := f.start()
	var started object
	_ = json.Unmarshal(w.Body.Bytes(), &started)
	sid := str(f.a.current(), "id")
	entered := make(chan struct{})
	release := make(chan struct{})
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Messages []struct{ Content string } }
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Messages[0].Content == auditRules {
			replyFixture(w, true)
			return
		}
		close(entered)
		<-release
		replyFixture(w, false)
	}))
	defer llm.Close()
	f.a.c.LLMURL = llm.URL
	body := fmt.Sprintf(`{"sessionId":%q,"platform":"douyin","events":[{"id":"pending","kind":"comment","text":"question"}]}`, sid)
	token := str(started, "collectorToken")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- f.req("POST", "/api/collector/events", body, token) }()
	<-entered
	ack := readAck(t, f.req("POST", "/api/collector/events", body, token))
	if ack.Accepted != 0 || len(ack.Processed) != 0 || strings.Join(ack.Retry, ",") != "pending" {
		close(release)
		<-done
		t.Fatal(ack)
	}
	close(release)
	ack = readAck(t, <-done)
	if strings.Join(ack.Processed, ",") != "pending" || len(ack.Retry) != 0 {
		t.Fatal(ack)
	}
	ack = readAck(t, f.req("POST", "/api/collector/events", body, token))
	if ack.Accepted != 0 || strings.Join(ack.Processed, ",") != "pending" {
		t.Fatal(ack)
	}
}
func TestStartupClearsPendingKeepsDone(t *testing.T) {
	f := newFixture(t)
	_ = f.a.Close()
	_ = f.start()
	sid := str(f.a.current(), "id")
	if _, e := f.a.db.Exec("INSERT INTO events(session_id,id,state) VALUES(?,?,'pending'),(?,?,'done')", sid, "pending", sid, "done"); e != nil {
		t.Fatal(e)
	}
	h, e := New(f.a.c)
	if e != nil {
		t.Fatal(e)
	}
	a := h.(*App)
	defer a.Close()
	var pending, done int
	_ = a.db.QueryRow("SELECT COUNT(*) FROM events WHERE state='pending'").Scan(&pending)
	_ = a.db.QueryRow("SELECT COUNT(*) FROM events WHERE state='done'").Scan(&done)
	if pending != 0 || done != 1 {
		t.Fatal(pending, done)
	}
}
func TestCollectorIntentionalSkipAcknowledged(t *testing.T) {
	f := newFixture(t)
	_ = f.a.Close()
	w := f.start()
	var started object
	_ = json.Unmarshal(w.Body.Bytes(), &started)
	body := fmt.Sprintf(`{"sessionId":%q,"platform":"douyin","events":[{"id":"no-script","kind":"like"}]}`, f.a.current()["id"])
	for i := 0; i < 2; i++ {
		ack := readAck(t, f.req("POST", "/api/collector/events", body, str(started, "collectorToken")))
		if strings.Join(ack.Processed, ",") != "no-script" || len(ack.Retry) != 0 || ack.Accepted != 1-i {
			t.Fatal(ack)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tasks) != 0 {
		t.Fatal("skip synthesized a task", f.tasks)
	}
}
