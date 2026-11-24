# 互动陪伴型自主主持 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 数字人在无人互动时有连贯且不机械重复的内容，有人互动时自然接话，并在提示词攻击下保持规则及权限边界。

**Architecture:** 沿用 Go 业务调度、SQLite、现有 LLM、Python Worker 与配音/口型链路。Go 维护主题储备、话题进度、近期记录与生成版本；LLM 只输出结构化内容草案，校验后逐小段交给 Worker。播放结果驱动记忆更新，不把生成或入队当成已经说完。

**Tech Stack:** Go、SQLite（modernc.org/sqlite）、OpenAI-compatible LLM、Python、React/TypeScript；不引入新的模型服务、向量数据库或 Agent 框架。

**Spec:** 本文「需求与实施决定」为本次计划的需求基线；另读 `docs/contracts.md` 与 `docs/superpowers/specs/2026-11-24-lingcast-next-design.md`。用户要求先写计划；当前仅编写计划，不执行产品代码改动。

## Global Constraints

- 单场直播、固定人物/形象/平台，沿用现有开停播和手动口播。
- 定位为互动＋陪伴；「1 小时」是内容储备基准，手动停播，没有一小时自动结束或告别。
- 采用主题素材＋即兴主持；自动生成的素材校验后直接使用，允许运营者修改，无强制人工审核。
- 固定话术和手动插播原文不被 LLM 改写；新增自主主持为独立模式。
- 当前服务器时间和时区为可信事实；不编造观众、评论、礼物、实时新闻、天气或现实经历。
- 观众昵称、评论、历史对话、模型生成素材均不拥有指令权限；模型无工具、文件、配置或服务操作权限。
- 使用现有 LLM 配置；不改 GPU、端口、Compose 拓扑，不部署、不自动外部转推。
- 不宣称提示词攻击可被完全防住；不宣称 CPU 测试证明自然听感或真实 GPU 效果。

## Review Focus

1. 长时间无人互动、主题耗尽或重启：继续储备、避免短期循环，不自动结束；Task 3/4/7 覆盖。
2. LLM 慢请求与暂停/停播/新会话交错：旧结果不能播进新状态，管理操作不被锁阻塞；Task 4 覆盖。
3. 生成、接收、部分播放、完整播放不是同一事实：失败或丢失回执不得进入「已经讲过」记忆；Task 2/4 覆盖。
4. 昵称、素材、多轮摘要中的间接注入：不能变成人设或控制命令，不能直接朗读攻击载荷；Task 1/5 覆盖。
5. 高频互动、长评论和 LLM 故障：队列有界、评论不过时积压、固定稿保真、画面可继续 idle；Task 5/7 覆盖。

---

## 需求与实施决定

### 已确认

- 用户不需要自己编整场话术；系统准备内容并自动组织。
- 人物无人互动时能展开自己的内容，有互动时能接话，互动结束后自然继续。
- 内容准备直接使用；防提示词攻击是硬要求。

### 本计划建议的具体值（实施选择，非用户原话）

