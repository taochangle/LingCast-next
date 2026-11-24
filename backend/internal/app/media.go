package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (a *App) mediaURL(i string) string {
	return strings.TrimRight(a.c.PublicBaseURL, "/") + "/media/" + i + "?token=" + url.QueryEscape(a.c.MediaToken)
}
func (a *App) publicMedia(o object) object {
	v := object{}
	for k, x := range o {
		v[k] = x
	}
	v["url"] = a.mediaURL(str(o, "id"))
	return v
}
func (a *App) serveMedia(w http.ResponseWriter, r *http.Request, i string) {
	o := a.get("media", i)
	if o == nil {
		fail(w, 404, "Not found")
		return
	}
	http.ServeFile(w, r, filepath.Join(a.c.DataDir, "media", i))
}
func (a *App) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 201<<20)
	if e := r.ParseMultipartForm(1 << 20); e != nil {
		fail(w, 400, "Upload too large or invalid multipart")
		return
	}
	defer r.MultipartForm.RemoveAll()
	src, _, e := r.FormFile("file")
	if e != nil {
		fail(w, 400, "File required")
		return
	}
	defer src.Close()
	f, e := os.CreateTemp(filepath.Join(a.c.DataDir, "media"), "upload-")
	if e != nil {
		fail(w, 500, "Storage unavailable")
		return
	}
	path := f.Name()
	defer os.Remove(path)
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(src, (200<<20)+1))
	_ = f.Close()
	if e != nil || n == 0 || n > 200<<20 {
		fail(w, 400, "Invalid upload size")
		return
	}
	o := object{"id": id(), "sha256": hex.EncodeToString(h.Sum(nil)), "fps": 0}
	img, _ := os.Open(path)
	cfg, _, imageErr := image.DecodeConfig(img)
	_ = img.Close()
	if imageErr == nil {
		if n > 30<<20 || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 100000000 {
			fail(w, 400, "Image exceeds limits")
			return
		}
		img, _ = os.Open(path)
		_, _, e = image.Decode(img)
		_ = img.Close()
		if e != nil {
			fail(w, 400, "Invalid image")
			return
		}
		o["kind"] = "image"
		o["width"] = cfg.Width
		o["height"] = cfg.Height
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		format, detectErr := videoFormat(path)
		if detectErr != nil {
			fail(w, 400, "Supported self-contained video required (MP4/MOV, WebM/MKV, AVI)")
			return
		}
		inputArgs := []string{"-protocol_whitelist", "file", "-format_whitelist", format, "-f", format}
		if format == "mov" {
			inputArgs = append(inputArgs, "-enable_drefs", "0", "-use_absolute_path", "0")
		}
		probeArgs := append([]string{"-v", "error", "-max_alloc", "134217728"}, inputArgs...)
		probeArgs = append(probeArgs, "-show_streams", "-show_format", "-of", "json", path)
		raw, e := boundedOutput(exec.CommandContext(ctx, "ffprobe", probeArgs...))
		var probe struct {
			Format struct {
				Duration string `json:"duration"`
			} `json:"format"`
			Streams []struct {
				CodecType string `json:"codec_type"`
				Width     int    `json:"width"`
				Height    int    `json:"height"`
				Rate      string `json:"r_frame_rate"`
			} `json:"streams"`
		}
		if e != nil || json.Unmarshal(raw, &probe) != nil {
			fail(w, 400, "Invalid media")
			return
		}
		duration, _ := strconv.ParseFloat(probe.Format.Duration, 64)
		if duration <= 0 {
			fail(w, 400, "Video must have a positive duration")
			return
		}
		video := false
		for _, s := range probe.Streams {
			if s.CodecType == "audio" {
				fail(w, 400, "Idle videos must be silent")
				return
			}
			if s.CodecType == "video" {
				video = true
				var num, den float64 // ffprobe rational frame rate
				if _, e = fmt.Sscanf(s.Rate, "%f/%f", &num, &den); e != nil || den <= 0 || num/den <= 0 || num/den > 120 || s.Width <= 0 || s.Height <= 0 || s.Width > 4096 || s.Height > 4096 {
					fail(w, 400, "Unsupported video dimensions or frame rate")
					return
				}
				o["width"] = s.Width
				o["height"] = s.Height
				o["fps"] = num / den
			}
		}
		if !video {
			fail(w, 400, "Video required")
			return
		}
		decodeArgs := append([]string{"-v", "error", "-xerror", "-max_alloc", "134217728", "-threads", "2"}, inputArgs...)
		decodeArgs = append(decodeArgs, "-i", path, "-f", "null", "-")
		if e = exec.CommandContext(ctx, "ffmpeg", decodeArgs...).Run(); e != nil {
			fail(w, 400, "Video decode failed")
			return
		}
		o["kind"] = "video"
	}
	dest := filepath.Join(a.c.DataDir, "media", str(o, "id"))
	if os.Rename(path, dest) != nil {
		fail(w, 500, "Storage unavailable")
		return
	}
	if a.put("media", o) != nil {
		_ = os.Remove(dest)
		fail(w, 500, "Storage unavailable")
		return
	}
	write(w, 201, a.publicMedia(o))
}

// Force a self-contained demuxer from the file signature before FFmpeg sees input.
// Network protocols and playlists are never enabled; MOV external data refs are disabled.
func videoFormat(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	b := make([]byte, 12)
	if _, e = io.ReadFull(f, b); e != nil {
		return "", e
	}
	switch {
	case string(b[4:8]) == "ftyp":
		return "mov", nil
	case bytes.Equal(b[:4], []byte{0x1a, 0x45, 0xdf, 0xa3}):
		return "matroska", nil
	case string(b[:4]) == "RIFF" && string(b[8:12]) == "AVI ":
		return "avi", nil
	}
	return "", errors.New("Unsupported container")
}

type cappedOutput struct{ buffer bytes.Buffer }

func (b *cappedOutput) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > 2<<20 {
		return 0, errors.New("Probe metadata exceeds limit")
	}
	return b.buffer.Write(p)
}
func (b *cappedOutput) Bytes() []byte { return b.buffer.Bytes() }
func boundedOutput(cmd *exec.Cmd) ([]byte, error) {
	var out cappedOutput
	cmd.Stdout = &out
	if e := cmd.Run(); e != nil {
		return nil, e
	}
	return out.Bytes(), nil
}
