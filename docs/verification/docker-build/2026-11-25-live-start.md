# Real live start and upload normalization — 2026-11-25

Two integration problems caused the reported generic Worker start failure:

1. Worker media URLs used the browser's public address, which timed out from the container. A new optional `WORKER_MEDIA_BASE_URL` supplies a reachable internal business address without changing browser URLs. The deployment uses `http://app:8080`; remote deployments retain the public fallback.
2. Upload accepted 24fps video while live output requires 25fps. The Go image already contained FFmpeg/ffprobe. Upload now converts silent videos to H.264 MP4, 25fps, yuv420p, pads odd dimensions to even, and recomputes the stored SHA256. Conversion is bounded by timeout, threads and output-size checks. Existing audio-bearing video remains rejected. No conversion was left in Worker.

## Validation

- Regression test first reproduced the original 24fps storage behavior; with the fix it verifies actual stored H.264/25fps bytes and their SHA256 using real FFmpeg/ffprobe. Input fixture remains unchanged.
- Go `go test -race ./...` passed after the upload change. The earlier Worker-media regression verifies that start payloads use the internal address while browser URLs and remote-deployment fallback remain intact.
- Built/deployed the Go image on the target server. Re-uploaded the existing 24fps idle through the new conversion pipeline, retained the original media object and a private backup, and updated the existing idle's media reference. The idle and default selection identities remain unchanged. Conversion mapping is in private server `data/verification/gpu-20261125/idle-conversion.json`.
- Started the actual configured avatar/look. Real NVIDIA face preparation completed, and API start returned `status: live` after about 44.96 seconds.
- Submitted a manual spoken test. Worker task results reported `completed`; status returned `live`, no error, empty queue, and speaking false after completion.
- Captured about 9.6 MB of the actual SRS FLV preview. FFprobe identified H.264 video, 768x1376, 25/1 fps and AAC audio; audio analysis found non-silent samples with peak -7.4 dB. The media artifact stays private on the server.
- Test session was left live for operator preview, with automatic hosting and platform relay disabled. No platform connection or viewer collection was started.

This verifies real face preparation, synthesis/render task completion and playable audio/video transport. Lip-sync quality and long-running reliability still require visual/listening acceptance; codecs and task completion alone do not establish those.