- 每个人物增加兴趣列表 `interests`，最多 5 项、每项 40 字；留空时由人设生成可编辑的兴趣建议，不编个人经历。保留人设、兴趣、固定话术各自职责。
- `hostMode=fixed|companion`；现有人物迁移为 `fixed`，新人物默认 `companion`。原 `hostEnabled` 仍控制暂停/恢复。直播中不修改本场模式；开播请求可显式选择模式，默认使用人物设置。
- 一批 12 个主题，每个 3～5 个可展开角度；可扩展为约 1 小时的主题储备，不承诺固定字数等于一小时。未用主题少于 4 个时补充一批，最多保存 36 个待用主题。
- 每个主题包含标题、角度、日常观察/明确标记的虚构例子、去重键；生成出来的「知识」不是已核实事实。第一版不联网检索，不自动播具体时效事实、医疗/投资建议或无法核实的数字/引语；改用观点、假设和生活话题。
- 自主口播每段 40～120 个 Unicode 字符，1～3 句，绝不超过 Worker 240 字符分段上限；纯欢迎/感谢可更短。每个话题通常 3～6 段，但允许因互动提前切换。
- 通常段间留白 2～5 秒，话题切换 5～10 秒；连续 5 分钟无评论，下一次话题切换可留白 10～20 秒。注入时钟/随机源测试，不随机到每个 tick 重算。问题邀请不多于每 3 个自主段一次，无回复时继续而非催促。
- 只生成 1 个在途自主段，最多缓存 1 个待播自主段；只在 Worker 空闲且没有高优先级任务时提交。评论来了丢弃尚未提交的自主草稿，不能越过正在播的小段做强制音频截断。
- 保持 Worker 原优先级 manual > gift > like > comment > welcome > host；本期不改成评论压过礼物。连续评论优先延续真实交流，简短感谢后可接回话题。
- 上下文仅用本场最近 12 个成功播放的片段、当前主题/角度和最近 20 个已处理真实互动；不做观众身份长期记忆。不持久化原始昵称/评论，待处理事件短期内存有界；日志不得原样记录攻击载荷。
- 跨场只保留该人物最近 7 天、最多 200 条主题键/角度键和最近 100 段自主口播摘要/文本，超额按时间清理。中断话题进度只属于本场；不把上一场当成仍在场的对话。
- 同主题键在 7 天内不复用；同段精确归一化去重，加字符三元组 Jaccard >=0.75 拦截近似复读（不足 3 字按精确匹配）。这是第一版启发式，不承诺语义去重。
- 每个生成/互动请求超时最多 20 秒；同类生成失败退避 5/15/30/60 秒（上限 60），有效但重复/不合规的响应每次最多重试 1 次。无合规内容时保持 idle 并显示原因，不拿空泛套话无限填充。
- 人物/兴趣编辑影响下一次生成；已在播文字不变。用管理员配置修订号使尚未提交草稿失效；不把观众内容提升为管理员配置。

### 安全边界

可信控制来源只有管理员配置、固定服务规则、服务器时间和经验证的播放状态。观众输入及模型返回必须经类型/长度/来源校验，以 JSON 数据字段进入上下文，不允许插入 role、system、developer 或任意消息对象。不自动 Base64 解码、不访问评论链接。模型仅允许 `speak` 或 `skip` 结果，所有输出字段是受限数据；模型返回的「修改设置」「调用工具」等字段一律拒绝。

生成文本经过规则检查（敏感配置/提示词探测、越权请求、结构和长度、明确攻击模式）及无权限审核调用；审核只返回 allow/deny/category，失败默认不自动播出。审核模型不是信任根：仍不能改变服务器状态，且拒绝理由不得把攻击原文带回口播。初始类别为 injection、secret-exfiltration、unsafe-content、unsupported-factual-claim；规则和审核的漏报率通过验收记录，不能作为绝对安全保证。

记忆不采用自由文本「长期规则摘要」。可信主题进度由应用根据完成的 taskId 更新；对话内容只以有来源的 spoken/viewer 数据保留。被拦截输入/输出不进入后续内容上下文。固定稿为管理员原文，不走改写或生成审核；自动互动不得借固定稿机制插入观众原文。

## 文件与契约分工

新增 Go 文件保持每项职责独立，不继续把逻辑堆入 `interaction.go`：

- `backend/internal/app/host_types.go`：配置、主题、草稿、播放事实类型。
- `host_store.go`：SQLite 主持记录、主题和过期清理；复用现有数据库连接，不建第二套数据库。
- `host_generation.go`：主题/短段生成、去重和结构校验。
- `llm_safety.go`：可信提示构造、输入输出限制、审核结果解析。
- `host_director.go`：本场调度、取消、恢复和播放确认。
- `host_api.go`：主题管理与主持状态，只挂管理员路由。
- 修改 `app.go`、`store.go`、`live.go`、`interaction.go`、`llm.go`：迁移、路由、事件接入和生命周期。
- 修改 `worker/session.py`、`worker/app.py`：有界播放回执；不修改推流/模型协议。
- 新增 `frontend/src/components/HostContentManager.tsx`、`HostStatus.tsx`；修改 `App.tsx`、`types.ts`、相关测试，沿用已确认钴蓝设计。

