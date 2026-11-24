FROM node:22-alpine AS ui
WORKDIR /build
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/index.html frontend/tsconfig.json frontend/vite.config.ts ./
COPY frontend/src ./src
RUN npm run build

FROM golang:1.26-alpine AS backend
WORKDIR /build
COPY backend/go.* ./
RUN go mod download
COPY backend/cmd ./cmd
COPY backend/internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/lingcast ./cmd/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates ffmpeg tzdata
COPY --from=backend /out/lingcast /usr/local/bin/lingcast
COPY --from=ui /build/dist /opt/lingcast/ui
ENTRYPOINT ["lingcast"]
