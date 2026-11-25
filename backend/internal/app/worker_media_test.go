package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStartUsesWorkerMediaBaseWithoutChangingPublicURLs(t *testing.T) {
	f := newFixture(t)
	f.a.c.WorkerMediaBaseURL = "http://app:8080/"
	var payload struct {
		ImageURL string `json:"imageUrl"`
		Idles    []struct {
			URL string `json:"url"`
		} `json:"idles"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			write(w, 200, object{"ready": true})
			return
		}
		if r.URL.Path == "/sessions" {
			_ = json.NewDecoder(r.Body).Decode(&payload)
			write(w, 200, object{"previewUrl": "/preview/x.flv"})
			return
		}
		write(w, 200, object{})
	}))
	defer server.Close()
	f.a.c.WorkerURL = server.URL
	if w := f.start(); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, u := range []string{payload.ImageURL, payload.Idles[0].URL} {
		if !strings.HasPrefix(u, "http://app:8080/media/") || !strings.HasSuffix(u, "?token=m") {
			t.Fatal(u)
		}
	}
	if u := f.a.mediaURL("m1"); !strings.HasPrefix(u, "http://business/media/") {
		t.Fatal(u)
	}
	f.a.c.WorkerMediaBaseURL = ""
	if f.a.workerMediaURL("m1") != f.a.mediaURL("m1") {
		t.Fatal("remote deployment fallback changed")
	}
}
