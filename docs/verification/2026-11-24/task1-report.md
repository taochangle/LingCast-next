# Task 1 — Go persisted API

Implemented all admin resource routes, session lifecycle/worker orchestration, authenticated media, collector events, fixed scripts and OpenAI-compatible comment responses. Files are restricted to backend/; this report is the requested task artifact.

## RED evidence

Before implementation, `cd backend && go test ./...` failed:

```
internal/app/app_test.go:3:39: undefined: Config
internal/app/app_test.go:3:102: undefined: New
internal/app/app_test.go:4:45: undefined: New
internal/app/app_test.go:4:49: undefined: Config
FAIL lingcast/backend/internal/app [build failed]
```

Initial tests covered persistence and invalid startup timezone. Additional meaningful integration tests were added while implementing the remaining lifecycle and media behavior.

## GREEN evidence

Final commands:

```
cd backend && go test -race ./... && go vet ./...
?    lingcast/backend/cmd/server [no test files]
ok   lingcast/backend/internal/app 2.081s
```

`go vet` completed without output. `git diff --check` passed. Tests cover concurrent starts (one201/one409), failed start releasing slot after confirmed cleanup, foreign avatar/look rejection, collector platform/token lifecycle, manual text unchanged, fixed collector script unchanged and event dedup, injected business-clock/timezone in LLM prompt, invalid upload and oversized valid image cleanup, persistent avatar records across reopen, invalid timezone, explicit host toggling/throttle, and stop during an outstanding start without resurrecting its session.

## Implementation details and review

- SQLite WAL and transaction-backed singleton live slot, persisted objects, bounded dedup history per session; UUID v4 IDs. Live material mutation/deletion is blocked. Referenced parents/material kinds/default idle ownership are validated.
- Worker task IDs, priority kind, server timestamps, 120-second expiry sent exactly; Worker owns its bounded100 priority queue. No unbounded in-memory event queue was introduced; collector batches limited100. Pending interactive requests remain ordinary synchronous HTTP requests.
- Tokens stored hashed for collectors; rotation and stopping invalidate old credentials. Before each collector dispatch, live ownership is rechecked; LLM waits release the session lock so stop can proceed.
- Worker startup reserves singleton before network preparation; cancellation/stale completion cannot install a live record. Failed actual starts require confirmed Worker stop before slot release. If cleanup fails, the error slot is deliberately retained and explicit stop can retry. Readiness failures before posting a start safely release the slot without trying to stop an unknown Worker session.
- `WORKER_START_TIMEOUT` defaults12m for first face preparation; normal remote calls have25s timeout. Preview streaming uses no total request timeout. Start/stop overlap is tested; Worker is additionally required to cancel/serialize its own preparation/stop and reject stale routes.
- Status polling detects dead Worker regardless of host toggle. Host checks idle queue and speaking state, cycles saved text, throttles at least5s after observed activity/completion, and sends no automatically invented host script. Worker enforces actual dequeue priorities.
- Media capped200MiB, images30MiB/100million pixels; supported decoded images PNG/JPEG/GIF. Silent video requires positive duration, bounded dimensions/fps, ffprobe and full ffmpeg error-on-corruption decode. Uploaded names are ignored; paths are generated UUIDs and temporary failures removed. Media URLs use separate escaped MEDIA_TOKEN.
- Fresh business time is injected per LLM call, including explicit timezone/date/time/weekday; persona is system context, viewer input is user data. Saved scripts and manual speech bypass LLM entirely.
- Admin settings show configured booleans and service locations, never configured tokens. Relay remains explicit and validates RTMP/RTMPS server+key. Built React is served using STATIC_DIR (default ../frontend/dist), with index fallback for extensionless UI routes.
- Pure-Go modernc SQLite v1.34.5, module minimum Go1.23. Commands/handlers do not require embedded frontend files at compile time.

## Limits / remaining acceptance

