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
	f.a.Close()
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
	f.a.Close()
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
	f.a.Close()
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
			f.a.Close()
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
