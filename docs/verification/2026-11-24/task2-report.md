# Task 2 report — React 管理端

Status: implemented; focused CPU/UI validation GREEN. Scope only frontend/. Contracts read: docs/contracts.md, Task 2 plan, design, root AGENTS.md. No old LingCast checkout changed. Impeccable was consulted as an Operate UI; existing explicit Chinese/dark studio brief substituted for additional design interview because this is a delegated implementation task with approved architecture. No subagents spawned.

## Implementation

React 19 + TypeScript + Vite, same-origin Bearer API client, sessionStorage admin token with disconnect clearing. Avatar CRUD/persona/voice ID; per-avatar look CRUD/image upload; idle upload/delete/default bottom plate; categorized script CRUD. Current live avatar's deletion paths disabled; server remains authoritative for all ownership/material/live validation. Upload client caps 30 MiB image and 200 MiB video; real decode checks belong to backend.

Studio: required avatar/look/platform, only avatar-owned looks, selection cleared on avatar change, default-idle required. Live start/stop; failed-session explicit cleanup. Manual text, host pause/resume, five-second status polling. Collector origin/session/platform and password-masked token, explicit rotation/copy. Tokens not bundled or logged. Explicit external relay enable and disable plus independent stop command after reload; startup never calls relay. MediaSource FLV preview uses authenticated same-origin Go proxy and lazy mpegts.js, cleans up on URL/session change. Settings read-only from environment-backed safe API, explicit connectivity check. Chinese error/loading/empty/disabled states, keyboard focus, reduced motion and responsive layout.

## RED evidence

Initial `npm test -- --run` attempt happened before dependency install completed and failed `vitest: command not found`; this is setup failure, NOT behavioral RED.

Meaningful RED: `npm test -- --run src/api.test.ts` produced 1 failed / 1 passed. `handles empty success bodies` failed with `SyntaxError: Unexpected end of JSON input`, proving successful empty-body stop replies were not handled. API client then changed to text parsing with an empty-body branch. Initial feature tests were written before the App but first executed once implementation existed; no claim they supplied a behavioral RED.

## GREEN evidence

Final `npm test -- --run`: 2 test files, 7 tests passed. Tests cover API server error text/Bearer header; empty success; required start selections and avatar ownership filtering; platform payload; no automatic relay; visible API errors; explicit host/stop actions; clearing stale look on avatar change; explicit relay enable only after click. Mock requests only, no external relay.

Final `npm run build`: TypeScript succeeded, Vite succeeded in 502 ms. App JS 245.14 kB / gzip 76.75; lazy mpegts 277.19 kB / gzip 64.27; CSS 7.97 kB. No chunk-size warning after splitting codec. `git diff --check -- frontend` clean.

## Actual visual inspection

Local Vite only at http://localhost:5187, fake non-secret admin token `local-ui-verification`. CUA rendered screenshot inspection: 1280×720 desktop studio with navigation/preview/preparation and visible backend-unavailable HTTP 500 alert. 390×844 responsive studio plus avatar empty state/create form: readable Chinese, navigation wraps within viewport, panels/form inputs fit viewport, no horizontal overflow observed. Viewport override reset afterward. Backend was unavailable then, so no successful backend-connected UI flow is claimed. No real platforms visited, no external RTMP enabled. Dev server may remain on port 5187 for integration.

## Limitations / integration follow-up

- Real backend CRUD/upload, actual MP4 decode failures, GPU/model startup and FLV bytes playback still require integration validation. Tests use fake responses, not inference.
- No real platform DOM, long-running stream, Windows/Linux hardware, or relay output acceptance.
- Relay GET/state is absent from frozen contract; UI does not infer current relay state after reload and always provides explicit `确保停止转推` command. Collector token intentionally cannot be recovered from API after reload; rotate it and reconfigure plugin.
- Settings check displays safe server response as text. Actual secret redaction is backend-owned.
- Browser previews require MediaSource support. Network and codec errors are shown; no fabricated healthy preview.
- UI screenshot evidence inspected live, not saved as a committed artifact. No model weights/media/real credentials in frontend paths.

## Review round 1 — async selection and live-state races

Fixed Important findings: manager look/idle/script loads now commit only for the current selection and latest request generation; effect cleanup cancels superseded selection loads. Manager fieldsets and page navigation lock during mutations, preventing mutation results from landing in a different editor. Busy state counts overlapping commands. Background reads no longer lock selection.

Live polling/refresh use a generation guard. Every command invalidates preceding reads before/after its mutation; polling skips pending commands. Superseded reads (including failures) cannot restore pre-start/pre-stop/pre-host state. Collector token and relay enable state carry session identity; accepting a different or stopped session clears credentials, relay inputs and enable state. Explicit stop remains available after reload. Login, stored API authorization and preview token normalize whitespace consistently.

Added deferred-promise race tests for old scripts, old look and idle responses, pre-start null poll, pre-host enabled poll, pre-stop running poll and cross-session credential/state reset. Ran the completed regression tests against the original `8a45e2d` App (temporarily substituted inside a Python try/finally, restored afterward): exit 1, 5 failed / 5 passed; failures were old script/look and pre-start/pre-host/pre-stop response overwrites. First poll-test attempt had a fixture bug (GET was explicit and Testing Library also uses setInterval); corrected fixture before baseline evidence above. No claim fixture failures were product regressions.

Formatted TSX, TS, CSS, HTML and JSON config with pinned `npx --yes prettier@3.6.2 --write`; no formatter dependency added and package-lock.json unchanged. Final post-format commands: `npm test -- --run` → 2 files / 13 tests pass (975 ms); `npm run build` → TypeScript and Vite pass (503 ms); `git diff --check -- .` in frontend → clean. No real model/media/platform/relay validation added in this fix round; original acceptance limits remain.

## Review round 2 — slow polling starvation

Read the SDD fix-loop reference. Confirmed Important finding: the five-second interval launched overlapping GETs, each invalidating the previous request generation; responses taking consistently longer than five seconds could all be discarded.

Scoped fix: track live GETs in flight and skip only background interval ticks until reads settle. `readLive` decrements its count in `finally`, including failure. Explicit refreshes and command-driven refreshes still execute immediately and retain generation invalidation, so a stale read cannot override a command. No API, dependencies or other UI changes.

Covering test: frontend/src/App.test.tsx `serializes slow background polls and accepts their results` holds a deferred poll, invokes two additional interval ticks, verifies no extra GET was started, resolves the slow response and verifies its session was accepted, then verifies a subsequent poll runs and updates state. RED `npm test -- --run src/App.test.tsx`: 1 failed / 11 passed; regression failed `expected 4 to be 2` at live GET count. GREEN after fix and pinned Prettier: `npm test -- --run` → 2 files, 14 tests passed (1.04 s); prior delayed pre-start/pre-host/pre-stop tests remain green. `npm run build` → TypeScript and Vite pass (516 ms). `git diff --check -- .` in frontend clean. Original hardware/platform acceptance limits unchanged.