Tests use a fake HTTP Worker/LLM, not GPU inference or real SRS/relay. No production deployment or external RTMP was performed. The media test exercises invalid ffprobe input and oversized image; positive real video acceptance remains appropriate for integration smoke using synthetic ffmpeg files. Tests do not claim full100-job saturation or priority-dequeue proof (Worker owns that queue), exact frame synchronization, real platform DOM, hardware, or sustained streaming acceptance. SQLITE/media are persistent; full backup/restore and interrupted filesystem writes are not exercised here. FFmpeg/ffprobe must be installed in the app deployment image. Operator must provide a routable PUBLIC_BASE_URL for remote Workers (default localhost is development-only).

# Review fix round 1

Read receiving-code-review and the SDD fix-loop reference. Verified all reported findings against owning backend code; no unrelated paths changed.

## RED (before fixes)

Added `backend/internal/app/review_test.go` and ran:

```
go test ./internal/app -run 'TestRecoverSlot|TestStopRecovers|TestPersistenceFailures|TestStopReleaseAtomic|TestPreviewIs|TestSessionDedup|TestExactEvent|TestExternalPlaylist' -count=1
```

Observed failures: confirmed idle Worker retained stale slot; stale stop returned503 instead of recovery; injected SQLite UPDATE failure still returned201 for start and200 for token/host/stop; DELETE failure still returned stopped200; preview contained Worker-facing public origin; cap accepted new event instead of rejecting it; compound event kind `gift|like` was accepted. Playlist canary test already passed, then retained as a regression alongside stricter demux restrictions.

## Changes

- Positive idle recovery now requires a successful Worker status with `status=stopped` and explicit `preparing=false`. Startup clears crashed/restarted stale slots only under that proof; explicit stop can recover when stale stop is rejected but status proves idle. Missing flag, preparation in flight, unreachable status, and active Worker remain uncertain and retain the singleton. Worker implementer added explicit preparing in every status response.
- A transaction now persists a terminal session, removes only its own singleton slot, and removes its dedup IDs together. Stop/token rotation/host changes check persistence before success; start installation failure returns500 and attempts Worker cleanup while retaining the slot if persistence cannot be completed. Trigger-injected UPDATE/DELETE errors are covered.
- Preview URLs are relative same-origin routes. Restart reconciliation repairs restored live status, stale error and preview fields.
- GET `/api/live` keeps `session:null` when no active singleton; additive optional `lastSession` reports the latest released stopped/error record.
- Collector IDs are retained throughout the session. At10000 distinct IDs, new IDs receive409 instructing a restart; duplicates still return accepted0. Capacity is rechecked under the lock at each insertion to cover concurrent batches. No history eviction permits old replay. Terminal release cleans the session's IDs.
- Exact enum membership replaces delimiter substring membership for script/event kinds.
- FFprobe/FFmpeg are forced to signature-selected self-contained demuxers (ftyp ISO BMFF, Matroska/WebM, AVI); only file protocol is enabled, MOV external data refs/absolute paths disabled. Playlists cannot reach automatic probing or external fetch. Probe metadata output capped2MiB, individual allocation capped128MiB, decode threads2; shared60s process deadline retained. Synthetic silent MP4 acceptance and audio-track rejection verified using actual FFmpeg/FFprobe.

## GREEN

```
cd backend && go test -race ./... && go vet ./...
?    lingcast/backend/cmd/server [no test files]
ok   lingcast/backend/internal/app 3.226s
```

`go vet` and `git diff --check` passed silently. New covering tests: `TestRecoverSlotAfterWorkerRestart`, `TestStopRecoversConfirmedIdleOnly`, `TestPersistenceFailuresAreNotSuccess`, `TestStopReleaseAtomic`, `TestPreviewIsSameOrigin`, `TestSessionDedupDoesNotForget`, `TestExactEventEnum`, `TestExternalPlaylistRejectedWithoutFetch`, `TestSilentVideoValidation`, `TestAmbiguousWorkerRestartRetainsSlot`, `TestRestoreLiveSessionRepairsPreviewAndError` in `review_test.go`. Existing race suite remains green. Live process on18080 was not restarted or touched. GPU/real long-running inference still unverified.

