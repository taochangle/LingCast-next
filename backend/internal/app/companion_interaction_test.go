package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCommentStormBounded(t *testing.T) {
	var turns []ViewerTurn
	for i := 0; i < 25; i++ {
		turns = appendViewerTurn(turns, ViewerTurn{Kind: "comment", Text: strings.Repeat("x", i)})
	}
	if len(turns) != 20 || len(turns[0].Text) != 5 {
		t.Fatal(turns)
	}
	f := newFixture(t)
	d, err := f.a.BuildInteractionReply(context.Background(), HostInput{}, ViewerTurn{Kind: "comment", Text: strings.Repeat("界", 501)})
	if err != nil || d.Action != "skip" {
		t.Fatal(d, err)
	}
}

func replyFixture(w http.ResponseWriter, audit bool) {
	content := `{"action":"speak","text":"我们接着聊。"}`
	if audit {
		content = `{"allow":true,"category":"safe"}`
	}
	write(w, 200, object{"choices": []object{{"message": object{"content": content}}}})
}

func collectTurns(t *testing.T, f *fixture, sid, token string, events []object) collectorAck {
	t.Helper()
	b, e := json.Marshal(object{"sessionId": sid, "platform": "douyin", "events": events})
	if e != nil {
		t.Fatal(e)
	}
	return readAck(t, f.req("POST", "/api/collector/events", string(b), token))
}
func sessionForReplies(t *testing.T, f *fixture, mode string) (string, string) {
	t.Helper()
	w := f.req("POST", "/api/live", fmt.Sprintf(`{"avatarId":"a1","lookId":"l1","platform":"douyin","hostMode":%q}`, mode), "a")
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	var started object
	if err := json.Unmarshal(w.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	return str(f.a.current(), "id"), str(started, "collectorToken")
}

func TestCommentStormQueueAndExpiry(t *testing.T) {
	f := newFixture(t)
	now := f.a.c.Clock()
	var seconds atomic.Int64
	f.a.c.Clock = func() time.Time { return now.Add(time.Duration(seconds.Load()) * time.Second) }
	sid, token := sessionForReplies(t, f, "fixed")
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	modelFixture(t, f.a, func(sys string) string {
		if sys == auditRules {
			return `{"allow":true,"category":"safe"}`
		}
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return `{"action":"speak","text":"接话。"}`
	})
	var events []object
	for i := 0; i < 25; i++ {
		events = append(events, object{"id": fmt.Sprint(i), "kind": "comment", "text": fmt.Sprint(i)})
	}
	finished := make(chan collectorAck, 1)
	go func() { finished <- collectTurns(t, f, sid, token, events) }()
	<-entered
	f.a.mu.Lock()
	depth := len(f.a.replies.queue)
	f.a.mu.Unlock()
	var overflow int
	e := f.a.db.QueryRow("SELECT COUNT(*) FROM events WHERE state='done'").Scan(&overflow)
	seconds.Store(31)
	close(release)
	ack := <-finished
	if e != nil || depth != 20 || overflow != 4 || calls.Load() != 1 || len(ack.Processed) != 25 || ack.Accepted != 25 || len(f.tasks) != 0 {
		t.Fatal(depth, overflow, calls.Load(), ack, f.tasks, e)
	}
}
func TestFixedScriptsRemainVerbatim(t *testing.T) {
	f := newFixture(t)
	sid, token := sessionForReplies(t, f, "companion")
	var events []object
	for _, k := range []string{"comment", "welcome", "gift", "like"} {
		text := "  原稿  " + k
		if err := f.a.put("scripts", object{"id": k, "avatarId": "a1", "kind": k, "text": text}); err != nil {
			t.Fatal(err)
		}
		kind := k
		if kind == "welcome" {
			kind = "enter"
		}
		events = append(events, object{"id": k, "kind": kind, "nickname": "nickname", "text": "different"})
	}
	ack := collectTurns(t, f, sid, token, events)
	if ack.Accepted != 4 || len(f.tasks) != 4 {
		t.Fatal(ack, f.tasks)
	}
	for _, task := range f.tasks {
		if task["text"] != "  原稿  "+str(task, "id") {
			t.Fatal(task)
		}
	}
}
func TestInteractionContinuesTopic(t *testing.T) {
	f := newFixture(t)
	sid, token := sessionForReplies(t, f, "companion")
	for i := 0; i < 5; i++ {
		_, e := f.a.db.Exec(`INSERT INTO host_topics VALUES(?, 'a1',?,?,'[{"id":"one","text":"开头"},{"id":"two","text":"接下来"}]','ready',1)`, fmt.Sprint(i), fmt.Sprint(i), "窗边植物"+fmt.Sprint(i))
		if e != nil {
			t.Fatal(e)
		}
	}
	if e := f.a.RegisterPlayback(sid, "intro", "host", "0", "one"); e != nil {
		t.Fatal(e)
	}
	receipt := func(seq int64, id, text string) {
		t.Helper()
		if e := f.a.RecordPlayed(sid, TaskResult{Seq: seq, TaskID: id, State: "completed", Text: text, CompletedAt: f.a.c.Clock()}); e != nil {
			t.Fatal(e)
		}
	}
	receipt(1, "intro", "刚才聊到窗台的树影。")
	hostText := strings.Repeat("窗边叶片的纹理值得细看", 4) + "。"
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Messages []struct{ Content string } }
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Messages[0].Content == auditRules {
			replyFixture(w, true)
			return
		}
		var data struct {
			Source       string
			CurrentTopic *HostTopic
			ViewerTurns  []ViewerTurn
			SpokenTurns  []SpokenTurn
		}
		if e := json.Unmarshal([]byte(req.Messages[1].Content), &data); e != nil {
			t.Error(e)
		}
		calls++
		if data.CurrentTopic == nil || data.CurrentTopic.ID != "0" || len(data.SpokenTurns) != calls || len(data.ViewerTurns) != min(calls, 2) {
			t.Errorf("call %d: %+v", calls, data)
		}
		if calls >= 2 && data.ViewerTurns[0].Text != "叶子是什么形状？" {
			t.Error(data)
		}
		if calls < 3 {
			replyFixture(w, false)
			return
		}
		if data.Source != "host" {
			t.Error(data)
		}
		write(w, 200, object{"choices": []object{{"message": object{"content": fmt.Sprintf(`{"action":"speak","text":%q}`, hostText)}}}})
	}))
	defer server.Close()
	f.a.c.LLMURL = server.URL
	for i, question := range []string{"叶子是什么形状？", "那颜色呢？"} {
		id := fmt.Sprint(i)
		ack := collectTurns(t, f, sid, token, []object{{"id": id, "kind": "comment", "text": question}})
		if ack.Accepted != 1 || len(f.tasks) != i+1 {
			t.Fatal(ack, f.tasks)
		}
		in, e := f.a.LoadHostContext("a1", sid)
		if e != nil || len(in.SpokenTurns) != i+1 {
			t.Fatal(in, e)
		}
		receipt(int64(i+2), id, str(f.tasks[i], "text"))
	}
	f.req("PATCH", "/api/live/"+sid+"/host", `{"enabled":true}`, "a")
	if e := f.a.CompanionTick(context.Background(), sid); e != nil {
		t.Fatal(e)
	}
	if calls != 3 || len(f.tasks) != 3 || f.tasks[2]["text"] != hostText {
		t.Fatal(calls, f.tasks)
	}
}

