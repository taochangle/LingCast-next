package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"net/http/httptest"
	"strings"
	"testing"
)

func TestPersistence(t *testing.T) {
	c := Config{DataDir: t.TempDir(), AdminToken: "a", MediaToken: "m"}
	h, e := New(c)
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", "/api/avatars", strings.NewReader(`{"name":"A","persona":"P","voiceId":"v"}`))
	r.Header.Set("Authorization", "Bearer a")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	h, e = New(c)
	if e != nil {
		t.Fatal(e)
	}
	r = httptest.NewRequest("GET", "/api/avatars", nil)
	r.Header.Set("Authorization", "Bearer a")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"A"`) {
		t.Fatal(w.Body.String())
	}
}
func TestInvalidTimezone(t *testing.T) {
	_, e := New(Config{DataDir: t.TempDir(), AdminToken: "a", MediaToken: "m", Timezone: "not/a/zone"})
	if e == nil {
		t.Fatal("invalid timezone accepted")
	}
}

type fixture struct {
	a         *App
	h         http.Handler
	worker    *httptest.Server
	tasks     []object
	mu        sync.Mutex
	failStart bool
	started   chan struct{}
	release   chan struct{}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{}
	f.worker = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			write(w, 200, object{"ready": true})
		case r.URL.Path == "/sessions":
			if f.started != nil {
				select {
				case f.started <- struct{}{}:
				default:
				}
				<-f.release
			}
			if f.failStart {
				fail(w, 503, "not ready")
			} else {
				write(w, 200, object{"previewUrl": "/preview/x.flv"})
			}
		case strings.HasSuffix(r.URL.Path, "/tasks"):
			var o object
			_ = json.NewDecoder(r.Body).Decode(&o)
			f.mu.Lock()
			f.tasks = append(f.tasks, o)
			f.mu.Unlock()
			write(w, 200, object{})
		case r.URL.Path == "/status":
			s := f.a.current()
			sid := ""
			if s != nil {
				sid = str(s, "id")
			}
			write(w, 200, object{"sessionId": sid, "status": "live", "speaking": false, "queueDepth": 0})
		default:
			write(w, 200, object{})
		}
	}))
	t.Cleanup(f.worker.Close)
	h, e := New(Config{DataDir: t.TempDir(), AdminToken: "a", MediaToken: "m", WorkerURL: f.worker.URL, WorkerToken: "w", PublicBaseURL: "http://business", Clock: func() time.Time { return time.Date(2026, 11, 24, 10, 20, 30, 0, time.UTC) }})
	if e != nil {
		t.Fatal(e)
	}
	f.h = h
	f.a = h.(*App)
	t.Cleanup(func() { _ = f.a.Close() })
	for k, o := range map[string]object{"avatars": {"id": "a1", "name": "A", "persona": "P", "voiceId": "v"}, "looks": {"id": "l1", "avatarId": "a1", "imageId": "m1", "defaultIdleId": "i1"}, "idles": {"id": "i1", "lookId": "l1", "mediaId": "m2"}} {
		if e = f.a.put(k, o); e != nil {
			t.Fatal(e)
		}
	}
	_ = f.a.put("media", object{"id": "m1", "kind": "image", "sha256": "x"})
	_ = f.a.put("media", object{"id": "m2", "kind": "video", "sha256": "y"})
	return f
}
func (f *fixture) req(m, p, b, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(m, p, strings.NewReader(b))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}
func (f *fixture) start() *httptest.ResponseRecorder {
	return f.req("POST", "/api/live", `{"avatarId":"a1","lookId":"l1","platform":"douyin"}`, "a")
}
func TestConcurrentStart(t *testing.T) {
	f := newFixture(t)
	f.started = make(chan struct{}, 1)
	f.release = make(chan struct{})
	done := make(chan *httptest.ResponseRecorder)
	go func() { done <- f.start() }()
	<-f.started
	second := f.start()
	if second.Code != 409 {
		t.Fatal(second.Code, second.Body.String())
	}
	close(f.release)
	if first := <-done; first.Code != 201 {
		t.Fatal(first.Code, first.Body.String())
	}
}
func TestStartFailureReleasesSlot(t *testing.T) {
	f := newFixture(t)
	f.failStart = true
	if w := f.start(); w.Code != 503 {
		t.Fatal(w.Code)
	}
	if f.a.current() != nil {
		t.Fatal("slot leaked")
	}
	f.failStart = false
	if w := f.start(); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestCrossAvatarLookRejected(t *testing.T) {
	f := newFixture(t)
	_ = f.a.put("avatars", object{"id": "other"})
	w := f.req("POST", "/api/live", `{"avatarId":"other","lookId":"l1","platform":"douyin"}`, "a")
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}
func TestCollectorLifecycleAndPlatform(t *testing.T) {
	f := newFixture(t)
	w := f.start()
	var started struct {
		Session object `json:"session"`
		Token   string `json:"collectorToken"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &started)
	sid := str(started.Session, "id")
	body := fmt.Sprintf(`{"sessionId":%q,"platform":"xiaohongshu","events":[]}`, sid)
	if w = f.req("POST", "/api/collector/events", body, started.Token); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	body = strings.ReplaceAll(body, "xiaohongshu", "douyin")
	if w = f.req("POST", "/api/live/"+sid+"/stop", "", "a"); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w = f.req("POST", "/api/collector/events", body, started.Token); w.Code != 401 {
		t.Fatal(w.Code)
	}
}
func TestFixedScriptBypassesLLM(t *testing.T) {
	f := newFixture(t)
	_ = f.a.put("scripts", object{"id": "s1", "avatarId": "a1", "kind": "gift", "text": "exact 原文"})
	_ = f.start()
	s := f.a.current() // manual and saved fixed script must remain exact
	if w := f.req("POST", "/api/live/"+str(s, "id")+"/speak", `{"text":"原样 manual"}`, "a"); w.Code != 200 {
		t.Fatal(w.Code)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tasks[0]["text"] != "原样 manual" || f.tasks[0]["kind"] != "manual" {
		t.Fatal(f.tasks)
	}
}
func TestPromptUsesServerClock(t *testing.T) {
	f := newFixture(t)
	var prompt string
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var o struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&o)
		prompt = o.Messages[0].Content
		write(w, 200, object{"choices": []object{{"message": object{"content": "reply"}}}})
	}))
	defer llm.Close()
	f.a.c.LLMURL = llm.URL
	_, e := f.a.reply(context.Background(), "persona", "nick", "time?")
	if e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{"2026-11-24 18:20:30", "Asia/Shanghai", "Tuesday"} {
		if !strings.Contains(prompt, s) {
			t.Fatal(prompt)
		}
	}
}
func TestUploadInvalidAndOversized(t *testing.T) {
	f := newFixture(t)
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	part, _ := mw.CreateFormFile("file", "../../bad.mp4")
	_, _ = part.Write([]byte("not a media file"))
	_ = mw.Close()
	r := httptest.NewRequest("POST", "/api/media", &b)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Authorization", "Bearer a")
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(f.a.list("media")) != 2 {
		t.Fatal("bad media persisted")
	}
}
func TestHostPauseAndPriority(t *testing.T) {
	f := newFixture(t)
	_ = f.start()
	s := f.a.current()
	if s["hostEnabled"] != false {
		t.Fatal("host automatically enabled")
	}
	w := f.req("PATCH", "/api/live/"+str(s, "id")+"/host", `{"enabled":true}`, "a")
	if w.Code != 200 || f.a.current()["hostEnabled"] != true {
		t.Fatal(w.Code)
	}
	w = f.req("PATCH", "/api/live/"+str(s, "id")+"/host", `{"enabled":false}`, "a")
	if w.Code != 200 || f.a.current()["hostEnabled"] != false {
		t.Fatal(w.Code)
	}
}
func TestStopDuringStartCannotResurrect(t *testing.T) {
	f := newFixture(t)
	f.started = make(chan struct{}, 1)
	f.release = make(chan struct{})
	done := make(chan *httptest.ResponseRecorder)
	go func() { done <- f.start() }()
	<-f.started
	s := f.a.current()
	w := f.req("POST", "/api/live/"+str(s, "id")+"/stop", "", "a")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	close(f.release)
	if w = <-done; w.Code != 503 && w.Code != 409 {
		t.Fatal(w.Code)
	}
	if f.a.current() != nil {
		t.Fatal("cancelled start resurrected")
	}
}
func TestHostDoesNotOverlapOrRewrite(t *testing.T) {
	f := newFixture(t)
	_ = f.a.Close()
	_ = f.a.put("scripts", object{"id": "host1", "avatarId": "a1", "kind": "host", "text": "host exact"})
	_ = f.start()
	s := f.a.current()
	s["hostEnabled"] = true
	_ = f.a.put("sessions", s)
	f.a.hostTick()
	f.a.hostTick()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tasks) != 1 || f.tasks[0]["kind"] != "host" || f.tasks[0]["text"] != "host exact" {
		t.Fatal(f.tasks)
	}
}
func TestCollectorDedupAndFixedScript(t *testing.T) {
	f := newFixture(t)
	_ = f.a.put("scripts", object{"id": "s", "avatarId": "a1", "kind": "gift", "text": "谢谢 原文"})
	w := f.start()
	var result object
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	s := f.a.current()
	body := fmt.Sprintf(`{"sessionId":%q,"platform":"douyin","events":[{"id":"ev","kind":"gift","nickname":"<script>","text":"gift"}]}`, s["id"])
	for i := 0; i < 2; i++ {
		w = f.req("POST", "/api/collector/events", body, str(result, "collectorToken"))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tasks) != 1 || f.tasks[0]["text"] != "谢谢 原文" || f.tasks[0]["kind"] != "gift" {
		t.Fatal(f.tasks)
	}
}
func TestOversizedImageRejected(t *testing.T) {
	f := newFixture(t)
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	part, _ := mw.CreateFormFile("file", "image.png")
	_ = png.Encode(part, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	_, _ = io.CopyN(part, zeroReader{}, 31<<20)
	_ = mw.Close()
	r := httptest.NewRequest("POST", "/api/media", &b)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Authorization", "Bearer a")
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	files, _ := os.ReadDir(filepath.Join(f.a.c.DataDir, "media"))
	if len(files) != 0 {
		t.Fatal("upload left files")
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }
