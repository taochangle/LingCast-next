# LingCast Next Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 独立实现多人物多形象、单场直播、双平台采集和可拆分 GPU 服务的新版。
**Architecture:** Go 业务 API 与 React 管理端，SQLite 和本地持久化媒体；独立 Python Worker 复用流式配音/口型及 FFmpeg/SRS。Compose 可本地组合或跨机连接。
**Tech Stack:** Go net/http, modernc SQLite, React/TypeScript/Vite, Python 3.12, CosyVoice/MuseTalk, FFmpeg, SRS, Docker Compose.
**Spec:** ../specs/2026-11-24-lingcast-next-design.md; exact wire contracts: ../../contracts.md

## Global Constraints
- 整个系统同时仅运行一场直播；人物 → 多形象 → 多 idle，开播固定形象。
- 不修改或部署旧版；不包含模型权重、真实认证值或用户媒体；不发送平台评论或启用外部推流测试。
- 标准 Compose，不做 setup；Windows WSL2 与 Linux NVIDIA 分别实际验收。
- Worker 与模型独立，远端地址和认证可配置；默认不启用外部转推。
- LLM 每次注入业务服务器时间和明确时区，默认 Asia/Shanghai；固定稿原文口播。
- 抖音和小红书分别适配 DOM，平台和场次必须匹配；未展示信息不猜测。
- CPU mock 测试不等于 GPU、真实平台 DOM 或长时间直播验收。

## Review Focus
- 并发开播/启动超时只能保留一个场次，失败可清理（Task1）。
- 停播重开后旧采集 token/旧 Worker 任务失效（Task1/3/4）。
- 损坏/超大/跨人物素材被拒绝且不遗留有效记录（Task1/2）。
- 不完整 PCM/JPEG 流、模型失联和 FFmpeg 提前退出应报告错误并清理（Task3）。
- 跨机地址/媒体认证/远程模型配置与两个独立 Compose 能一致工作（Task3/5）。

---

### Task 1: Go 持久化业务 API 与直播调度
**Files:** Create `backend/cmd/server/main.go`, `backend/internal/app/{app,store,media,live,interaction,worker,llm}.go`, matching `_test.go`, `backend/go.mod`, `backend/go.sum`.
**Interfaces:** consumes Worker REST in docs/contracts.md; produces complete Admin API and media URLs there. `New(Config) (http.Handler,error)`; `Config` contains data/admin/media/worker/LLM/timezone/public URL fields and injectable HTTP client/clock for tests. SQLite transactions enforce single live slot; bounded queues and HTTP timeouts. Record IDs UUIDs. Prefer small focused files.
- [ ] Write httptest tests `TestConcurrentStart`, `TestStartFailureReleasesSlot`, `TestCrossAvatarLookRejected`, `TestCollectorLifecycleAndPlatform`, `TestUploadInvalidAndOversized`, `TestPersistence`, `TestPromptUsesServerClock`, `TestFixedScriptBypassesLLM`, `TestHostPauseAndPriority`. Assert 1 success/1 conflict for concurrent starts, expired token401, platform400, invalid media400, persistent resources after reopen, exact timezone/date in LLM request and exact manual text in Worker payload.
- [ ] Run `cd backend && go test ./...`, record expected RED before implementation.
- [ ] Implement API per contracts, CRUD validation and referential integrity; live resources cannot be deleted; safe worker start/stop reconciliation; admin auth; media bounded decode; last session state, collector rotation, queue 100 / expiry120s / session dedup. Host uses saved host scripts in order and checks actual Worker idle; priorities enforced by Worker. Reject invalid timezone at startup. Secrets never logged or returned.
- [ ] Run `go test -race ./...` and `go vet ./...`; record GREEN output and limitations.
- [ ] Self-review and commit only backend paths with `feat: add persisted live management API`.

### Task 2: React 管理端
**Files:** Create `frontend/package.json`, lockfile, `frontend/src/{main,App,api,types}.tsx|ts`, `frontend/src/{styles.css,components/*}`, `frontend/index.html`, TS/Vite configs and focused UI tests.
**Interfaces:** consumes Admin API contracts (env-backed read-only service config); exports build `frontend/dist` for Go static hosting. Admin token stored in sessionStorage, never bundled. API origin same-origin, Vite /api and /media dev proxy.
- [ ] Write tests for API error display and selecting only looks belonging to selected avatar; assert start includes platform and empty/missing selection disables start. Assert stop/host/relay explicit actions; no automatic relay on start.
- [ ] Run focused Vitest RED, then implement UI covering characters/persona/voice, image uploads/looks/idles/default idle, categorized scripts, start/stop, platform selector, per-session collector config/token rotation, manual speech, host pause, preview and explicit relay config, safe service settings and checks.
- [ ] Keep Chinese product copy and clear empty/loading/error states. Use React text rendering for viewer values; avoid raw HTML. Visually cohesive dark studio interface, desktop-first responsive layout. Do not expose authentication in logs. Build modest reusable panels; no framework migration.
- [ ] Run `npm test -- --run`, `npm run build`; record GREEN. Self-review and commit only frontend paths `feat: add live studio management UI`.