核心类型及接口（字段采用 camelCase JSON）：

- `HostTopic{id,avatarId,title,angles:[]HostAngle,state:ready|active|done|disabled,createdAt}`；`HostAngle{id,text}`，题目 ≤80 字、角度 ≤200 字。
- `HostDraft{taskId,sessionId,revision,topicId,angleId,text,pauseMs}`；pauseMs 由应用计算，不由模型控制。
- `TaskResult{seq,taskId,state:completed|failed,text,completedAt}`：text 是 Worker 实际处理的原文；只有完整交给输出管道且最终段成功才为 completed，不等于平台观众已听到。
- `HostStatus{mode,state:paused|preparing|waiting|speaking|degraded,topicTitle,lastError,nextEligibleAt}`；错误为面向运营者的中文，不返回提示词/密钥。
- `BuildTopics(ctx,HostInput) ([]HostTopic,error)`；`BuildHostDraft(ctx,HostInput) (HostDraft,error)`；`CheckGeneratedSpeech(ctx,SpeechCandidate) (SafetyDecision,error)`。
- `HostInput` 明确包含 persona、interests、serverTime/timezone、currentTopic、spokenTurns、viewerTurns、recentTopicKeys；不接受外部自定义 messages。
- `SafetyDecision{allow bool,category string}`；`SpeechCandidate{source:host|interaction,text,context}`。
- `GET /api/avatars/{id}/host-topics`；`POST .../host-topics/generate`（202，进行中则复用任务、不重复创建）；`PATCH /api/host-topics/{id}` 只允许 title/angles/state=disabled，修改 active 主题使未提交草稿失效。
- `GET /api/live/{id}/host-status`；现有 avatars POST/PATCH 增加 interests/hostMode；现有开播请求增加可选 hostMode。
- Worker `GET /sessions/{id}/task-results?after=<seq>` 返回 `{results,nextSeq,truncated}`，保留当前会话最近 256 个终态，不携带任何管理权限。Go 每次 hostTick 先拉取，按 `(sessionId,taskId)` 幂等落库；结果缺口时未确认任务记 unknown，不伪造完成、不自动重播。

## Task 1: 内容数据、配置与可信输入边界

**Files:** Create `host_types.go`, `host_store.go`, `llm_safety.go`, `host_store_test.go`, `llm_safety_test.go`; Modify `app.go`, `store.go`, `llm.go`（均在 `backend/internal/app/`）。

**Interfaces:** 产出上述 HostInput/HostTopic/HostDraft/SafetyDecision 与 `CheckGeneratedSpeech`；`LoadHostContext(avatarID,sessionID string) (HostInput,error)`；`RecordPlayed(sessionID string,result TaskResult) error`。具体 SQL 表：host_topics、host_runs、host_turns；新表含 schema_version 或通过现有 additive migration 管理，不清空旧 objects/events。