func TestStaleInteractionNeverSubmits(t *testing.T) {
	for _, change := range []string{"persona", "session"} {
		t.Run(change, func(t *testing.T) {
			f := newFixture(t)
			sid, token := sessionForReplies(t, f, "companion")
			entered, release := make(chan struct{}), make(chan struct{})
			modelFixture(t, f.a, func(sys string) string {
				if sys == auditRules {
					close(entered)
					<-release
					return `{"allow":true,"category":"safe"}`
				}
				return `{"action":"speak","text":"原先的人设。"}`
			})
			done := make(chan collectorAck, 1)
			go func() {
				done <- collectTurns(t, f, sid, token, []object{{"id": "inflight", "kind": "comment", "text": "你好"}})
			}()
			<-entered
			if change == "session" {
				w := f.req("POST", "/api/live/"+sid+"/stop", "", "a")
				if w.Code != 200 {
					close(release)
					t.Fatal(w.Code, w.Body.String())
				}
				sessionForReplies(t, f, "companion")
			} else {
				w := f.req("PATCH", "/api/avatars/a1", `{"persona":"新的设定"}`, "a")
				if w.Code != 200 {
					close(release)
					t.Fatal(w.Code, w.Body.String())
				}
			}
			close(release)
			ack := <-done
			if len(f.tasks) != 0 {
				t.Fatal(f.tasks)
			}
			if change == "persona" && len(ack.Processed) != 1 {
				t.Fatal(ack)
			}
		})
	}
}

