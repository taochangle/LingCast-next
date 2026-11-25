package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestVideoUploadNormalizes24FPSAndHashesStoredFile(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg required")
	}
	f := newFixture(t)
	input := filepath.Join(t.TempDir(), "idle.mp4")
	if out, e := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=s=128x128:r=24:d=2", "-an", "-c:v", "libx264", input).CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	original, e := os.ReadFile(input)
	if e != nil {
		t.Fatal(e)
	}
	var body bytes.Buffer
	multi := multipart.NewWriter(&body)
	part, _ := multi.CreateFormFile("file", "idle.mp4")
	_, _ = part.Write(original)
	_ = multi.Close()
	r := httptest.NewRequest("POST", "/api/media", &body)
	r.Header.Set("Authorization", "Bearer a")
	r.Header.Set("Content-Type", multi.FormDataContentType())
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var media object
	if e = json.Unmarshal(w.Body.Bytes(), &media); e != nil {
		t.Fatal(e)
	}
	if media["fps"] != float64(25) {
		t.Fatalf("upload kept original rate: %v", media["fps"])
	}
	stored := filepath.Join(f.a.c.DataDir, "media", str(media, "id"))
	data, _ := os.ReadFile(stored)
	hash := sha256.Sum256(data)
	if str(media, "sha256") != hex.EncodeToString(hash[:]) {
		t.Fatal("hash does not describe stored normalized file")
	}
	raw, e := exec.Command("ffprobe", "-v", "error", "-show_streams", "-show_format", "-of", "json", stored).Output()
	if e != nil {
		t.Fatal(e)
	}
	var probe struct {
		Streams []struct {
			Rate  string `json:"avg_frame_rate"`
			Codec string `json:"codec_name"`
			Type  string `json:"codec_type"`
		} `json:"streams"`
	}
	if e = json.Unmarshal(raw, &probe); e != nil {
		t.Fatal(e)
	}
	if len(probe.Streams) != 1 || probe.Streams[0].Rate != "25/1" || probe.Streams[0].Codec != "h264" {
		t.Fatal(string(raw))
	}
	after, _ := os.ReadFile(input)
	if !bytes.Equal(original, after) {
		t.Fatal("caller original changed")
	}
}
