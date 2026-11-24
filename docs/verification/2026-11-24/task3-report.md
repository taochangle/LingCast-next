# Task3 implementation report

Owned paths: worker/, services/, docs/model-services.md. Existing LingCast checkout only read; no deployment/external RTMP/model download was performed. Task3 report scratch path is intentionally not committed.

## Implemented

* Worker authenticated REST matches contracts; media tokens preserved in URLs, checksum/size/time validation, no redirects or arbitrary local client paths. One fixed-look session; responsive status and stop during blocked preparation, cancellation prevents late installs. Empty status stopped/preparing false. Same failed/stopped ID stop idempotent; stale routes cannot touch new session. Detached cancelled preparation retains preparing true until finished, new starts conflict meanwhile.
* Scheduler stable priority/manual>gift>like>comment>welcome>host, queue100, max120s expiry, session dedup with100000 task cap. status queueDepth/speaking/lastTaskCompletedAt for host throttling.
* Full real default-idle preparation, resident readiness, per-voice existence check; authenticated CosyVoice zero-shot and MuseTalk adapter. CosyVoice invoked once internally by MuseTalk, paired16k/25fps s16/JPEG NDJSON consumed by Worker with contiguous index/clock/completion validation and bounded lines. Arbitrary look and voice IDs. Changed default material gets lookId+SHA256 profile; wrong hash rejected instead of replaying stale latents. No dummy ready model.
* FFmpeg25fps idle cycling, bounded speaking queue, matching audio slices, H264/AAC FLV to SRS; authenticated preview proxy. External relay only explicit PUT. Output/decode/relay subprocesses terminated on stop; dead FFmpeg makes status error. Text errors never include URLs/tokens.
* Separate model dependency inputs copied from verified pinned upstream URLs, actual upstream runtime API inspected. MuseTalk Python3.10/cu118 stack differs from Worker3.12 and CosyVoice/cu121. Offline HF and local S3FD checkpoint prevent implicit startup model downloads. Exact mount/asset/entrypoint requirements in docs/model-services.md.

## TDD/verification evidence

1. Initial RED `python3 -m unittest discover -s worker/tests -v`: ModuleNotFoundError worker.scheduler;1 test module error, no production code yet. Tests cover all named plan cases.
2. GREEN after implementation:8 tests, OK.
3. Service manifest RED same command: ModuleNotFoundError services.common; GREEN10 tests, OK.
4. Failed-start stop regression RED: ValueError stale session from test_failed_start_stop_idempotent. GREEN13 tests after tombstone cleanup support.
5. Preparation cancellation regression RED: test_stop_during_preparation_cannot_install_late_session failed "stop blocked behind preparation". GREEN14 tests after releasing lifecycle lock during preparation/cancellation gate before output installation.
6. preparing status regression RED: KeyError preparing. GREEN16 tests after explicit flag in empty and existing state. Includes authenticated local Worker and model HTTP health requests; unavailable model503/ready false, unauthenticated401.
7. Real CPU synthetic output RED: FFprobe "Invalid data found when processing input", zero-byte short FLV. Root cause buffered output plus startup input probing. Added explicit flush_packets1 and minimal probe duration; retained optional real test_small_frame_mux_and_process_cleanup.
8. Material versioning regression RED: test_material_registration_hash_versioned raised immutable-look ValueError. GREEN after hash-versioned model profiles; users may change stopped look default idle without inventing a new look ID.
9. Final command `uv run --python 3.12 --with numpy==1.26.4 --with opencv-python-headless==4.10.0.84 python -m unittest discover -s worker/tests -v`:17 tests, OK, including synthetic128x12825fps idle -> real local FLV video+audio streams, duration>1s and process closed. No network output.
10. `python3 -m compileall -q worker services`: exit0. `uv run --python 3.12 --with ruff==0.14.6 ruff check --select E4,I --fix worker services` + `ruff format worker services`: all checks passed;14 files formatted. Imports split/sorted, standard indentation.