# Review fix round 2

Read SDD fix-loop before this scoped round. Confirmed embedded bytes.Buffer promoted ReadFrom, allowing os/exec's io.Copy optimization to bypass the capped Write method.

Covering regression added to `backend/internal/app/review_test.go`: `TestBoundedOutputRejectsOversizedChild` launches the current test executable as a real child process emitting3MiB. RED: `go test ./internal/app -run TestBoundedOutputRejectsOversizedChild -count=1` failed with `oversized child stdout was accepted 3145728`.

Replaced embedded Buffer with a named private buffer and explicit Bytes accessor; the type no longer implements promoted ReadFrom. Regression verifies rejection and both retained length and buffer capacity at most2MiB. No other scope changed.

GREEN:

```
go test ./internal/app -run 'TestBoundedOutputRejectsOversizedChild|TestSilentVideoValidation|TestExternalPlaylistRejectedWithoutFetch|TestUploadInvalidAndOversized|TestOversizedImageRejected' -count=1
ok lingcast/backend/internal/app 0.652s

go test -race ./... && go vet ./...
?  lingcast/backend/cmd/server [no test files]
ok lingcast/backend/internal/app 3.246s
```

Vet and `git diff --check` passed silently. Report appended; no server process restarted.

# Collector acknowledgement / preview integration fix

New cross-module requirement: additive processedIds/retryIds must distinguish completed duplicates, pending concurrent requests, and partial failures without changing accepted/auth behavior. Files remain backend-only. Removed the probe buffer Capacity assertion as requested; byte-length cap and rejection checks remain.

RED evidence: new `collector_ack_test.go` tests initially failed with `{1 [] []}`, `{0 [] []}`, and `table events has no column named state`. Preview regression `TestPreviewFlushesBeforeUpstreamCloses` initially failed with `Client.Timeout exceeded while awaiting headers` while upstream deliberately stayed open.

Implementation:
- SQLite events explicitly records pending/done. Existing schemas gain state with legacy IDs treated as done; new requests reserve pending before LLM/dispatch, set done after successful dispatch or intentional no-script skip, and remove pending on failure. Startup removes reservations from crashed handlers; stable task IDs allow safe Worker session dedup on retry.
- Every200 collector response includes processedIds and retryIds string arrays. Already-done duplicates appear in processedIds with accepted0. Concurrent pending duplicates appear only in retryIds. Partial failures are retryable while completed IDs remain acknowledged; only durably marked done counts as newly accepted. Session cap and stale-token rules remain.
- Preview writes/flushed upstream status headers immediately and flushes each copied chunk using ResponseController, so small FLV fragments reach the browser before upstream closes.

Covering tests: `TestCollectorPartialFailuresAndDurableAcknowledgement` (both fake LLM and Worker fail partially, then retry), `TestCollectorPendingDuplicateRetries`, `TestStartupClearsPendingKeepsDone`, `TestCollectorIntentionalSkipAcknowledged` in `collector_ack_test.go`; streaming regression in `review_test.go`. Existing oversized child output regression retains error/length assertions only.

GREEN commands/output:

```
go test ./internal/app -run 'TestCollectorPartialFailuresAndDurableAcknowledgement|TestCollectorPendingDuplicateRetries|TestStartupClearsPendingKeepsDone|TestPreviewFlushesBeforeUpstreamCloses' -count=1
ok lingcast/backend/internal/app 0.432s

go test -race ./... && go vet ./...
?  lingcast/backend/cmd/server [no test files]
ok lingcast/backend/internal/app 3.781s
```

Vet and diff whitespace checks passed. No18080 process restart or external RTMP. Real FLV/SRS sustained streaming remains separate acceptance from the small-chunk HTTP regression.
