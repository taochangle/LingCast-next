ARG DOCKER_IMAGE_PREFIX=docker.1ms.run/
FROM ${DOCKER_IMAGE_PREFIX}library/node:22-alpine AS ui
WORKDIR /build
COPY frontend/package*.json ./
ARG NPM_REGISTRY=https://registry.npmmirror.com
RUN npm ci --registry="${NPM_REGISTRY}"
COPY frontend/index.html frontend/tsconfig.json frontend/vite.config.ts ./
COPY frontend/src ./src
RUN npm run build

FROM ${DOCKER_IMAGE_PREFIX}library/golang:1.26-alpine AS backend
WORKDIR /build
COPY backend/go.* ./
ARG GOPROXY=https://goproxy.cn
ENV GOPROXY=${GOPROXY}
RUN go mod download
COPY backend/cmd ./cmd
COPY backend/internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/lingcast ./cmd/server

FROM ${DOCKER_IMAGE_PREFIX}library/alpine:3.22
ARG APK_MIRROR=https://mirrors.tuna.tsinghua.edu.cn/alpine
RUN if [ -n "${APK_MIRROR}" ]; then \
      sed -i "s@https\?://dl-cdn.alpinelinux.org/alpine@${APK_MIRROR%/}@g" /etc/apk/repositories; \
    fi \
    && apk add --no-cache ca-certificates ffmpeg tzdata
COPY --from=backend /out/lingcast /usr/local/bin/lingcast
COPY --from=ui /build/dist /opt/lingcast/ui
ENTRYPOINT ["lingcast"]