## Attribution and implementation rulings

Read-only reuse from old worker: AudioWindows copied; MuseTalk frame callback/pose rotation and PCM resampling adapted; FFmpeg audio-first per-frame interleaving informs new Linux-only output. No private old avatar manifests, deployment addresses, config or credentials copied.

Ruling: model images Python3.10 rather than plan's broad3.12 baseline because pinned MuseTalk numpy1.23.5/TensorFlow2.12/cu118 upstream stack does not support Python3.12. Worker remains3.12. Wrong choice cost: separate build environments required, documented for integration.

Ruling: no rendered-media cache in V1. Fingerprints cover fixed source IDs, voice reference bytes, look hash and exact text; removing replay storage avoids stale-cache risk. Models/face preprocessing remain resident/persistent. Wrong choice cost: repeated fixed text reruns inference.

Ruling: stopped-but-preparing explicitly remains busy until in-flight model call returns (max600s prepare timeout), preserving isolation after stop without claiming model-side cancellation. Wrong choice cost: restart may temporarily409; no stale output installation. Worker audio/FFmpeg/relay immediately cancel; resident preparation itself may still be running.

## Actual acceptance gaps

Host is macOS ARM without NVIDIA. CPU/mock HTTP tests do not prove CosyVoice/MuseTalk loading, pretrained weights compatibility, actual face quality, GPU throughput, continuous SRS playback, external relay reliability, remote TLS deployment, Windows WSL2 or Linux NVIDIA execution. Model images are runnable definitions supplied by integration owner but are not built/verified here; exact transitive package/image and weight snapshot checksum lock still requires operator record. No real model assets were downloaded. Actual real GPU prepare/render->SRS end-to-end remains required before release. Test output is local temporary FLV only.

## Task3 review fix round1

Read `subagent-driven-development/references/fix-loop.md`. Accepted all3 Important findings and the controller's long-text contract integration correction. No endpoint changes.

### RED evidence

`python3 -m unittest discover -s worker/tests -p test_review_cleanup.py -v` initially ran3 tests, FAILED(failures=3):

* `test_stop_during_final_fetch_skips_preparation`: expected prepared=[]; actual [('look', SHA256)] after stop and final fetch release. Added cancellation gate directly before model preparation.
* `test_concurrent_stop_does_not_release_capture_or_close_fd_twice`: expected closed=[123]; actual [123,123]. HTTP stop and writer-finally could close same descriptor. Resources now detached exactly once under lifecycle lock before slow termination. Writes share that lock so no write targets a detached/reused fd. VideoCapture is local to the writer and release occurs on that thread only.
* `test_stop_interrupts_blocked_render_and_allows_retired_restart`: stop thread still alive after1second waiting for a real localhost HTTP response body. Model render transport now tracks explicit HTTP(S) connections and socket references, shuts down socket reads on cancellation, and closes transport on completion. Session stop captures retiring thread/generation under lock, waits at most5seconds and rejects replacement until the actual output/inference thread retires. `/status preparing` also stays true while retiring; backend reconciliation contract unchanged. Renderer thread is joined by writer-finally, eliminating detached inference mistaken for retired session.

`python3 -m unittest discover -s worker/tests -p test_text_chunks.py -v` RED3tests:600char request exceeded240, long Chinese text produced one unbounded request, scheduler rejected9000-byte business text. Worker now accepts the contract's10000UTF8byte text; ModelClient splits sequential renderer calls into <=240Unicodechar chunks, preferably sentence/newline boundaries after120chars, preserving exact original concatenation. Cancellation checked between chunks; renderer still invokes TTS internally once per chunk. JSON request envelope limit raised64kiB for escaped Unicode representation; no wire shape changes.

### GREEN evidence

