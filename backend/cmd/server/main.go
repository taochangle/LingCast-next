package main

import (
	"lingcast/backend/internal/app"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	g := os.Getenv
	startTimeout := 12 * time.Minute
	if raw := g("WORKER_START_TIMEOUT"); raw != "" {
		var err error
		startTimeout, err = time.ParseDuration(raw)
		if err != nil || startTimeout <= 0 {
			log.Fatal("Invalid WORKER_START_TIMEOUT")
		}
	}
	h, e := app.New(app.Config{LLMDisableThinking: g("LLM_DISABLE_THINKING") == "true", WorkerStartTimeout: startTimeout, DataDir: g("DATA_DIR"), AdminToken: g("ADMIN_TOKEN"), MediaToken: g("MEDIA_TOKEN"), WorkerURL: g("WORKER_URL"), WorkerToken: g("WORKER_TOKEN"), LLMURL: g("LLM_URL"), LLMToken: g("LLM_TOKEN"), LLMModel: g("LLM_MODEL"), Timezone: g("TIMEZONE"), PublicBaseURL: g("PUBLIC_BASE_URL"), WorkerMediaBaseURL: g("WORKER_MEDIA_BASE_URL"), StaticDir: g("STATIC_DIR")})
	if e != nil {
		log.Fatal(e)
	}
	addr := g("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	s := http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	log.Fatal(s.ListenAndServe())
}
