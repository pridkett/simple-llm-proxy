# External Integrations

**Analysis Date:** 2026-03-25

## APIs & External Services

**LLM Providers:**
- **OpenAI** - Chat completions, embeddings via `internal/provider/openai/openai.go`
  - SDK/Client: Custom HTTP client (stdlib `net/http`)
  - Base URL: `https://api.openai.com/v1` (configurable via `api_base`)
  - Auth: API key via Bearer token in `Authorization` header
  - Config: `litellm_params.model` as `openai/gpt-4` (provider/model format)

- **Anthropic** - Chat completions (no embeddings) via `internal/provider/anthropic/anthropic.go`
  - SDK/Client: Custom HTTP client (stdlib `net/http`)
  - Base URL: `https://api.anthropic.com/v1` (configurable via `api_base`)
  - Auth: API key via `x-api-key` header
  - Version Header: `anthropic-version: 2023-06-01`
  - Config: `litellm_params.model` as `anthropic/claude-3-sonnet` format

**Cost & Model Metadata:**
- **LiteLLM GitHub** - Model pricing and context window data
  - URL: `https://raw.githubusercontent.com/BerriAI/litellm/refs/heads/main/model_prices_and_context_window.json`
  - Purpose: Fetch model specifications (input/output costs, max tokens, context windows)
  - Implementation: `internal/costmap/costmap.go` Manager downloads and caches in-memory
  - Timeout: 30 seconds (HTTP client timeout in `costmap.New()`)
  - Reloadable: Via `POST /admin/costmap/reload` endpoint, custom URL via `PUT /admin/costmap/url`

## Data Storage

**Databases:**
- **SQLite** (pure Go, modernc.org/sqlite)
  - Location: Configurable via `general_settings.database_url` in config (default: `./proxy.db`)
  - Connection: Via `database/sql` package with `modernc.org/sqlite` driver
  - ORM/client: Standard `database/sql` (no ORM)
  - Implementation: `internal/storage/sqlite/sqlite.go`
  - Migrations: Run via `Initialize(ctx)` method
  - WAL Mode: Enabled for better concurrency (`PRAGMA journal_mode=WAL`)

**Tables:**
- `usage_logs` - Request logging (request_id, model, provider, endpoint, tokens, cost, status, latency, timestamp)
- `cost_overrides` - User-defined cost mappings (model_name, cost_map_key or custom_spec JSON, updated_at)

**File Storage:**
- **SQLite database file** - Stored locally at path specified in config (default `./proxy.db`)
- No cloud storage integration

**Caching:**
- **In-memory** cost map cache in `internal/costmap/costmap.go` Manager
- Cost data cached after successful download from LiteLLM GitHub
- Cache invalidated on reload via admin API

## Authentication & Identity

**Auth Provider:**
- Custom master key authentication
  - Implementation: `internal/api/middleware/auth.go`
  - Mechanism: Bearer token validation (`Authorization: Bearer {master_key}`)
  - Master key source: `general_settings.master_key` from config (supports `os.environ/` expansion)
  - Protected routes: All `/v1/*` and `/admin/*` endpoints require master key

**Provider Credentials:**
- OpenAI API key: Stored in config via `litellm_params.api_key` per deployment
- Anthropic API key: Stored in config via `litellm_params.api_key` per deployment
- Keys support `os.environ/VAR_NAME` syntax for environment variable injection
- Keys passed at request time to provider via Authorization/x-api-key headers

## Monitoring & Observability

**Error Tracking:**
- No external error tracking service (Sentry, etc.)
- Errors logged via zerolog structured logging

**Logs:**
- **Zerolog** v1.34.0 structured JSON logging (`internal/logger/`)
- **Lumberjack** v2.2.1 log file rotation (`gopkg.in/natefinch/lumberjack.v2`)
- Log output: Console (default) or JSON file (configurable in `log_settings`)
- Log settings: Level, format, file path, rotation (max size, max age, max backups, compression)

**Request Logging:**
- Per-request structured logs via middleware (`internal/api/middleware/logging.go`)
- Request logs stored in SQLite `usage_logs` table for analytics
- Fields: request_id, model, provider, endpoint, prompt/completion tokens, cost, status code, latency

## CI/CD & Deployment

**Hosting:**
- Docker/Kubernetes-ready (self-contained binary)
- Multi-platform binary builds available (Linux amd64/arm64, macOS amd64/arm64, Windows amd64)
- Frontend built as static assets (deployable to CDN/static host)
- No pre-built container images in repo

**CI Pipeline:**
- No CI/CD configured in repo
- Manual build via `make build` or multi-platform `make build-all`

## Environment Configuration

**Required env vars (when using `os.environ/` syntax in config):**
- `OPENAI_API_KEY` - OpenAI API key for deployments using OpenAI
- `ANTHROPIC_API_KEY` - Anthropic API key for deployments using Anthropic
- `PROXY_MASTER_KEY` - Master authentication key for proxy endpoints (if using `os.environ/` expansion)

**Secrets location:**
- Not checked into git (use `.env` or shell environment)
- Config file can reference secrets via `os.environ/VAR_NAME` pattern
- Example: `api_key: os.environ/OPENAI_API_KEY`

## Webhooks & Callbacks

**Incoming:**
- No webhook support in proxy

**Outgoing:**
- No webhook calls made to external services
- All integration is request/response based (LLM provider calls, cost map downloads)

---

*Integration audit: 2026-03-25*