- [ ] 写失败测试 `TestHostConfigMigration`：旧人物默认 fixed，已有原文不变；兴趣数量/长度非法返回400；旧数据无损。`TestTrustBoundary`：昵称伪装 system、评论含消息 JSON、带 URL/编码载荷不能改变 messages 角色或触发联网；注入输入不进入可信配置。
- [ ] 在 `backend/` 跑 `go test ./internal/app -run 'TestHostConfigMigration|TestTrustBoundary' -v`，先确认预期功能缺失失败。
- [ ] 实现类型、增量迁移、显式可信 system 消息＋不可信 JSON 数据消息、长度限制及无 tools 的请求构造；输出只解析预定结构，拒绝多余操作字段。`CheckGeneratedSpeech` 的审核调用使用独立只读提示及严格枚举，超时/解析失败返回不允许。
- [ ] 增加 `TestGeneratedSpeechFailClosed`：假 LLM 输出变更配置、回显提示词/密钥探测、审核超时/无效 JSON 均不得进入 Worker；正常短段可通过。测试使用本地假 LLM，不把真实密钥送入模型。
- [ ] 重跑以上测试，通过后提交 `feat: add companion content storage and trust boundaries`。

## Task 2: Worker 播放终态与 Go 幂等记忆

**Files:** Modify `worker/session.py`, `worker/app.py`; Create `worker/tests/test_task_results.py`, `backend/internal/app/host_results_test.go`; Modify `host_store.go`。

**Interfaces:** 产出上述 task-results REST 与 `RecordPlayed`，提供给 Task 4/5；seq 在会话内单调，停止后旧 session 路由拒绝访问。仅记录成功的原任务最后一段，不把分段误判为整稿完成。

- [ ] 写 `test_result_only_after_final_segment`、`test_failed_or_cancelled_not_completed`、`test_results_bounded_and_session_scoped`：分段只最终生成 completed；失败不为 completed；最多256条且 after 缺口 truncated=true；旧会话不可读。
- [ ] 运行 `python -m unittest worker.tests.test_task_results -v`，确认失败。
- [ ] 在 session 锁内写入有界终态环，HTTP 仅认证读取；Go 按 taskId 幂等存储，成功回执才更新话题角度/已播记录，failed/unknown 仅诊断。手动稿/固定稿照原文处理，人工稿不进入自动主题跨场去重。
- [ ] 写 `TestPlayedReceiptIdempotent`、`TestMissingReceiptDoesNotAdvanceTopic`：重放回执无重复记忆，丢回执或部分输出不推进角度；Go 重启可从数据库恢复游标并识别缺口。
- [ ] 跑 Python 新测试及 Go 对应测试，通过后提交 `feat: report bounded task playback outcomes`。

## Task 3: 自动主题储备与短段生成

**Files:** Create `host_generation.go`, `host_generation_test.go`, `host_api.go`; Modify `app.go`, `host_store.go`。

**Interfaces:** 实现 `BuildTopics`、`BuildHostDraft`；提供主题 API；主题补充与短段生成调用 Task 1 的校验入口。所有 LLM 网络请求在全局 a.mu 之外，写入前重验管理员修订/人物/会话身份。

- [ ] 写 `TestTopicReserveRefill`：12主题、<4补充、最多36、同人物生成单飞、取消后无落库；`TestHostDraftBoundsAndDedup`：40～120字符、1～3句、重复和非法 JSON 不入池，近似阈值0.75；`TestRecentTopicsExpire`：7天/200主题/100段界限及清理无跨人物误删。
- [ ] 跑 `go test ./internal/app -run 'TestTopicReserveRefill|TestHostDraftBoundsAndDedup|TestRecentTopicsExpire' -v`，确认失败。
- [ ] 实现生成、规范化/近似去重、主题和角度有界结构及管理员编辑 API；库存不足可按当前主题其他未讲角度继续，不能无限重讲已讲角度。生成失败按5/15/30/60秒退避，不能每秒重试。
- [ ] 增加 `TestUnverifiedFactsNotBroadcast`：自动素材中的虚假新闻/具体天气/无来源数据经审核拒绝，模型建议访问链接不会触发网络请求；模型生成素材仍按不可信数据使用。
- [ ] 测试通过后提交 `feat: generate reusable companion topic reserves`。

## Task 4: 连贯主持调度、留白与生命周期

**Files:** Create `host_director.go`, `host_director_test.go`; Modify `interaction.go`, `live.go`, `app.go`。

