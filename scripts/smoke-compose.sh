#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

# Isolated project, random loopback ports, local-only endpoints and disposable data.
export SMOKE_PROJECT="lingcast-smoke-$(python3 -c 'import secrets; print(secrets.token_hex(4))')"
export APP_BIND_ADDRESS=127.0.0.1 APP_HOST_PORT=0
export WORKER_BIND_ADDRESS=127.0.0.1 WORKER_HOST_PORT=0
export PUBLIC_BASE_URL=http://app:8080 WORKER_URL=http://worker:8090
export TTS_URL=http://fixture:8091 LIPSYNC_URL=http://fixture:8092
export LLM_URL='' LLM_MODEL='' COMPOSE_PROFILES=''
export TIMEZONE=Asia/Shanghai WORKER_START_TIMEOUT=30s
for variable in ADMIN_TOKEN MEDIA_TOKEN WORKER_TOKEN TTS_TOKEN LIPSYNC_TOKEN LLM_TOKEN; do
    export "$variable=$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
done

compose=(docker compose --env-file .env.example -p "$SMOKE_PROJECT" -f compose.yaml -f compose.ai.yaml -f tests/integration/compose.smoke.yaml)
cleanup() {
    status=$?
    if (( status != 0 )); then
        "${compose[@]}" logs --tail 60 >&2 || true
    fi
    "${compose[@]}" down --volumes --remove-orphans --rmi local >/dev/null
    exit "$status"
}
trap cleanup EXIT

"${compose[@]}" build app worker
"${compose[@]}" up -d --no-build
python3 tests/integration/smoke.py \
    "$("${compose[@]}" port app 8080)" \
    "$("${compose[@]}" port worker 8090)" \
    "$("${compose[@]}" port fixture 8092)"
