FROM nvidia/cuda:12.1.1-cudnn8-devel-ubuntu22.04@sha256:21196d81f56b48dbee70494d5f10322e1a77cc47ffe202a3bf68eab81533c20f
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    python3.10 python3.10-dev python3.10-venv git ffmpeg libsndfile1 libgl1 libglib2.0-0 \
    build-essential ca-certificates && rm -rf /var/lib/apt/lists/*
RUN python3.10 -m venv /opt/venv
ENV PATH="/opt/venv/bin:${PATH}" PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
RUN git init /opt/CosyVoice && git -C /opt/CosyVoice remote add origin https://github.com/FunAudioLLM/CosyVoice.git \
    && git -C /opt/CosyVoice fetch --depth=1 origin 074ca6dc9e80a2f424f1f74b48bdd7d3fea531cc \
    && git -C /opt/CosyVoice checkout --detach FETCH_HEAD \
    && git -C /opt/CosyVoice submodule update --init --recursive --depth=1 \
    && test "$(git -C /opt/CosyVoice/third_party/Matcha-TTS rev-parse HEAD)" = dd9105b34bf2be2230f4aa1e4769fb586a3c824e
COPY services/tts/*requirements.txt /tmp/tts/
RUN pip install --no-cache-dir torch==2.3.1+cu121 torchaudio==2.3.1+cu121 --index-url https://download.pytorch.org/whl/cu121 \
    && pip install --no-cache-dir setuptools==75.8.2 wheel==0.45.1 packaging==24.2 cython==0.29.37 numpy==1.26.4 \
    && DS_BUILD_OPS=0 pip install --no-cache-dir --no-build-isolation deepspeed==0.15.1 pyworld==0.3.4 \
    && pip install --no-cache-dir -r /tmp/tts/requirements.txt \
    && pip check
WORKDIR /app
COPY services/common.py ./services/common.py
COPY services/tts ./services/tts
CMD ["python", "-m", "services.tts.app"]