**Interfaces:** `CompanionTick(ctx context.Context,sessionID string) error`；`InvalidateHostDraft(sessionID string,reason string)`；`CompanionStatus(sessionID string) HostStatus`。复用 Task 2 回执和 Task 3 生成；保留 fixed 原调度分支。

- [ ] 写 `TestCompanionFlow`：空闲生成/校验/入队/完成/推进下一角度；没有回执不推进。`TestHostPauseTiming`：注入时钟/随机源验证2～5秒、5～10秒和长安静10～20秒；不忙等、不每tick刷新截止时间。
- [ ] 跑上述 Go 测试确认失败。
- [ ] 实现本场 revision 与取消上下文、单生成/单缓存、固定模式回退及状态；提交前再核对 session/status/hostEnabled/revision/队列。暂停废弃未提交草稿，已进入Worker的小段允许自然播完；停播不能被 LLM 请求阻塞。
- [ ] 增加 `TestLateGenerationCannotCrossSession`、`TestSlowLLMDoesNotBlockStop`、`TestManualStopAfterHour`、`TestRestartDoesNotReplayUnknown`：暂停/停止/换会话时旧结果丢弃；65分钟仍可主持且没有自动告别；重启不重播未确认段；错误期间保持idle并呈现 degraded。
- [ ] 测试通过后提交 `feat: schedule coherent companion speech with natural pauses`。

## Task 5: 互动接话、回到话题与攻击回归

**Files:** Modify `interaction.go`, `llm.go`, `host_director.go`; Create `companion_interaction_test.go`, `prompt_attack_test.go`。

**Interfaces:** `BuildInteractionReply(ctx context.Context,input HostInput,event ViewerTurn) (ReplyDecision,error)`；`ReplyDecision{action:speak|skip,text}`。观众无稳定身份契约，不构建跨场个人档案。`OnViewerTurn(sessionID string,event ViewerTurn)` 仅使未提交自主草稿失效，不写可信人设。

- [ ] 写 `TestInteractionContinuesTopic`：评论接话→连续追问→安静后自然延续，输入含已播上下文及可信服务器时间；固定评论稿仍按原文且不因新模式被重写。`TestCommentStormBounded`：最多20条短期上下文；等待超过30秒的普通评论不再生成答复；现有插件 processed/retry 与事件去重契约不破坏。
- [ ] 跑新测试确认失败。
- [ ] 实现事件入站限制、每场最多1个互动LLM在途（后台待处理FIFO最多20条，满则明确skip并完成事件，不制造无限retry），优先真实互动；昵称只作称呼数据，不能作为系统角色。事件超时/skip记为已处理但不记为已播，失败重试仍沿用原稳定 taskId。
- [ ] 建立表驱动攻击集 `TestPromptAttackCorpus`：忽略前令、伪系统/管理员、昵称攻击、多语种、编码要求、引用嵌套、多轮污染、要求泄露提示词/配置、生成素材注入。断言角色/权限不变、未授权输出被阻断、攻击原文不进后续主题/可信记忆；不要求某一句固定拒绝文案。
- [ ] 加 `TestTimeCannotBeOverriddenByComment` 和 `TestFixedScriptsRemainVerbatim`，验证用户声称的时间不能覆盖服务器时间、现有欢迎礼物/人工稿保持原文；测试通过后提交 `feat: integrate safe contextual viewer conversations`。

## Task 6: 运营者设置、主题管理与主持状态

**Files:** Create `frontend/src/components/HostContentManager.tsx`, `HostStatus.tsx`, `HostContentManager.test.tsx`; Modify `frontend/src/App.tsx`, `types.ts`, `App.test.tsx`；Go `host_api.go` 增加状态路由测试。

**Interfaces:** 消费上述 avatar配置、主题和 host-status API；`HostContentManager({avatarId:string})` 管主题与兴趣；`HostStatus({sessionId:string})` 展示当前话题/准备/暂停/降级，不假报真人观众数或播放成功。

