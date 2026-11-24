# Final fix wave

Implementation commit `5b3fcf8` on feat/initial-version addresses all three Important findings in final-review.md.

- The single renderer takes one bounded text segment per scheduling decision and drains it before taking the next. A started script's remainder keeps the original queue slot, priority, FIFO sequence and dedup identity. Queue expiry applies before speech begins. The integration-level Worker regression injects a manual announcement during the first host render, asserts the drain order, exact resumed text including whitespace, and rejection of a duplicate task ID.
- Worker relay process status distinguishes disabled, running and failed. Go refresh obtains only a matching session's allowlisted state and reports unknown on lookup failure. The UI shows launch pending after command acceptance, process health on refresh, and supports explicit disable. Tests exercise a disposable local Python child, server-side status redaction and reloading a studio whose session remains live after relay failure.
- MuseTalk loads only shared models on startup, verifies the selected base SHA, and releases resident avatar references before allocating its successor. Rendering also drops its transient bound callback reference. Re-selecting valid cached material reuses its on-disk artifacts, updates the manifest only when the selected binding changes, and checks the selected source hash before rendering. A fake avatar allocator checks the previous object is already released when the next allocation starts.

## RED

- `python3 -m unittest worker.tests.test_final_review worker.tests.test_worker.WorkerTests.test_long_host_yields_after_drain_then_resumes_exact_text` initially produced three assertion failures and one missing-method error. It reproduced wrong speech order, retained avatar, failed startup with stale binding, and absent child state reporting.
- In backend, `go test ./internal/app -run TestLiveRefreshReportsSanitizedRelayFailure` failed because the live session omitted the relay state.
- In frontend, `npm test -- --run -t 'shows relay failure'` failed because the refreshed studio omitted the process failure.

## GREEN

- The same focused Python command passed five tests after adding the cached-binding and modified-file regression. The targeted UI test passed.
- Backend `go test -race -count=1 ./... && go vet ./...` passed.
- Frontend `npm test -- --run && npm run build` passed all 15 tests and TypeScript/Vite production build.
- `docker run --rm -v "$PWD:/check:ro" -w /check --entrypoint python lingcast-task5-worker:latest -m unittest discover -s worker/tests` passed all 33 tests in the Linux CPU environment, including real FFmpeg mux.
- `./scripts/check-compose.sh && ./scripts/smoke-compose.sh` passed five configuration checks and the local Go/Worker/FFmpeg/SRS path using synthetic inference. The smoke project and its volumes were cleaned up.
- `git diff --check` passed. The final switch from whole-file allocation to a bounded streaming SHA was verified with the five focused Python tests.

Process state observes the local relay child. Platform ingest, live comments, GPU model loading, real inference, VRAM consumption and production deployment remain outside this CPU verification. No public RTMP test or change to the original LingCast tree.
