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

# Verify empty/unset prefixes select Docker Hub rather than restoring the mirror.
python3 - <<'PY'
import json
import os
from pathlib import Path
import subprocess
import tempfile

source = Path('.env.example').read_text()
settings = ('DOCKER_IMAGE_PREFIX', 'PIP_INDEX_URL', 'PYTORCH_CU121_INDEX_URL',
            'PYTORCH_CU118_INDEX_URL', 'MMCV_WHEEL_INDEX_URL', 'APT_MIRROR',
            'APK_MIRROR', 'NPM_REGISTRY', 'GOPROXY')
with tempfile.TemporaryDirectory() as directory:
    without_prefix = Path(directory) / 'official.env'
    without_prefix.write_text('\n'.join(line for line in source.splitlines()
                                        if not line.startswith('DOCKER_IMAGE_PREFIX=')))
    for label, prefix in [('template', None), ('empty', ''), ('unset', None),
                          ('custom', 'registry.example.invalid/')]:
        environment = dict(os.environ)
        for name in settings:
            environment.pop(name, None)
        if prefix is not None:
            environment['DOCKER_IMAGE_PREFIX'] = prefix
        if label == 'custom':
            environment.update({
                'PIP_INDEX_URL': 'https://pip.example.invalid/simple',
                'PYTORCH_CU121_INDEX_URL': 'https://wheels.example.invalid/cu121',
                'PYTORCH_CU118_INDEX_URL': 'https://wheels.example.invalid/cu118',
                'MMCV_WHEEL_INDEX_URL': 'https://wheels.example.invalid/mmcv/index.html',
                'APT_MIRROR': 'https://apt.example.invalid',
                'APK_MIRROR': 'https://apk.example.invalid/alpine',
                'NPM_REGISTRY': 'https://npm.example.invalid',
                'GOPROXY': 'https://go.example.invalid',
            })
        env_file = str(without_prefix) if label == 'unset' else '.env.example'
        data = json.loads(subprocess.check_output([
            'docker', 'compose', '--env-file', env_file, '-f', 'compose.yaml',
            '-f', 'compose.ai.yaml', '--profile', 'models', 'config', '--format', 'json',
        ], env=environment, text=True))
        expected = 'docker.1ms.run/' if label == 'template' else (prefix or '')
        services = data['services']
        assert services['srs']['image'].startswith(expected + 'ossrs/srs:5@sha256:')
        for service in ('app', 'worker', 'tts', 'lipsync'):
            assert services[service]['build']['args']['DOCKER_IMAGE_PREFIX'] == expected
        for service in ('worker', 'tts', 'lipsync'):
            assert services[service]['build']['args']['PIP_INDEX_URL'] == environment.get(
                'PIP_INDEX_URL', 'https://pypi.tuna.tsinghua.edu.cn/simple')
            assert services[service]['build']['args']['APT_MIRROR'] == environment.get(
                'APT_MIRROR', 'https://mirrors.tuna.tsinghua.edu.cn')
        for service, name, default in (
            ('tts', 'PYTORCH_CU121_INDEX_URL', 'https://mirror.sjtu.edu.cn/pytorch-wheels/cu121'),
            ('lipsync', 'PYTORCH_CU118_INDEX_URL', 'https://mirror.sjtu.edu.cn/pytorch-wheels/cu118'),
            ('lipsync', 'MMCV_WHEEL_INDEX_URL', 'https://download.openmmlab.com/mmcv/dist/cu118/torch2.0.0/index.html'),
            ('app', 'APK_MIRROR', 'https://mirrors.tuna.tsinghua.edu.cn/alpine'),
            ('app', 'NPM_REGISTRY', 'https://registry.npmmirror.com'),
            ('app', 'GOPROXY', 'https://goproxy.cn'),
        ):
            assert services[service]['build']['args'][name] == environment.get(name, default)
        print(f'Mirror settings ({label}): valid')
PY
