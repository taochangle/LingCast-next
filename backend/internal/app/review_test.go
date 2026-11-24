package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRecoverSlotAfterWorkerRestart(t *testing.T) {
	f := newFixture(t)
	if w := f.start(); w.Code != 201 {
		t.Fatal(w.Code)
	}
	_ = f.a.Close()
	idle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, object{"sessionId": nil, "status": "stopped", "preparing": false})
	}))
	defer idle.Close()
	c := f.a.c
	c.WorkerURL = idle.URL
	h, e := New(c)
	if e != nil {
		t.Fatal(e)
	}
	a := h.(*App)
	defer a.Close()
	if a.current() != nil {
		t.Fatal("confirmed idle worker retained stale slot")
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/live", nil)
	r.Header.Set("Authorization", "Bearer a")
	h.ServeHTTP(w, r)
	var out object
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["session"] != nil || out["lastSession"] == nil {
		t.Fatal(w.Body.String())
	}
}
func TestStopRecoversConfirmedIdleOnly(t *testing.T) {
	for _, preparing := range []bool{false, true} {
		t.Run(fmt.Sprint(preparing), func(t *testing.T) {
			f := newFixture(t)
			_ = f.start()
			_ = f.a.Close()
			s := f.a.current()
			idle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					fail(w, 409, "stale")
					return
				}
				write(w, 200, object{"sessionId": nil, "status": "stopped", "preparing": preparing})
			}))
			defer idle.Close()
			f.a.c.WorkerURL = idle.URL
			w := f.req("POST", "/api/live/"+str(s, "id")+"/stop", "", "a")
			if !preparing && (w.Code != 200 || f.a.current() != nil) {
				t.Fatal(w.Code, w.Body.String())
			}
			if preparing && (w.Code != 503 || f.a.current() == nil) {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}
func TestPersistenceFailuresAreNotSuccess(t *testing.T) {
	for _, action := range []string{"start", "collector-token", "host", "stop"} {
		t.Run(action, func(t *testing.T) {
			f := newFixture(t)
			_ = f.a.Close()
			path := "/api/live"
			method := "POST"
			body := `{"avatarId":"a1","lookId":"l1","platform":"douyin"}`
			if action != "start" {
				_ = f.start()
				path += "/" + str(f.a.current(), "id") + "/" + action
				body = ""
				if action == "host" {
					method = "PATCH"
					body = `{"enabled":true}`
				}
			}
			_, e := f.a.db.Exec(`CREATE TRIGGER fail_session_update BEFORE UPDATE ON objects WHEN OLD.kind='sessions' BEGIN SELECT RAISE(ABORT,'injected'); END`)
			if e != nil {
				t.Fatal(e)
			}
			w := f.req(method, path, body, "a")
			if w.Code < 500 {
				t.Fatal("persistence error reported success", w.Code, w.Body.String())
			}
		})
	}
}
func TestStopReleaseAtomic(t *testing.T) {
	f := newFixture(t)
	_ = f.a.Close()
	_ = f.start()
	sid := str(f.a.current(), "id")
	_, e := f.a.db.Exec(`CREATE TRIGGER fail_release BEFORE DELETE ON live_slot BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if e != nil {
		t.Fatal(e)
	}
	w := f.req("POST", "/api/live/"+sid+"/stop", "", "a")
	if w.Code != 500 {
		t.Fatal(w.Code, w.Body.String())
	}
	if s := f.a.current(); s == nil || s["status"] == "stopped" {
		t.Fatal("non-atomic terminal status", s)
	}
}
func TestPreviewIsSameOrigin(t *testing.T) {
	f := newFixture(t)
	_ = f.start()
	if !strings.HasPrefix(str(f.a.current(), "previewUrl"), "/api/live/") {
		t.Fatal(f.a.current())
	}
}
func TestSessionDedupDoesNotForget(t *testing.T) {
	f := newFixture(t)
	_ = f.a.Close()
	w := f.start()
	var result object
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	sid := str(f.a.current(), "id")
	tx, _ := f.a.db.Begin()
	for i := 0; i < 10000; i++ {
		_, _ = tx.Exec("INSERT INTO events(session_id,id) VALUES(?,?)", sid, fmt.Sprint(i))
	}
	_ = tx.Commit()
	body := fmt.Sprintf(`{"sessionId":%q,"platform":"douyin","events":[{"id":"new","kind":"like"}]}`, sid)
	w = f.req("POST", "/api/collector/events", body, str(result, "collectorToken"))
	if w.Code != 409 {
		t.Fatal("capacity should reject new IDs", w.Code, w.Body.String())
	}
	body = strings.Replace(body, `"id":"new"`, `"id":"0"`, 1)
	w = f.req("POST", "/api/collector/events", body, str(result, "collectorToken"))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"accepted":0`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestExactEventEnum(t *testing.T) {
	f := newFixture(t)
	w := f.start()
	var result object
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	body := fmt.Sprintf(`{"sessionId":%q,"platform":"douyin","events":[{"id":"bad","kind":"gift|like"}]}`, f.a.current()["id"])
	w = f.req("POST", "/api/collector/events", body, str(result, "collectorToken"))
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestExternalPlaylistRejectedWithoutFetch(t *testing.T) {
	f := newFixture(t)
	var hits atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.Write([]byte("not media")) }))
	defer remote.Close()
	data := "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\n" + remote.URL + "/segment.ts\n#EXT-X-ENDLIST\n"
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	p, _ := mw.CreateFormFile("file", "idle.mp4")
	_, _ = p.Write([]byte(data))
	_ = mw.Close()
	r := httptest.NewRequest("POST", "/api/media", &b)
	r.Header.Set("Authorization", "Bearer a")
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	if w.Code != 400 || hits.Load() != 0 {
		t.Fatal("playlist accessed external resource", w.Code, hits.Load())
	}
}
func TestSilentVideoValidation(t *testing.T) {
	if _, e := exec.LookPath("ffmpeg"); e != nil {
		t.Skip("ffmpeg unavailable")
	}
	f := newFixture(t)
	for _, audio := range []bool{false, true} {
		t.Run(fmt.Sprint(audio), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "idle.mp4")
			args := []string{"-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=128x128:r=25"}
			if audio {
				args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=16000")
			}
			args = append(args, "-t", "0.2", "-c:v", "libx264", "-pix_fmt", "yuv420p", path)
			if out, e := exec.Command("ffmpeg", args...).CombinedOutput(); e != nil {
				t.Fatal(e, string(out))
			}
			data, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			var b bytes.Buffer
			mw := multipart.NewWriter(&b)
			p, _ := mw.CreateFormFile("file", "untrusted.bin")
			_, _ = p.Write(data)
			_ = mw.Close()
			r := httptest.NewRequest("POST", "/api/media", &b)
			r.Header.Set("Authorization", "Bearer a")
			r.Header.Set("Content-Type", mw.FormDataContentType())
			w := httptest.NewRecorder()
			f.h.ServeHTTP(w, r)
			want := 201
			if audio {
				want = 400
			}
			if w.Code != want {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}
func TestAmbiguousWorkerRestartRetainsSlot(t *testing.T) {
	f := newFixture(t)
	_ = f.start()
	_ = f.a.Close()
	idle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, object{"sessionId": nil, "status": "stopped", "preparing": true})
	}))
	defer idle.Close()
	c := f.a.c
	c.WorkerURL = idle.URL
	h, e := New(c)
	if e != nil {
		t.Fatal(e)
	}
	a := h.(*App)
	defer a.Close()
	if s := a.current(); s == nil || s["status"] != "error" || str(s, "collectorHash") != "" {
		t.Fatal("uncertain preparation released slot", s)
	}
}
func TestRestoreLiveSessionRepairsPreviewAndError(t *testing.T) {
	f := newFixture(t)
	_ = f.start()
	_ = f.a.Close()
	s := f.a.current()
	s["status"] = "starting"
	s["error"] = "obsolete"
	s["previewUrl"] = "http://worker-only/preview"
	_ = f.a.put("sessions", s)
	h, e := New(f.a.c)
	if e != nil {
		t.Fatal(e)
	}
	a := h.(*App)
	defer a.Close()
	s = a.current()
	if s["status"] != "live" || s["error"] != "" || !strings.HasPrefix(str(s, "previewUrl"), "/api/live/") {
		t.Fatal(s)
	}
}

