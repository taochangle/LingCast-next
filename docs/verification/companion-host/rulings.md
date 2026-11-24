# Rulings I made

实施期间的裁定，按发生顺序保存；成本说明见各条。此前用户提供的工作约定随后已撤销，已确认的功能范围不变。

- Ruling: Work in place on feat/initial-version — project AGENTS.md explicitly mandates this existing feature branch, user authorized execution here, worktree creation is optional and has no prior consent — cost: no separate linked worktree isolation; maintain clean commits and no old-repo writes.

- Ruling: The new approved companion plan supersedes old fixed-only hosting behavior where explicitly changed; all other V1 constraints remain — new feature necessarily adds autonomous generation — cost: migration/compatibility tests required.

- Ruling: Task 1 owns foundational types/storage and safety; Task 2 owns receipt advancement; initial RecordPlayed may persist facts without downstream advancement until Task 2 — staged dependency — cost: intermediate commits not whole feature.

- Ruling: Interests/persona may change during a live session and invalidate unsubmitted drafts; voiceId/name and hostMode remain locked for active avatar — approved plan says edits affect next generation but media identity remains fixed — cost: narrow partial PATCH handling needed.

- Ruling: Storage host_turns retains only successful spoken outputs and topic progress; raw viewer inputs stay transient bounded memory — privacy constraint controls over broad wording on context — cost: viewer turns disappear on restart.

- Ruling: Do not store cancelled/failed generated text or attack payloads in persistent diagnostics; receipts may be transient and diagnostic statuses only — safety and privacy boundary — cost: less raw failure detail.

- Ruling: Real GPU/60 minute listening is conditional on user GPU/idle readiness; execute all CPU/integration evidence now and clearly mark listening unverified — approved Task 7 explicitly says later — cost: naturalness not accepted by code tests.

- Ruling: Initial same-topic key exclusion means do not generate a separate topic with that key for seven days; the current active topic may continue through different angles — coherence and dedup requirements otherwise contradict — cost: define active-vs-reuse distinctly.

- Ruling: Latest user “确认 开始执行” authorizes the linked seven-task plan and proposed subagent execution — fresh task1 agent lacked conversation due deliberate no-history context; supplied explicit approval quote — cost: none, no scope expansion.

- Ruling: Enforce new/edited persona max4096 Unicode characters at API; preserve preexisting long persona storage and use a bounded model-facing representation — aligns accepted settings with generation while preserving migration data — cost: legacy overlong persona context truncated, original retained.

- Ruling: Task7 extend existing CPU Compose smoke to autonomous lifecycle if practical — changed Go/Python receipt boundary requires real transport verification beyond independent unitfakes — cost: localintegration fixture/test changes, no realGPUclaims.
