# Multi-stage build: Vue frontend + pure-Go proxy in one image (issue #73).
#
# Build:  just docker-build   (docker build -t simple-llm-proxy .)
# Run:    just docker-run     (mounts config.yaml + a data dir, secrets via env)
#
# The image contains NO secrets and NO database — mount config and data at
# runtime and pass provider keys / master key as environment variables
# (config.yaml's os.environ/VAR references resolve against the container env).

# --- Stage 1: frontend build ------------------------------------------------
FROM node:22-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# --- Stage 2: backend build -------------------------------------------------
FROM golang:1.25-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Pure Go (modernc.org/sqlite) — CGO stays off for a static binary
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/proxy ./cmd/proxy

# --- Stage 3: runtime -------------------------------------------------------
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && adduser -D -H proxy
WORKDIR /app
COPY --from=backend /out/proxy /app/proxy
# Default FrontendDir fallback is ./frontend/dist relative to the workdir,
# so the SPA is served at / with no extra config.
COPY --from=frontend /src/frontend/dist /app/frontend/dist
USER proxy
EXPOSE 8080
# Mount your config at /app/config.yaml and point database_url at a mounted
# volume (e.g. /data/proxy.db) for persistence.
ENTRYPOINT ["/app/proxy"]
CMD ["-config", "/app/config.yaml"]
