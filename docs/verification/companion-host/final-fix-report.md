# Whole-branch review fixes

Base `599d3c5`, feature branch `feat/initial-version`. One fix wave for all seven findings. The SDD report is local under the repository's ignored .superpowers directory.

## Outcomes

1. A matching Worker status of error revokes the collector, cancels the director draft, and persists session error from the companion tick or the live GET. A failing receipt endpoint, paused hosting or an LLM retry deadline cannot suppress the tick's status observation. The storage update checks the currently live session under the lock. The slot remains occupied until explicit stop reconciliation. A mismatched error has no authority. The existing fixed scheduler's failure policy and stop reconciliation remain in force. Coverage includes both enabled states and both observers, first GET response, revoked collector request, blocked new start, draft invalidation and successful explicit stop. The director model-failure ladder and the soak exercise recoverable generation failures.
2. The director queries ready or current active topics, filters unresolved registrations in SQL, then orders eligible candidates with the current active topic first. The admin page retains its 200-row display limit. Four ready topics behind 200 disabled rows still produce speech on a real tick.
3. Retention retires ready material with pending or unknown playback whose owning session has released its slot. The terminal transaction removes the slot and prunes that avatar atomically. Existing abandoned records are eligible on subsequent pruning. A partial index covers unresolved topic registrations. The live late receipt still advances the first angle. Following stop, the unresolved rows retire and expire. The first-ever pending submission with no host_runs cursor is covered separately.
4. The interaction queue shares a failure deadline across comments within the session. An immediate model outage causes one inference attempt per deadline. Queued and fresh comments receive their original retry IDs, and older comments expire by first arrival. Expired comments are acknowledged without speaking. The queue lock is released during inference and no timer or mutex sleep is introduced. Worker submission errors do not count as model failures. Successful generation resets the model deadline. Existing partial-failure coverage still verifies one failed Worker attempt and one redelivery under the same ID, using an advanced fake clock and ordering the Worker failure before the model outage.
5. Explicit credential indicators receive secret-exfiltration, including when a separate instruction-override pattern is also present. Injection-only candidates receive injection. Both short-circuit the model audit and deny speech.
6. Close signals shutdown and cancels/removes the director under the same lock used to acquire tick ownership. A post-close timer callback or direct tick cannot create a director. Reading status does not allocate one. Shared fixtures now instantiate an open app without starting its ticker so fake-clock tests can explicitly drive an open scheduler.
7. The CPU smoke message and verification record now describe a completed viewer reply. The smoke separately observes the reply's Worker completion and the next model request's prior autonomous turn. It does not independently correlate an audit invocation to the reply.

## Focused RED and GREEN

From backend, against the original implementation with the new tests:

```sh
go test ./internal/app -run 'Test(MatchingWorkerErrorOccupiesSlot|DirectorSelectsEligibleBeforeDisplayLimit|UnconfirmedTopicsExpireAfterSession|InteractionClassOutageBackoff|DeterministicCredentialAndInjectionCategories|ClosedAppCannotStartDirector)$' -count=1
```

Exit 1. Four failed worker-observer/enabled cases reported `status=live`. The selection case reported `older ready topic not scheduled sql: no rows in result set`. Retirement returned `retired count 0 <nil>`. Burst 0 made 4 calls for 4 retry IDs. The shutdown test reported `director work after shutdown`. Both credential probes returned injection. The same focused command passed after implementation (`ok lingcast/backend/internal/app 0.624s`).

A self-review case exercised stopping before the first-ever receipt. Initially, retirement depended on a host_runs row, which is created by a cursor write rather than by task registration:

```sh
go test ./internal/app -run '^TestStopBeforeFirstReceiptRetiresTopic$' -count=1
```

Exit 1, `unresolved first topic without receipt cursor 1 <nil>`. The fix checks registered topic/session identity directly. The final focused race run below passed that case.

An existing collector test initially failed its immediate retry after a new model cooldown (`{0 [good] [bad-llm bad-worker]}`). Its revised event ordering still exercises both downstream failures. It advances the fake clock to five seconds before retrying and checks exact stable-ID Worker attempt counts.

Final combined focused run from backend:

```sh
go test -race -v ./internal/app -run 'Test(MatchingWorkerErrorOccupiesSlot|DirectorSelectsEligibleBeforeDisplayLimit|UnconfirmedTopicsExpireAfterSession|StopBeforeFirstReceiptRetiresTopic|InteractionClassOutageBackoff|DeterministicCredentialAndInjectionCategories|ClosedAppCannotStartDirector|CollectorPartialFailuresAndDurableAcknowledgement|DirectorFailureBackoff|CompanionSoak90Minutes|RetryPreemptsApprovedHostDraft|GeneratedSpeechFailClosed|LateGenerationCannotCrossSession|TopicReserveLimitsAndSingleflight|LateFailuresRespectReadyCap|RecentTopicsExpire)$' -count=1
```

16 top-level tests passed. `ok lingcast/backend/internal/app 6.426s`. Simulated 90 minutes produced 85 autonomous submissions, 29 distinct topics, one missing receipt and one failed receipt. The interaction fake clock exercised 5, 15, 30, 60 and 60 seconds, requests just before each deadline, post-outage expiry, recovery, stable IDs and reset to five seconds. No RED assertion was needed for the smoke wording change. The final smoke below exercised its exact success output.

## Full checks on final code

- `cd backend && go test -race ./... && go vet ./...` passed. App tests took 14.450s, vet returned 0.
- `python3 -m unittest discover -s worker/tests -v` ran 36 tests in 3.143s, OK (skipped=1, host lacks OpenCV). `docker run --rm --network none -v "$PWD:/src:ro" -w /src --entrypoint python lingcast-task5-worker:latest -m unittest -v worker.tests.test_output_smoke` ran the skipped FFmpeg test using the current source, 1 test in 5.706s, OK.
- `cd frontend && npm test -- --run && npm run build` passed 28/28 tests in 3 files. TypeScript and Vite production build succeeded.
- `bash scripts/check-compose.sh` returned 0 for all five configurations.
- `bash scripts/smoke-compose.sh` returned 0. Output included “Signed media, real Worker startup and private SRS FLV preview verified”, “Manual speech, deduplicated events and long text reached the synthetic renderer verbatim”, “Autonomous speech, Go receipt consumption and completed viewer reply verified”, and “Stop invalidated the session, its collector token and stale tasks”. The isolated Compose project was torn down.
- `git diff --check` passed.

Real GPU inference, perceived naturalness and a wall-clock 60-minute listening session remain NOT RUN. Smoke inference was synthetic and used local private output. No push, external deployment or relay.

## Changed files

Runtime under backend/internal/app: app.go, host_api.go, host_director.go, host_generation.go, host_store.go, interaction.go, live.go, llm_safety.go, store.go. Regression and fixture updates in the same directory: app_test.go, collector_ack_test.go, companion_interaction_test.go, companion_soak_test.go, host_director_test.go, llm_safety_test.go and prompt_attack_test.go. Smoke/evidence: tests/integration/smoke.py, docs/contracts.md and docs/verification/companion-host/README.md.