func TestProbeOutputChild(t *testing.T) {
	if os.Getenv("LINGCAST_PROBE_OUTPUT_CHILD") != "1" {
		return
	}
	_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), 3<<20))
	os.Exit(0)
}
func TestBoundedOutputRejectsOversizedChild(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestProbeOutputChild$")
	cmd.Env = append(os.Environ(), "LINGCAST_PROBE_OUTPUT_CHILD=1")
	data, e := boundedOutput(cmd)
	if e == nil {
		t.Fatal("oversized child stdout was accepted", len(data))
	}
	out, ok := cmd.Stdout.(*cappedOutput)
	if !ok {
		t.Fatal("missing capped writer")
	}
	if len(out.Bytes()) > 2<<20 {
		t.Fatal("retained output exceeds cap", len(out.Bytes()))
	}
}

func TestPreviewFlushesBeforeUpstreamCloses(t *testing.T) {
	f := newFixture(t)
	_ = f.a.Close()
	_ = f.start()
	sid := str(f.a.current(), "id")
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/x-flv")
		_, _ = w.Write([]byte("FLV"))
		w.(http.Flusher).Flush()
		<-release
	}))
	defer upstream.Close()
	defer close(release)
	f.a.c.WorkerURL = upstream.URL
	server := httptest.NewServer(f.h)
	defer server.Close()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	req, _ := http.NewRequest("GET", server.URL+"/api/live/"+sid+"/preview", nil)
	req.Header.Set("Authorization", "Bearer a")
	resp, e := client.Do(req)
	if e != nil {
		t.Fatal("preview did not flush headers before close", e)
	}
	defer resp.Body.Close()
	buf := make([]byte, 3)
	if _, e = io.ReadFull(resp.Body, buf); e != nil {
		t.Fatal("preview did not flush bytes before close", e)
	}
	if string(buf) != "FLV" {
		t.Fatal(string(buf))
	}
}