func TestExpiredRedeliveryKeepsFirstArrival(t *testing.T) {
	f := newFixture(t)
	now := f.a.c.Clock()
	var offset atomic.Int64
	f.a.c.Clock = func() time.Time { return now.Add(time.Duration(offset.Load()) * time.Second) }
	sid, token := sessionForReplies(t, f, "fixed")
	var hits atomic.Int64
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); offset.Store(19); failHTTP(w) }))
	defer model.Close()
	f.a.c.LLMURL = model.URL
	events := []object{{"id": "stable", "kind": "comment", "text": "窗边的叶子"}}
	ack := collectTurns(t, f, sid, token, events)
	if len(ack.Retry) != 1 {
		t.Fatal(ack)
	}
	offset.Store(35)
	ack = collectTurns(t, f, sid, token, events)
	if len(ack.Processed) != 1 || hits.Load() != 1 || len(f.tasks) != 0 {
		t.Fatal(ack, hits.Load(), f.tasks)
	}
}

func TestScriptChangeAcrossFailedSubmission(t *testing.T) {
	for _, initialFixed := range []bool{false, true} {
		t.Run(fmt.Sprint(initialFixed), func(t *testing.T) {
			f := newFixture(t)
			sid, token := sessionForReplies(t, f, "fixed")
			setFixed := func() {
				if e := f.a.put("scripts", object{"id": "fixed", "avatarId": "a1", "kind": "comment", "text": "固定原文"}); e != nil {
					t.Fatal(e)
				}
			}
			if initialFixed {
				setFixed()
			}
			var posts, models atomic.Int64
			modelFixture(t, f.a, func(sys string) string {
				models.Add(1)
				if sys == auditRules {
					return `{"allow":true,"category":"safe"}`
				}
				return `{"action":"speak","text":"窗边的树影。"}`
			})
			worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { posts.Add(1); failHTTP(w) }))
			defer worker.Close()
			f.a.c.WorkerURL = worker.URL
			events := []object{{"id": "stable", "kind": "comment", "text": "窗边的树影"}}
			ack := collectTurns(t, f, sid, token, events)
			if len(ack.Retry) != 1 || posts.Load() != 1 {
				t.Fatal(ack, posts.Load())
			}
			if initialFixed {
				_, e := f.a.db.Exec("DELETE FROM objects WHERE kind='scripts' AND id='fixed'")
				if e != nil {
					t.Fatal(e)
				}
			} else {
				setFixed()
			}
			before := models.Load()
			ack = collectTurns(t, f, sid, token, events)
			if len(ack.Processed) != 1 || posts.Load() != 1 || models.Load() != before {
				t.Fatal(ack, posts.Load(), models.Load())
			}
			ack = collectTurns(t, f, sid, token, events)
			if ack.Accepted != 0 || len(ack.Processed) != 1 {
				t.Fatal(ack)
			}
		})
	}
}

func TestCollectorEventAgeMigration(t *testing.T) {
	for _, legacyState := range []bool{false, true} {
		t.Run(fmt.Sprint(legacyState), func(t *testing.T) {
			f := newFixture(t)
			schema := "session_id TEXT,id TEXT,PRIMARY KEY(session_id,id)"
			if legacyState {
				schema = "session_id TEXT,id TEXT,state TEXT NOT NULL DEFAULT 'done' CHECK(state IN ('pending','done')),PRIMARY KEY(session_id,id)"
			}
			if _, err := f.a.db.Exec("DROP TABLE events; CREATE TABLE events(" + schema + "); INSERT INTO events(session_id,id) VALUES('sid','done')"); err != nil {
				t.Fatal(err)
			}
			if legacyState {
				if _, err := f.a.db.Exec("INSERT INTO events VALUES('sid','working','pending')"); err != nil {
					t.Fatal(err)
				}
			}
			if err := migrateCollectorEvents(f.a.db); err != nil {
				t.Fatal(err)
			}
			var state string
			if err := f.a.db.QueryRow("SELECT state FROM events WHERE id='done'").Scan(&state); err != nil || state != "done" {
				t.Fatal(state, err)
			}
			if legacyState {
				if err := f.a.db.QueryRow("SELECT state FROM events WHERE id='working'").Scan(&state); err != nil || state != "retry" {
					t.Fatal(state, err)
				}
			}
			if _, err := f.a.db.Exec("INSERT INTO events(session_id,id,state,first_arrival) VALUES('sid','lost','pending',123)"); err != nil {
				t.Fatal(err)
			}
			if err := migrateCollectorEvents(f.a.db); err != nil {
				t.Fatal(err)
			}
			var arrival int64
			if err := f.a.db.QueryRow("SELECT state,first_arrival FROM events WHERE id='lost'").Scan(&state, &arrival); err != nil || state != "retry" || arrival != 123 {
				t.Fatal(state, arrival, err)
			}
		})
	}
}

func TestRetryPreemptsApprovedHostDraft(t *testing.T) {
	f := newFixture(t)
	origin := f.a.c.Clock()
	var seconds atomic.Int64
	f.a.c.Clock = func() time.Time { return origin.Add(time.Duration(seconds.Load()) * time.Second) }
	sid, token := sessionForReplies(t, f, "companion")
	for _, key := range []string{"a", "b", "c", "d"} {
		if _, e := f.a.db.Exec(`INSERT INTO host_topics VALUES(?,'a1',?,?,'[{"id":"one","text":"树影"}]','ready',1)`, key, key, key); e != nil {
			t.Fatal(e)
		}
	}
	if w := f.req("PATCH", "/api/live/"+sid+"/host", `{"enabled":true}`, "a"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	hostReady, hostRelease := make(chan struct{}), make(chan struct{})
	replyReady, replyRelease := make(chan struct{}), make(chan struct{})
	releaseHost := sync.OnceFunc(func() { close(hostRelease) })
	releaseReply := sync.OnceFunc(func() { close(replyRelease) })
	var checks, replyCalls atomic.Int64
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/status" && checks.Add(1) == 2 {
			close(hostReady)
			<-hostRelease
		}
		f.worker.Config.Handler.ServeHTTP(w, r)
	}))
	defer worker.Close()
	defer releaseHost()
	f.a.c.WorkerURL = worker.URL
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Messages []struct{ Content string } }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if req.Messages[0].Content == auditRules {
			replyFixture(w, true)
			return
		}
		var input struct{ Source string }
		if err := json.Unmarshal([]byte(req.Messages[1].Content), &input); err != nil {
			t.Error(err)
			return
		}
		if input.Source == "interaction" {
			if replyCalls.Add(1) == 1 {
				failHTTP(w)
				return
			}
			close(replyReady)
			<-replyRelease
			replyFixture(w, false)
			return
		}
		write(w, 200, object{"choices": []object{{"message": object{"content": approvedDirectorResponse(req.Messages[0].Content)}}}})
	}))
	defer model.Close()
	defer releaseReply()
	f.a.c.LLMURL = model.URL
	events := []object{{"id": "retrying", "kind": "comment", "text": "聊聊叶片"}}
	seconds.Store(2)
	if ack := collectTurns(t, f, sid, token, events); len(ack.Retry) != 1 {
		t.Fatal(ack)
	}
	hostDone := make(chan error, 1)
	go func() { hostDone <- f.a.CompanionTick(context.Background(), sid) }()
	select {
	case <-hostReady:
	case <-time.After(2 * time.Second):
		t.Fatal("approved host draft did not reach final worker check")
	}
	seconds.Store(12)
	result := make(chan collectorAck, 1)
	go func() { result <- collectTurns(t, f, sid, token, events) }()
	select {
	case <-replyReady:
	case <-time.After(2 * time.Second):
		t.Fatal("interaction retry did not enter generation")
	}
	releaseHost()
	select {
	case <-hostDone:
	case <-time.After(2 * time.Second):
		t.Fatal("host tick did not finish")
	}
	f.a.mu.Lock()
	lastViewer := f.a.director.lastViewer
	f.a.mu.Unlock()
	f.mu.Lock()
	queued := len(f.tasks)
	f.mu.Unlock()
	releaseReply()
	ack := <-result
	if queued != 0 || !lastViewer.Equal(origin.Add(2*time.Second)) || ack.Accepted != 1 || len(f.tasks) != 1 || f.tasks[0]["id"] != "retrying" {
		t.Fatal(queued, lastViewer, ack, f.tasks)
	}
}

