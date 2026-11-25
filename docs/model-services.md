# Resident model services and Worker

These services execute real resident inference; failed imports, missing references/weights and unavailable NVIDIA CUDA keep `/readyz` false. CPU tests use injected adapters and prove neither GPU inference nor generated media quality. No model weights, reference recordings or private configurations are included. No downloads occur at service startup.

## Processes and environment

Set `TTS_GPU_ID` and `LIPSYNC_GPU_ID` in `.env` to the host NVIDIA GPU index or UUID for each service (both default to `0`). Compose exposes only the selected device to each model container, where it is addressed as `cuda:0`. For example, set both to `0` to share host GPU 0, or use `0` and `1` to place the services on separate GPUs. Worker and Go do not request a GPU.

Run from repository root (copy `worker/` and `services/` into `/app`, `WORKDIR /app`):

| Process | Command | Required settings | Default port |
|---|---|---|---|
| Worker | `python -m worker.app` | `WORKER_TOKEN`, `TTS_URL`, `TTS_TOKEN`, `LIPSYNC_URL`, `LIPSYNC_TOKEN` | `WORKER_PORT=8090` |
| CosyVoice | `python -m services.tts.app` | `TTS_TOKEN` | `TTS_PORT=8091` |
| MuseTalk | `python -m services.lipsync.app` | `LIPSYNC_TOKEN`, `TTS_URL`, `TTS_TOKEN` | `LIPSYNC_PORT=8092` |

All GETs and POSTs, including readiness, require `Authorization: Bearer <service token>`. Tokens must be nonempty. Bind GPU services behind authenticated TLS when crossing hosts; do not expose raw SRS ingest or HTTP globally. Model readiness is not GPU acceptance. Worker readiness requires both resident services and reports unhealthy when an active FFmpeg exits.

Worker installs `worker/requirements.txt` plus FFmpeg/ffprobe; Python3.12 on Linux/WSL2. `WORKER_CACHE_DIR=/cache/worker` holds transient downloaded session files and is removed on stop/failure. `SRS_RTMP_URL=rtmp://srs:1935/live/{sessionId}`, `SRS_HTTP_URL=http://srs:8080`. Output uses 25fps BGR24 + 16kHz mono s16le, paced in 40ms slices, H.264/AAC FLV. Preview `/preview/{sessionId}.flv` requires Worker auth; Go proxies for the browser. Relay only starts on explicit PUT, has separate process cleanup, and never starts based on selected platform.

## Locked upstream source and build inputs

Build sources are configurable in `.env`: `DOCKER_IMAGE_PREFIX=docker.1ms.run/` by default in the template; empty or omitted means official Docker Hub when using Compose. `PIP_INDEX_URL` defaults to TUNA, and `PYTORCH_CU121_INDEX_URL` / `PYTORCH_CU118_INDEX_URL` default to SJTUG's matching CUDA indexes. `MMCV_WHEEL_INDEX_URL` selects the exact cu118/torch2.0 wheel directory. These change build-time download locations, not pinned package/source versions or model mounts. No generic mirror silently replaces a CUDA-specific index.

Already copied weights require no download flag: set `COSYVOICE_WEIGHTS_DIR` and `MUSETALK_WEIGHTS_DIR` to the full local trees and skip manual weight-download commands. Startup remains offline, absent files fail readiness, and Docker builds exclude host `models/`. Fixed source bundles are shipped under `vendor/sources/`; Docker verifies and extracts them without Git or network source checkout. Python dependencies still install from configured package repositories. TTS installs the runtime subset in `services/tts/requirements.txt`: training-only DeepSpeed and unused TensorRT acceleration are excluded. The original upstream list stays unchanged for provenance. CUDA12 ONNX Runtime 1.18.0 uses its dedicated Microsoft package index; the ordinary PyPI wheel of that version targets a different CUDA stack. This is a Python binary dependency, not another weight download.

Sources were verified against actual upstream files, not invented revisions:

