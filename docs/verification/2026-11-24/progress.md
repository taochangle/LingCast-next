# SDD ledger — plan: /Users/taoxin/LingCast-next/docs/superpowers/plans/2026-11-24-lingcast-next.md
Base e5c8967. User selected subagent execution. Independent new repo feat/initial-version, old project untouched.
Ruling: execute directly from user's execution request — approval already supplied by session — reversible scope only.
Ruling: independent repository is isolation; no linked worktree needed — avoids touching old branch — cost if wrong is directory move.
Ruling: frozen contracts and disjoint directory ownership permit Task1/2/3 concurrent implementation — user requested division — interface defects require Task5 integration.
Preflight:
| Tasks | Producer/consumer | Result |
|1 self|Go API versus httptest|consistent; actual GPU is not test claim|
|2 self|React UI versus API contracts|consistent|
|3 self|Worker versus resident adapters|generic look preparation required, not old reserved IDs|
|4 self|plugin versus collector|fixtures not live DOM acceptance|
|5 self|Compose versus standalone AI|no app depends_on in AI file|
|1/2|camelCase REST|docs/contracts.md frozen|
|1/3|Worker REST, media URLs|explicit tokens and routable PUBLIC_BASE_URL|
|1/4|session token, platform|exact collector contract|
|1/5,2/5,3/5,4/5|deploy/build/test|integration gate after producer reviews|
Task1/2/3 base e5c8967; pending.
Task3 discovery: pinned MuseTalk upstream requires Python3.10 CUDA11.8 torch2.0.1; pinned CosyVoice requires torch2.3.1 CUDA12.1. Model images separately versioned, Worker stays Python3.12. Confirmed upstream refs provided by implementer; Task5 must consume docs/model-services.md.
Task2 review round1: Important stale manager responses and live polling races; Minor token trimming. Resumed frontend for focused fixes/tests.
Task1 review: dead Worker restart stale slot Critical; absolute preview URL, unchecked persistence, dedup history truncation, uploaded-media protocol exposure Important; exact enum validation Minor. Fix dispatch pending reviewer completion.
Ruling: /api/live.session remains active session or null per frozen contract; preserve latest terminal status as additive lastSession field — meets plan's last-session visibility without confusing start/stop UI — cost is optional UI enhancement.
Integration evidence: local Go actual HTTP health/list/settings/create success, compiled SPA served; synthetic PNG and silent25fps MP4 upload/decode201, signed media GET200. No inference/relay performed.
Task2 fix round1 8a45e2d..4bc1095: original stale-response/token findings addressed; new poll starvation Important found.
Task2 fix round2 4bc1095..b6a4992: starvation addressed,14tests/build GREEN, scoped review clean.
Task2: complete (commits c4de3fd..b6a4992 frontend-only, review clean).
Task1 fix round1 bbfa026..f3cc7ce: awaiting scoped review, race/vet GREEN11newtests.
Task4 base b6a4992, agent collector owns extensions/chrome.
Task3 actual FFmpeg CPU smoke H264/AAC FLV generated, no GPU evidence; report pending.
Task1 fix round1 review: original findings addressed; new Important cappedOutput promoted bytes.Buffer.ReadFrom bypasses Write cap. Round2 required named field + subprocess2MiB regression.
Task3 base c4de3fd head1ea53d6, report CPU17tests+real FLV; review pending.
Task2 browser integration: connected real Go18080, Studio and saved avatar persona/voice editable screens rendered correctly. No synthetic inference claim.
Task1 fix round2 f3cc7ce..aaecae5: ReadFrom bypass addressed, scoped spec passes. Minor deferred: bytes.Buffer.Cap<=2MiB assertion may be allocator-growth flaky; byte-length/error contract passes. Carry to Task5/final fix.
Task1: complete (commits c4de3fd..aaecae5 backend-only, original findings reviewed addressed,1minor deferred).
Task3 review round1: Important capture/fd race, canceled final fetch still prepares, blocked render retirement/restart; resumed worker for CPU regressions.
Task5 deployment started against frozen HTTP contracts and stable model env; final integration gated on Worker review cleanup. Own deploy/compose/scripts/docs only.
Ruling: Worker segments accepted long text into <=240 Unicode-character resident calls, preserving exact order/content and cancellation — model services cap per inference request while Go accepts longer fixed scripts — cost if wrong is prosody/segment-boundary refinement; never truncate silently.
Task5 found and coordinated long-text mismatch before GPU acceptance. Compose PUBLIC_BASE_URL must be routable for Worker AND browser media; preview is independent relative URL.
Task3 fix round1 1ea53d6..909a3f1:25CPUtests/real FLV GREEN; scoped review pending.
Task4 review: per-count ACK loses partial failures, transient sendMessage rejection abandons queues, DOM attribute/virtualization/root scoping gaps. Collector resumed fix1.
Ruling: additive processedIds/retryIds collector acknowledgement with pending/done persisted identity — count alone cannot reliably retry partial work — cost if wrong is backwards compatibility for pre-release clients; v1 new plugin requires explicit arrays.
Task1 integration fix dispatched for per-ID state/ack and deferred Cap assertion; preserves accepted count and frozen existing fields.
Task5 preview integration: low-bitrate FLV response.read(65536) can delay output, Go io.Copy needs flush. Backend owner adding flush/test; deployment owner explicitly allowed ONLY worker/app.py read1 integration change/test. No concurrent Worker owner edits now. Task3 scoped lifecycle diff remains1ea53d6..909a3f1.
Task1 collector+preview integration aaecae5..f4fbf8b: race/vet GREEN, new per-ID pending/done and flush tests; deferred Cap assertion fixed. Review pending.
Task4 fix1 f132386..8f81986:10Node GREEN, precise ACK/channel recovery/root+attribute+virtualized DOM; review pending.
Task5 actual CPU LinuxARM Compose smoke: Go/UI/Worker/SRS authenticated FLV; synthetic inference only; long Chinese segmentation/stop/stale reject GREEN. Target NVIDIA/WSL2/live platform still unverified.
Task3 fix1 review originals addressed but2new Important: writer failure joins renderer that only sees cancel not closed, causing hang; start publication+preparing cleanup non-atomic stop gap. Round2 dispatched for focused interleaving tests.
Task1 integration review f4fbf8b: per-ID ack and preview flush PASS, Cap assertion minor addressed. Task1 complete.
Task4 fix2 8f81986..677c0b9: ancestor attributes addressed,10tests GREEN, scoped spec+quality approved. Task4 complete.
Task3 fix2 909a3f1..164143b: output cancel-before-join and atomic publication,27testsGREEN, scoped review pending.
Task3 fix2 scoped originals addressed; new Important first recorded inference error can be overwritten by writer teardown RuntimeError because _run status guard accepts error. Round3 smallest error-preservation fix+interleaving regression.
Task3 fix3 164143b..7af15fe:primary error preservation ADDRESSED, scoped spec+quality clean. Task3 complete.
Root final full verification7af15fe: Go race+vet PASS, React14+build PASS, plugin10 PASS, Python28 including real local FFmpeg PASS.
Task5 review Important: Whisper20231117 sdist isolated build can select setuptools82 lacking pkg_resources; outer pin ineffective. Deploy owner fix1 requested preinstall Whisper no-build-isolation while preserving TensorRT isolation. CPU topology/spec passes.
Task5 fix1 7af15fe..a6eac78: Whisper preinstall no-deps/no-build-isolation only; outer tooling pinned, TensorRT isolation retained. Actual Python3.10 host and Linuxamd64 wheel RED pkg_resources missing -> GREEN wheel build/install. GPU image still unverified. Scoped review pending.
Root7af15fe Compose config5+smoke GREEN again; temp resources and root Go18080 process cleaned.
Task5 fix1 scoped PASS; Task5 complete.
Final wholebranch a6eac78:3Important (long task monopolizes short-boundary priority, relay child failure invisible to admin, all historical looks resident/eager startup). NoCritical. ONE final fix wave dispatched with complete list, then scoped re-review.
Final declined-to-judge accepted as explicit acceptance gaps: realGPU/images, likeness/lipsync quality, throughputVRAM, longrun, WSL2/LinuxNVIDIA, liveDOM, crosshostTLS/platformRTMP, chosen model artifact redistribution licenses. CPU evidence not production readiness.

Final fixwave a6eac78..5b3fcf8: all3Important addressed, scoped finalreview PASS/no newImportant. Root verification5b3fcf8:Go race/vet,UI15/build,Python33/realFFmpeg,Compose5/fullsmoke,previewstream unit PASS;plugin10 unchanged priorPASS. All5tasks complete. Newrepo has no upstream/basebranch; retain featurebranch/local only. Evidence archived under docs/verification/2026-11-24 before scratch removal.
