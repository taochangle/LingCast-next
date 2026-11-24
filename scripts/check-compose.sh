#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

export COMPOSE_PROFILES=''
compose=(docker compose --env-file .env.example)
check() {
    label=$1
    shift
    "${compose[@]}" "$@" config -q
    printf '%s: config valid\n' "$label"
}

WORKER_URL=https://worker.example.invalid check "Business only" -f compose.yaml
check "Standalone AI with resident models" -f compose.ai.yaml --profile models
check "Combined with resident models" -f compose.yaml -f compose.ai.yaml --profile models
TTS_URL=https://tts.example.invalid LIPSYNC_URL=https://lipsync.example.invalid \
    check "Combined with remote models" -f compose.yaml -f compose.ai.yaml
TTS_URL=https://tts.example.invalid LIPSYNC_URL=https://lipsync.example.invalid \
    check "Standalone AI with remote models" -f compose.ai.yaml
