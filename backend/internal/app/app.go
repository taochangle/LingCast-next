package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Config struct {
	WorkerStartTimeout                                                                                                      time.Duration
	DataDir, AdminToken, MediaToken, WorkerURL, WorkerToken, LLMURL, LLMToken, LLMModel, Timezone, PublicBaseURL, StaticDir string
	HTTPClient                                                                                                              *http.Client
	Clock                                                                                                                   func() time.Time
	HostRandom                                                                                                              func(int) int
}
type App struct {
	done           chan struct{}
	closeOnce      sync.Once
	c              Config
	db             *sql.DB
	mu             sync.Mutex
	loc            *time.Location
	cancel         context.CancelFunc
	lastHost       time.Time
	replies        *replyQueue
	director       *hostDirector
	hostTickActive bool
	hostIndex      int
	topicFlights   map[string]*topicFlight
}
type object map[string]any

func id() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func New(c Config) (http.Handler, error) {
	a, err := newApp(c)
	if err != nil {
		return nil, err
	}
	go a.hostLoop()
	return a, nil
}

func newApp(c Config) (*App, error) {
	if c.AdminToken == "" || c.MediaToken == "" {
		return nil, errors.New("ADMIN_TOKEN and MEDIA_TOKEN required")
	}
	if c.PublicBaseURL == "" {
		c.PublicBaseURL = "http://localhost:8080"
	}
	if c.DataDir == "" {
		c.DataDir = "data"
	}
	if c.StaticDir == "" {
		c.StaticDir = "../frontend/dist"
	}
	if c.Timezone == "" {
		c.Timezone = "Asia/Shanghai"
	}
	loc, e := time.LoadLocation(c.Timezone)
	if e != nil {
		return nil, e
	}
	if c.Clock == nil {
		c.Clock = time.Now
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if e = os.MkdirAll(filepath.Join(c.DataDir, "media"), 0700); e != nil {
		return nil, e
	}
	db, e := sql.Open("sqlite", filepath.Join(c.DataDir, "app.db"))
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	_, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS objects(id TEXT PRIMARY KEY, kind TEXT NOT NULL, body TEXT NOT NULL); CREATE TABLE IF NOT EXISTS live_slot(slot INTEGER PRIMARY KEY CHECK(slot=1),session_id TEXT NOT NULL);`)
	if e != nil {
		return nil, e
	}
	if e = migrateHost(db); e != nil {
		db.Close()
		return nil, e
	}
	if e = migrateCollectorEvents(db); e != nil {
		db.Close()
		return nil, e
	}
	a := &App{c: c, db: db, loc: loc, done: make(chan struct{})} // A restart must never assume an old worker stopped.
	if s := a.current(); s != nil {
		var status object
		statusErr := a.worker(context.Background(), "GET", "/status", nil, &status)
		var persistErr error
		if statusErr == nil && status["status"] == "stopped" && status["preparing"] == false {
			s["status"] = "error"
			s["error"] = "Worker restarted or no longer owns this session"
			s["collectorHash"] = ""
			persistErr = a.terminal(s)
		} else if statusErr == nil && status["sessionId"] == s["id"] && status["status"] == "live" {
			s["status"] = "live"
			s["error"] = ""
			s["previewUrl"] = "/api/live/" + str(s, "id") + "/preview"
			persistErr = a.put("sessions", s)
			if persistErr == nil {
				_, persistErr = a.db.Exec("UPDATE host_tasks SET state='unknown' WHERE session_id=? AND state='pending'", s["id"])
			}
		} else {
			s["status"] = "error"
			s["error"] = "Worker reconciliation required; stop this session before restart"
			s["collectorHash"] = ""
			persistErr = a.put("sessions", s)
		}
		if persistErr != nil {
			_ = db.Close()
			return nil, persistErr
		}
	}

	return a, nil
}
func write(w http.ResponseWriter, n int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(n)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, n int, s string) { write(w, n, object{"error": s}) }
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if d.Decode(v) != nil {
		fail(w, 400, "Invalid JSON")
		return false
	}
	return true
}
func eq(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if r.URL.Path == "/healthz" {
		write(w, 200, object{"ready": true})
		return
	}
	if len(p) == 2 && p[0] == "media" {
		if !eq(r.URL.Query().Get("token"), a.c.MediaToken) {
			fail(w, 401, "Unauthorized")
			return
		}
		a.serveMedia(w, r, p[1])
		return
	}
	if r.URL.Path == "/api/collector/events" {
		a.collect(w, r)
		return
	}
	authorized := strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") && eq(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), a.c.AdminToken)
	if len(p) == 4 && p[1] == "live" && p[3] == "preview" {
		authorized = authorized || eq(r.URL.Query().Get("token"), a.c.AdminToken)
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		if a.c.StaticDir != "" {
			path := filepath.Join(a.c.StaticDir, filepath.Clean("/"+r.URL.Path))
			if _, err := os.Stat(path); os.IsNotExist(err) && !strings.Contains(filepath.Base(path), ".") {
				http.ServeFile(w, r, filepath.Join(a.c.StaticDir, "index.html"))
			} else {
				http.FileServer(http.Dir(a.c.StaticDir)).ServeHTTP(w, r)
			}
		} else {
			http.NotFound(w, r)
		}
		return
	}
	if !authorized {
		fail(w, 401, "Unauthorized")
		return
	}
	if len(p) < 2 {
		http.NotFound(w, r)
		return
	}
	if (p[1] == "avatars" && len(p) >= 4 && p[3] == "host-topics") || p[1] == "host-topics" {
		a.hostTopicsAPI(w, r, p)
		return
	}
	switch p[1] {
	case "live":
		a.live(w, r, p)
	case "media":
		if len(p) == 2 && r.Method == "POST" {
			a.upload(w, r)
		} else if len(p) == 3 && r.Method == "GET" {
			o := a.get("media", p[2])
			if o == nil {
				fail(w, 404, "Not found")
			} else {
				write(w, 200, a.publicMedia(o))
			}
		} else {
			fail(w, 405, "Method not allowed")
		}
	case "settings":
		a.settings(w, r)
	default:
		a.crud(w, r, p)
	}
}
func str(o object, k string) string { s, _ := o[k].(string); return s }
func hash(s string) string          { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func (a *App) closedLocked() bool {
	select {
	case <-a.done:
		return true
	default:
		return false
	}
}

// Close terminates background polling. Call after HTTP shutdown.
func (a *App) Close() error {
	a.closeOnce.Do(func() {
		a.mu.Lock()
		close(a.done)
		if a.director != nil {
			if a.director.cancel != nil {
				a.director.cancel()
			}
			a.director = nil
		}
		a.mu.Unlock()
	})
	return nil
}