func TestInteractionClassOutageBackoff(t *testing.T) {
	f := newFixture(t)
	sid, token := sessionForReplies(t, f, "companion")
	now := f.a.c.Clock()
	f.a.c.Clock = func() time.Time { return now }
	var calls atomic.Int64
	var healthy atomic.Bool
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			calls.Add(1)
			failHTTP(w)
			return
		}
		var req struct{ Messages []struct{ Content string } }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if req.Messages[0].Content != auditRules {
			calls.Add(1)
		}
		replyFixture(w, req.Messages[0].Content == auditRules)
	}))
	defer model.Close()
	f.a.c.LLMURL = model.URL
	var nextID int
	burst := func() []object {
		var events []object
		for i := 0; i < 4; i++ {
			nextID++
			events = append(events, object{"id": fmt.Sprint(nextID), "kind": "comment", "text": "叶片"})
		}
		return events
	}
	var last []object
	for i, seconds := range []int{5, 15, 30, 60, 60} {
		last = burst()
		ack := collectTurns(t, f, sid, token, last)
		if calls.Load() != int64(i+1) || len(ack.Retry) != 4 {
			t.Fatalf("burst %d: model calls=%d retry=%v", i, calls.Load(), ack.Retry)
		}
		now = now.Add(time.Duration(seconds)*time.Second - time.Millisecond)
		ack = collectTurns(t, f, sid, token, burst())
		if calls.Load() != int64(i+1) || len(ack.Retry) != 4 {
			t.Fatal("early requests", calls.Load(), ack)
		}
		now = now.Add(time.Millisecond)
	}
	healthy.Store(true)
	if ack := collectTurns(t, f, sid, token, last); len(ack.Processed) != 4 || calls.Load() != 5 {
		t.Fatal("expired redeliveries", ack, calls.Load())
	}
	fresh := burst()[:1]
	if ack := collectTurns(t, f, sid, token, fresh); len(ack.Processed) != 1 || len(f.tasks) != 1 || f.tasks[0]["id"] != fresh[0]["id"] {
		t.Fatal("recovery", ack, f.tasks)
	}
	healthy.Store(false)
	next := burst()[:1]
	if ack := collectTurns(t, f, sid, token, next); len(ack.Retry) != 1 || calls.Load() != 7 {
		t.Fatal("failure after recovery", ack, calls.Load())
	}
	now = now.Add(5 * time.Second)
	healthy.Store(true)
	if ack := collectTurns(t, f, sid, token, next); len(ack.Processed) != 1 || len(f.tasks) != 2 || f.tasks[1]["id"] != next[0]["id"] {
		t.Fatal("reset deadline/stable id", ack, f.tasks)
	}
}
