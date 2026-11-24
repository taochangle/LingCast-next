ARG DOCKER_IMAGE_PREFIX=docker.1ms.run/
FROM ${DOCKER_IMAGE_PREFIX}nvidia/cuda:11.8.0-cudnn8-runtime-ubuntu22.04@sha256:85fb7ac694079fff1061a0140fd5b5a641997880e12112d92589c3bbb1e8b7ca
ARG APT_MIRROR=https://mirrors.tuna.tsinghua.edu.cn
COPY deploy/configure-apt.sh /tmp/configure-apt.sh
RUN sh /tmp/configure-apt.sh "${APT_MIRROR}" \
    && apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    python3.10 python3.10-venv git ffmpeg libsndfile1 libgl1 libglib2.0-0 ca-certificates \
    && rm -rf /var/lib/apt/lists/*
RUN python3.10 -m venv /opt/venv
ENV PATH="/opt/venv/bin:${PATH}" PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
RUN git init /opt/MuseTalk && git -C /opt/MuseTalk remote add origin https://github.com/TMElyralab/MuseTalk.git \
    && git -C /opt/MuseTalk fetch --depth=1 origin 0a89dec45a0192b824e3cf4daf96c239440c5ed8 \
    && git -C /opt/MuseTalk checkout --detach FETCH_HEAD
COPY services/lipsync/*requirements.txt /tmp/lipsync/
ARG PIP_INDEX_URL=https://pypi.tuna.tsinghua.edu.cn/simple
ARG PYTORCH_CU118_INDEX_URL=https://mirror.sjtu.edu.cn/pytorch-wheels/cu118
ARG MMCV_WHEEL_INDEX_URL=https://download.openmmlab.com/mmcv/dist/cu118/torch2.0.0/index.html
ENV PIP_INDEX_URL=${PIP_INDEX_URL}
RUN pip install --no-cache-dir torch==2.0.1+cu118 torchvision==0.15.2+cu118 torchaudio==2.0.2+cu118 \
    --index-url "${PYTORCH_CU118_INDEX_URL}" \
    && pip install --no-cache-dir --only-binary=mmcv mmcv==2.0.1 \
    --no-index --no-deps --find-links "${MMCV_WHEEL_INDEX_URL}" \
    && pip install --no-cache-dir -r /tmp/lipsync/requirements.txt \
    && pip check
WORKDIR /app
COPY services/common.py ./services/common.py
COPY services/lipsync ./services/lipsync
CMD ["python", "-m", "services.lipsync.app"]
