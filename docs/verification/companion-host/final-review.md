# Whole-branch review at 599d3c5 (base f201c97)

## Strengths

Worker terminal receipts carry the original task text and require the final segment and successful output drain (worker/session.py:182). Go updates the playback cursor, spoken turns and topic progress in a single transaction. Duplicate and truncated pages have distinct handling (backend/internal/app/host_store.go:223). The host receipt-gap follow-up is covered in backend/internal/app/host_director_test.go:303.

Generation and audit use separate constrained messages. The application owns task registration, session identity, revision checks and scheduling. Viewer input stays in data fields. The interaction path fences late successful replies, caps its FIFO, and retains the original first-seen time across retries. The frontend discards obsolete reads and clears protected panel data on an unauthorized response. The recorded CPU smoke exercises real Go-to-Worker transport. Test outcomes are taken from the task reports and verification record.

## Critical

None identified.

## Important

1. **A terminal Worker failure leaves the companion session live.** backend/internal/app/host_director.go:143, backend/internal/app/interaction.go:355 and backend/internal/app/live.go:28. A renderer/output failure moves the matching Worker to error and stops output. The companion branch turns that status into a retryable host-generation error, while the live GET reads only relay state. With hosting paused it skips the Worker status check entirely. The studio can show a live session and the collector remains authorized after output has died. Preserve the fixed branch's terminal session handling for an explicitly failed matching Worker, including when hosting is paused. Exercise both pause states, collector revocation and the session shown by GET. Keep temporary LLM errors recoverable.

2. **Usable reserve count and director selection can disagree permanently.** backend/internal/app/host_api.go:60, backend/internal/app/host_director.go:86 and backend/internal/app/host_generation.go:215. The director searches the newest 200 topics across all states. Repeated manual generation and disabling can leave four or more older ready topics behind 200 newer disabled records. Ready rows are protected from pruning. Refill sees sufficient stock and skips generation while selection returns no rows, so autonomous speech stays degraded across retries. Query eligible director candidates before applying a display limit. Test a reserve hidden behind 200 newer unavailable rows.

3. **Unconfirmed first angles can accumulate across sessions indefinitely.** backend/internal/app/host_store.go:362 and backend/internal/app/host_generation.go:211. A first-segment submission without a terminal receipt leaves its topic ready but unselectable. Refill disregards these topics and pruning protects every ready row, even when the session has ended and its Worker route is no longer readable. Repeated stop-during-first-segment cycles accumulate full topic material beyond the age/count limits. Retire unresolved topics on terminal session cleanup or make old unselectable ready rows eligible for pruning after their receipt-reconciliation lifetime. Verify that a live late receipt still reconciles, and that retired-session material expires.

4. **A model outage can trigger one failed interaction request per queued comment without backoff.** backend/internal/app/interaction.go:310 and backend/internal/app/interaction.go:328. Each job calls generation directly and immediately advances to the next queued job on transport failure. With fast failures and fresh arrivals, the queue's size and single-flight gates limit concurrency but do not limit request rate. Apply the planned per-class 5/15/30/60-second failure deadline to interaction generation and preserve original event age and stable IDs. Test a multi-event burst against an immediately failing model with a fake clock.

## Minor

- backend/internal/app/llm_safety.go:171 groups explicit credential probes with injection. Output is denied, but category-level evaluation misattributes these incidents. Split the deterministic categories and test them.
- backend/internal/app/app.go:220 and backend/internal/app/host_director.go:161 allow a tick that passed the timer select just before Close to create a director afterward. Gate tick creation under the lock against shutdown, and test that no work begins after close. This mainly affects shutdown and embedded usage.
- tests/integration/smoke.py:272 calls the viewer reply audited, although the smoke independently observes its receipt rather than the corresponding audit invocation. Narrow the message or assert the candidate-specific audit call in the fixture.

## Recommendations

Add focused regressions for the four Important cases at the affected boundaries. The existing successful soak and happy-path CPU integration do not exercise these configurations.

## Declined to judge

- Real GPU performance, perceived delivery quality, audiovisual continuity and at least sixty minutes of wall-clock endurance require the conditional equipment and listening session.
- Production-model prompt-attack false positives and false negatives require representative inference and evaluation; the synthetic corpus covers application controls.
- External platform ingestion and viewer reception require external relay and audience-side evidence, outside the approved local execution scope.

## Assessment

**Ready to merge? With fixes.** Receipt attribution and source boundaries are sound in the reviewed paths. Terminal live-state propagation, reserve selection/retention and interaction outage backoff need correction before merge. No suites or integration runs were repeated in this review.