### Task 3: Python Worker 与真实模型适配
**Files:** Create `worker/{app,session,scheduler,models,output}.py`, `worker/tests/*`, `worker/requirements.txt`, `services/{tts,lipsync}/`, `docs/model-services.md`. Reuse selected sources from old worker with attribution in docs; never copy private config.
**Interfaces:** implements exact Worker REST contract, consumes Go signed media URLs, renders through authenticated resident models, produces RTMP to SRS and authenticated preview proxy. `SessionManager.start(payload:dict)->dict`, `.stop(session_id:str)`, `.enqueue(session_id:str,task:dict)`, `.status()->dict`; rendering material registered by lookId and SHA.
- [ ] Write CPU tests `test_stale_session`, `test_queue_priority_expiry`, `test_partial_stream_rejected`, `test_wrong_material_hash`, `test_stop_cleans_processes`, `test_remote_media_auth`, `test_renderer_failure`; assert default idle preparation, no special avatar IDs, deterministic priority, no external relay without explicit command.
- [ ] Run unittest RED; implement bounded NDJSON parser with PCM/frame/index checks and real-time output reusing old `ffmpeg_pipe.py`/`audio_windows.py` as appropriate. Sequential idle pool, fixed look per session, default bottom plate for talking. Download media with bounded sizes/timeouts/checksum and clean temp files. No unsafe arbitrary local paths from clients. Stop cancels worker and FFmpeg; failures reflected in /status.
- [ ] Adapt existing CosyVoice/MuseTalk services to generic prepared look identity and voice config, documented authenticated preparation endpoint. Use real dependencies, explicit pinned upstream source/model assets. If current hardware cannot run models, provide runnable code/build definitions and exact unverified prerequisites; never substitute a dummy healthy model.
- [ ] Run CPU suite, py_compile; optional local FFmpeg smoke with synthetic frames if available; document real GPU gaps. Self-review and commit only owned worker/services/model docs `feat: add isolated streaming worker and model adapters`.

### Task 4: 双平台 Chrome 采集插件
**Files:** Create `extensions/chrome/{manifest.json,background.js,content.js,adapters.js,protocol.js,popup.html,popup.js,popup.css,README.md,tests/*}`.
**Interfaces:** consumes /api/collector/events with session token/platform; user configures origin, sessionId, token, platform; exact normalized event contract. Start/stop controlled by user, read-only DOM.
- [ ] Write Node tests for platform URL selection, safe normalized events, duplicates and persisted visible DOM initial scan, platform mismatch, malicious nicknames preserved as data. Include distinct synthetic DOM fixtures for both platforms; do not label fixtures as real platform acceptance.
- [ ] Run `node --test extensions/chrome/tests/*.test.mjs` RED; selectively reuse old protocol/DOM reader. Implement different selectors/parsers per platform; missing fields ignored, gifts require visible gift evidence. Bounded dedup, MutationObserver cleanup, explicit start, no comments/network interception/browser automation.
- [ ] Run tests GREEN and syntax checks, document selector validation still needing actual pages. Self-review and commit owned extension paths `feat: add platform-aware live event collector`.

### Task 5: Compose、跨模块集成与验收
**Files:** Create `compose.yaml`, `compose.ai.yaml`, `.env.example`, `deploy/{app,worker,tts,lipsync}.Dockerfile`, `deploy/srs.conf`, `README.md`, `docs/acceptance.md`; modify earlier modules only to fix verified contract integration defects.
**Interfaces:** consumes built modules; defaults app8080, worker8090, SRS private ingest1935, preview8080 inside network. Separate published host variables. GPU models profile `models`; workers use configured remote endpoints without profile. MEDIA_TOKEN passed in generated URLs. Explicit auth required, no hardcoded real addresses/tokens. Sources pinned via verified commits, do not invent hashes or image tags.
- [ ] Validate API/UI/Worker types against contracts; add integration test with httptest/fake inference to start, enqueue, platform event, stop and reject stale request. Add Compose config checks for business-only, remote AI-only, combined with models and combined remote models.
- [ ] Implement app multi-stage UI+Go build, worker FFmpeg image, model build/mount requirements, SRS private default networking. Models and cache volumes persist. Document Windows WSL2 and Linux prerequisites and all four exact start commands, configured routable PUBLIC_BASE_URL, TLS/auth for remote, download/preparation prerequisites and no production deployment.
- [ ] Run backend tests/vet, frontend tests/build, Python tests, extension tests, `docker compose -f compose.yaml config -q`, AI-only and combined configs. Attempt available CPU Compose smoke; explicitly report if Docker daemon unavailable or GPU unavailable.
- [ ] Review final diff, report acceptance evidence with pass/blocked separately. Commit `feat: package local and remote compose deployments`.

## Execution ruling
User explicitly requested execution with subagent division on 2026-11-24; this authorizes this plan's execution without another approval round. Fresh independent repository on feature branch is the isolated workspace; no old checkout worktree is required. Task1/2/3 are separate directory owners and may proceed concurrently against frozen docs/contracts.md; per-task reviews gate integration. Task4 follows when an agent slot is free; Task5 after all producers. Tests and real GPU acceptance are separate evidence.