* [CosyVoice requirements at `074ca6dc9e80a2f424f1f74b48bdd7d3fea531cc`](https://github.com/FunAudioLLM/CosyVoice/blob/074ca6dc9e80a2f424f1f74b48bdd7d3fea531cc/requirements.txt), vendored unchanged as `services/tts/upstream-requirements.txt`.
* Matcha-TTS submodule `dd9105b34bf2be2230f4aa1e4769fb586a3c824e`, verified via the pinned parent's GitHub contents API gitlink; do not clone branch main.
* [MuseTalk requirements at `0a89dec45a0192b824e3cf4daf96c239440c5ed8`](https://github.com/TMElyralab/MuseTalk/blob/0a89dec45a0192b824e3cf4daf96c239440c5ed8/requirements.txt), vendored unchanged as `services/lipsync/upstream-requirements.txt`. Additional exact versions for upstream's unpinned runtime extras are in `services/lipsync/requirements.txt`.

Model images need **Python3.10**, independently of Worker3.12. MuseTalk's pinned README recommends torch2.0.1/torchvision0.15.2/torchaudio2.0.2 CUDA11.8 and mmcv2.0.1/mmdet3.1.0/mmpose1.1.0. Install torch wheels from the configured cu118 index (SJTUG by default; official fallback `https://download.pytorch.org/whl/cu118`) and mmcv from matching torch2.0/cu118 wheel index; a CPU mmcv wheel cannot serve this model. Its numpy1.23.5 and TensorFlow2.12 pin rule out blindly using Worker Python3.12. CosyVoice independently pins torch2.3.1/torchaudio2.3.1 and CUDA12.1 index, with optional training/acceleration dependencies excluded from our runtime subset. Its requirements need build tools/CUDA toolchain; do not install both sets into one environment. Preserve source submodules recursively at their pinned gitlinks. Linux x86_64 image builds and initial real CUDA checks are recorded under `docs/verification/docker-build/`; complete rendered-video acceptance is separate.

Source acquisition is a maintainer operation, outside the target Docker build.
The repository already includes the bundles; an operator does not need to clone
the upstream repositories. The pinned codeload sources are:

```sh
https://codeload.github.com/FunAudioLLM/CosyVoice/tar.gz/074ca6dc9e80a2f424f1f74b48bdd7d3fea531cc
https://codeload.github.com/shivammehta25/Matcha-TTS/tar.gz/dd9105b34bf2be2230f4aa1e4769fb586a3c824e
https://codeload.github.com/TMElyralab/MuseTalk/tar.gz/0a89dec45a0192b824e3cf4daf96c239440c5ed8
```

Bundle contents, omitted demo material, original and repacked checksums are
documented in `vendor/sources/`. Source revisions lock source and direct
requirements; they are not a complete transitive Python package lock or
model-weight checksum lock. A production image must be built and recorded on
target NVIDIA hardware before release.

## External assets and mounts

CosyVoice: `COSYVOICE_SOURCE=/opt/CosyVoice`; mount the complete operator-acquired **Fun-CosyVoice3-0.5B-2512** snapshot at `COSYVOICE_MODEL=/models/CosyVoice3` read-only. Required configs/tokenizers, speech tokenizer, flow, LLM, HiFT and campplus weights must come from the same official snapshot; a bare `.pt` file is insufficient. Record the resolved model repository revision and SHA256 inventory in the operator deployment record before enabling models.

MuseTalk: `MUSETALK_SOURCE=/opt/MuseTalk`, `MUSETALK_CACHE=/cache/musetalk` writable persistent volume. Runtime changes cwd to that cache because upstream uses relative `models/` and `results/` paths. Mount the full weight tree read-only at **`/cache/musetalk/models`**. Required tree matches upstream `download_weights.sh`: `musetalkV15/{unet.pth,musetalk.json}`, `sd-vae` config/weights, `whisper` config/preprocessor/tokenizer/weights, `dwpose/dw-ll_ucoco_384.pth`, `s3fd/s3fd.pth` (the pinned upstream S3FD detector checkpoint), `face-parse-bisent` with `79999_iter.pth` and `resnet18-5c106cde.pth`. The adapter passes this local S3FD path explicitly and refuses to trigger upstream's automatic download. Hugging Face offline mode is enabled for both residents. Follow pinned upstream asset instructions explicitly; do not run unreviewed moving download scripts automatically. Record resolved snapshots/checksums and licenses. Cache `results/v15/avatars/` (profile name `lookId_SHA256`) contains prepared latents/masks generated from each submitted look and a source SHA256 marker; persist it. Never mount untrusted precomputed pickle/torch caches.

Both services use `MODEL_DATA_ROOT=/data`, `MODEL_MANIFEST=/data/manifest.json`. Use distinct per-service `/data` volumes: TTS reference data can be read-only; MuseTalk `/data/looks` and manifest writable. Manifest is operator-supplied configuration, never an arbitrary client filesystem path:

```json
{
  "profiles": {
    "your-voice-uuid": {"promptText": "真实参考音频中的原文", "promptAudio": "voices/your-reference.wav"}
  },
  "looks": {}
}
```

Reference audio paths resolve under `/data`; traversal outside is rejected. `voiceId` is arbitrary safe string/UUID; no default reserved personas. TTS loads all reference speakers on startup and rejects changed reference files until restarted, preventing stale loaded speaker identity. Authenticated `GET /voices` reports only IDs/fingerprints. Model readiness requires at least one real configured voice. Use only authorized reference audio. MuseTalk binds arbitrary `lookId` and source hash when preparing.

## Preparation and streaming protocol

Worker first downloads every signed media URL **including its query token**, with no redirects, bounded size/time and SHA256 verification; it never sends model tokens to business media URLs. Start requires selected default idle, models ready and successful face preparation. Default idle is the talking bottom plate. The Go upload pipeline drops audio tracks and converts video to H.264 MP4 at 25fps before storing it and hashes the converted bytes. Idles must decode fully, have matching even dimensions (64–4096), 25fps and no audio track; idle playback cycles in submitted order. Changing a stopped look's default material prepares a new profile keyed by look ID plus complete SHA256; render requests must match the current look/hash registration.

`POST LIPSYNC_URL/prepare?lookId=<uuid>&sha256=<lowercase sha256>` with Bearer LIPSYNC_TOKEN and raw default-idle video body (`Content-Type: video/mp4`, maximum200MiB). Body checksum is checked and video fully decoded. MuseTalk preprocesses face latents via actual upstream `Avatar(..., preparation=True)`, then atomically updates `looks` manifest. Returns `{ready:true,lookId,sha256}` only after real preparation. Repeated identical identity reuses matching cache; changed material gets a separate profile, never stale latents. Preparation can take minutes: operator may call it before live start. Worker uses a600s model preparation timeout; Go's shorter start timeout must clean up rather than regard this as success.

`POST /tts {voiceId,text}` yields NDJSON float32 little-endian `pcm` base64, `sample_rate`, immutable `voiceFingerprint`, followed by `{done:true,...}`. MuseTalk calls this TTS service internally once for the requested voice/text. Worker must not synthesize a separate audio stream.

`POST /render {lookId,voiceId,text,sha256}` yields old LingCast paired packet protocol:

* `{type:"audio",index:0,frames:N,pcm:"..."}` where PCM is exactly `N*640` mono16kHz s16 samples.
* Exactly N `{type:"frame",index:0,jpeg:"..."}` packets, then next contiguous audio index.
* Final `{type:"done"}`; model exceptions produce error events and cannot become a successful partial stream.

Each audio window includes a fingerprint over fixed source IDs, look hash, selected voice/reference fingerprint and exact text. There is no generated-media replay cache; changed weights require process restart and operator cache invalidation. The one-second window retains160ms acoustic context without repeating core audio (ported `AudioWindows`). Worker rejects bad PCM length, missing/extra frames, missing done, invalid JPEG markers, index changes and >8MiB NDJSON lines. JPEG full decode and dimensions are checked before output. Queue100, task expiry capped120s, stable kind priority manual/gift/like/comment/welcome/host; dedup retains session IDs and refuses further work after100000 tasks rather than silently forgetting identities. `/status` always includes `preparing:boolean`; after stop during preparation this stays true until the in-flight model call finishes, and new start conflicts during that interval. No late session/output install is allowed. Stop destroys queue identity and kills ffmpeg/relay; stale session routes are rejected.

## Reuse attribution and acceptance

Adapted read-only from `/Users/taoxin/LingCast/worker/`: `ai/audio_windows.py` copied into `services/lipsync/audio_windows.py`; `ai/musetalk_runtime.py` callback render/rotation and resident CosyVoice/MuseTalk clock conversion were adapted into focused generic services. `streaming/ffmpeg_pipe.py` informed audio-first per-frame interleaving and subprocess lifecycle; its Windows-native socket layer was not copied because target runtime is Linux containers on Linux/WSL2. No old private manifests, addresses, recordings or credentials were copied.

CPU suite: `python -m unittest discover -s worker/tests -v`; syntax: `python -m compileall -q worker services`. Tests use injected inference/output and local HTTP only. Real NVIDIA loading, face preparation, voice quality, continuous SRS output, remote GPU connectivity, Windows WSL2 and Linux x86_64 remain acceptance gates. Mac ARM CPU success is not evidence for those gates.