- [ ] 写测试：旧人物固定模式、选择陪伴模式、修改兴趣、主题自动补充/生成中/失败重试、禁用主题、401/409/503中文提示；切人物/切会话时晚响应不能覆盖当前状态。
- [ ] 在 `frontend/` 跑 `npm test -- --run` 确认新增功能测试失败。
- [ ] 在现有人物/话术区域增加模式和兴趣、主题列表，不重做导航；保留原文稿管理。直播区只增加当前话题和真实状态，暂停/恢复沿用原控件，错误不暴露提示词或内部token。
- [ ] 测试通过后 `npm run build`；浏览器检查1440桌面和390手机、空主题/降级/长标题，保存关键截图，再提交 `feat: manage companion topics and hosting status`。

## Task 7: 契约、长时模拟与真实听感验收

**Files:** Modify `docs/contracts.md`, `README.md`; Create `backend/internal/app/companion_soak_test.go`, `docs/verification/companion-host/README.md`；真实运行证据只提交脱敏文本，不提交评论、密钥或私人素材。

**Interfaces:** 汇总前六项，更新请求/响应示例、状态含义、错误恢复、fixed/companion迁移行为、无审核直接使用与安全限制。不修改模型端NDJSON/时钟协议。

- [ ] 先写 `TestCompanionSoak90Minutes`：假时钟90分钟、多个主题、无人互动/互动突发/暂停恢复/生成失败/回执缺口，断言不自动结束、不短期重复、各缓存及LLM并发有界、重试有退避、只有成功输出推进记忆；纯模拟不能拿来宣称真实播放90分钟。
- [ ] 跑失败测试，再补齐发现的集成缺口，更新契约和操作说明。
- [ ] 完整验证：`cd backend && go test -race ./... && go vet ./...`；根目录 `python -m unittest discover -s worker/tests -v`；`cd frontend && npm test -- --run && npm run build`；根目录 `bash scripts/check-compose.sh`。所有失败修正后再进入真实验收。
- [ ] 用户GPU和idle就绪后另行实测：先听10分钟无人互动，再用本地模拟事件做10分钟接话/插播，最后真实连续运行至少60分钟。记录停顿、首句等待、接话延迟、复读次数、人物一致性、音画衔接和失败恢复；不自动向平台发评论，不启用外部转推。
- [ ] 如提示注入导致不当口播，记录攻击类别/是否进入记忆和可重复fixture，不保存真实敏感数据；修复后重跑攻击集。审核拒绝过多也要调规则，不能以长时间沉默当作功能通过。
- [ ] 记录实测通过项与未验证项，提交 `test: verify long-running companion host behavior`。

## 实施顺序与审阅

按1→2→3→4→5→6→7实施；每项先失败测试再最小实现，单项提交，不并发修改共享调度文件。主题储备、安全、回执是同一主持闭环的依赖，不拆成互不兼容的独立实现。

代码审阅重点：LLM请求不持有全局锁；stop/pause的revision栅栏；completed回执语义；来自模型或观众的数据不升级权限；固定稿与插件ack兼容。听感不通过时优先调短段长度、留白和主题素材，不先改模型或添加随机动作/语气词。

## 自审结果

- 已覆盖手动停播、约一小时储备但可持续、直接使用、互动与陪伴、服务器时间、固定原文、防注入与记忆污染。
- 全部后续类型/接口在本计划定义；测试覆盖 Review Focus 五项。
- 现有「生成无记忆、固定5秒轮播、Worker仅lastTaskCompletedAt」均有对应替换/增量任务。
- 未包含：网页检索/热点新闻、观众长期画像、自动开停播、动作生成、换形象、新模型部署、GPU配置变更。
- 人物兴趣、主题数量、段长、留白、去重阈值与审核策略均为本计划建议值，等待用户审阅；实际自然度不能仅凭文本或CPU测试确认。
