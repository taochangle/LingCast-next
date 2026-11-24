package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (a *App) worker(ctx context.Context, m, p string, in, out any) error {
	return a.remote(ctx, a.c.WorkerURL, a.c.WorkerToken, m, p, in, out)
}
func (a *App) remote(ctx context.Context, base, token, m, p string, in, out any) error {
	if base == "" {
		return errors.New("Service not configured")
	}
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = strings.NewReader(string(b))
	}
	timeout := 25 * time.Second
	client := a.c.HTTPClient
	if m == "POST" && p == "/sessions" {
		timeout = a.c.WorkerStartTimeout
		if timeout <= 0 {
			timeout = 12 * time.Minute
		}
		copyClient := *client
		copyClient.Timeout = timeout
		client = &copyClient
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, m, strings.TrimRight(base, "/")+p, body)
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, e := client.Do(req)
	if e != nil {
		return errors.New("Service unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("Service returned %d", resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(out)
	}
	return nil
}

// workerIdle requires an explicit preparation flag, not absence of an active ID alone.
func (a *App) workerIdle(ctx context.Context) bool {
	var status object
	return a.worker(ctx, "GET", "/status", nil, &status) == nil && status["status"] == "stopped" && status["preparing"] == false
}