Targeted cleanup command now5 tests OK, including `test_new_start_rejected_until_old_generation_retires` and `test_writer_exclusively_releases_capture_after_blocked_read`. The latter blocks native-style read while HTTP stop runs; only the writer releases capture after read returns. Concurrent teardown test asserts exactly one audio-fd close. Local blocked-render HTTP regression completes stop within1second and immediately starts a new session only after retirement.

Targeted chunks command now3tests OK: `test_long_chinese_text_preserves_every_character_in_bounded_requests`, `test_cancellation_does_not_request_next_text_chunk`, `test_worker_queue_accepts_business_text_byte_limit`.

Final `uv run --python 3.12 --with numpy==1.26.4 --with opencv-python-headless==4.10.0.84 python -m unittest discover -s worker/tests -v`:25tests, OK (8.75seconds), preserving all original17cases including real local FFmpeg FLV smoke. `python3 -m compileall -q worker services`:exit0. Ruff import/indent checks and format passed. Only Worker paths changed for this fix. GPU/model image prerequisites and real hardware acceptance gaps remain as above.

## Task3 review fix round2

Accepted both new Important lifecycle findings; prior final-fetch/capture/FD/transport/text fixes preserved without reimplementation. Changes restricted to worker/output.py, worker/session.py and focused test file; deployment-owned worker/app.py untouched.

RED `python3 -m unittest discover -s worker/tests -p test_review_lifecycle.py -v` reproduced both new tests failing (initial test module imported an existing TestCase class, so5existing tests also ran; import changed to module import to avoid duplicate discovery):

* `test_writer_failure_cancels_renderer_and_reports_session_error`: `writer-finally hung joining idle renderer` after forced `_write` OSError. Output now sets cancellation and interrupts active model transport before renderer join. Original writer exception is retained in `failure` and re-raised. Session `_run` distinguishes explicit stopped state from failure-induced cancellation, so error status reports original OSError rather than suppressing it.
* `test_publication_stop_waits_for_retiring_generation_and_cleans_material`: expected preparing true while blocked previous output was retiring, actual false. Deterministic lock wrapper suspends start immediately after live publication, allowing stop at the exact prior gap. Preparation is now cleared inside the same lock as live publication/thread installation; removed the later successful-start finally write. Stop therefore always takes full live retirement/cleanup path at this boundary, directory cleanup occurs and replacement conflicts until old thread exits.

GREEN focused same command:2tests, OK (0.002seconds). Full suite run once after the fix and formatting: `uv run --python 3.12 --with numpy==1.26.4 --with opencv-python-headless==4.10.0.84 python -m unittest discover -s worker/tests -v` ->27tests OK (8.737seconds), including original FFmpeg FLV smoke and all round1 regressions. `python3 -m compileall -q worker services` exit0; scoped Ruff checks/format passed. No REST interface changes or GPU claims.

## Task3 review fix round3

Accepted new Important finding: `_run` overwrote an already recorded inference error with secondary writer teardown RuntimeError. Focused deterministic regression `test_inference_failure_is_not_overwritten_by_writer_cleanup_error` blocks `_write`, then enqueues inference that raises ValueError; inference shutdown closes output and unblocks writer with RuntimeError. RED `python3 -m unittest discover -s worker/tests -p test_review_lifecycle.py -v`:3tests,1failure; actual final error `RuntimeError: live output failed`, expected ValueError. Small guard change makes `_run` record only when same generation has neither stopped nor already entered error, preserving first failure. Same focused command GREEN3tests OK (0.027seconds); original inference ValueError remains and cleanup thread exits.

Full suite run once: `uv run --python 3.12 --with numpy==1.26.4 --with opencv-python-headless==4.10.0.84 python -m unittest discover -s worker/tests -v` ->28tests OK (8.745seconds), real FFmpeg smoke and all prior regressions included. Scoped Ruff check/format passed; `python3 -m compileall -q worker services` exit0. Only worker/session.py and worker/tests/test_review_lifecycle.py changed; worker/app.py untouched. No wire changes or additional acceptance claims.
