ARG DOCKER_IMAGE_PREFIX=docker.1ms.run/
FROM ${DOCKER_IMAGE_PREFIX}library/python:3.12-slim-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg ca-certificates \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY worker/requirements.txt /tmp/worker-requirements.txt
ARG PIP_INDEX_URL=https://pypi.tuna.tsinghua.edu.cn/simple
ENV PIP_INDEX_URL=${PIP_INDEX_URL}
RUN pip install --no-cache-dir -r /tmp/worker-requirements.txt
COPY worker ./worker
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
CMD ["python", "-m", "worker.app"]
