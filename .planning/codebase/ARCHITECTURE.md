# Architecture

**Analysis Date:** 2026-03-25

## Pattern Overview

**Overall:** Proxy server with pluggable provider pattern, load balancing, and failure tracking. OpenAI-compatible API endpoints abstract multiple LLM backends (OpenAI, Anthropic) with transparent translation between formats.

**Key Characteristics:**
- Provider-agnostic abstraction layer (`internal/provider/`) enables adding new LLM backends
- Load balancing with multiple strategies (shuffle, round-robin) across deployment instances
- Automatic failure detection and cooldown isolation of unhealthy deployments
- Request/response translation (Anthropic → OpenAI format compatibility)
- Configurable routing with environment variable expansion in YAML config
- Structured logging with zerolog

## Layers

**Handler Layer:**
- Purpose: HTTP request processing, input validation, response formatting
- Location: `internal/api/handler/`
- Contains: HTTP handlers (chat.go, models.go, health.go, embeddings.go, admin.go, costmap.go, openapi.go) that accept HTTP requests and return responses
- Depends on: router, provider, storage, costmap, config, model types
- Used by: HTTP router (`internal/api/router.go`)

**Routing/Load Balancing Layer:**
- Purpose: Model deployment selection, failure tracking, cooldown management, retry logic
- Location: `internal/router/`
- Contains: deployment registry (`router.go`), strategy interface and implementations (shuffle.go, roundrobin.go), cooldown manager (`cooldown.go`)
- Depends on: provider interface, config
- Used by: handlers, main entry point

**Provider Layer:**
- Purpose: Encapsulate LLM provider specifics, translate requests/responses to OpenAI format
- Location: `internal/provider/`
- Contains: provider interface (provider.go), registry (registry.go), implementations (openai/, anthropic/)
- Depends on: model types
- Used by: router, handlers

**Model Types Layer:**
- Purpose: Define request/response structures, error handling
- Location: `internal/model/`
- Contains: request.go (ChatCompletionRequest, Message, Tool definitions), response.go (ChatCompletionResponse, Usage, Choice), error.go (error types, WriteError helper)
- Depends on: nothing (leaf layer)
- Used by: handlers, providers, storage

**Storage Layer:**
- Purpose: Persistent data access (request logging, cost overrides)
- Location: `internal/storage/`
- Contains: storage interface (storage.go), SQLite implementation (sqlite/)
- Depends on: model types
- Used by: handlers, main entry point, costmap

**Configuration Layer:**
- Purpose: Load and parse YAML config, environment variable expansion, hot-reload support
- Location: `internal/config/`
- Contains: config structs (config.go), loader (loader.go with environment variable expansion), reloader (reloader.go for hot-reload)
- Depends on: nothing (leaf layer)
- Used by: main entry point, handlers, router

**Cost Mapping Layer:**
- Purpose: Manage model cost specifications from external CDN, apply overrides, track costs
- Location: `internal/costmap/`
- Contains: costmap manager (costmap.go) that loads LiteLLM cost map JSON, applies user overrides from storage
- Depends on: storage, HTTP client
- Used by: handlers (for model detail endpoint and cost patch operations)

**OpenAPI Layer:**
- Purpose: Generate and serve OpenAPI specification for the API
- Location: `internal/openapi/`
- Contains: OpenAPI builder (openapi.go), schemas (schemas.go), operation definitions (operations.go)
- Depends on: model types
- Used by: handlers, HTTP router

**Middleware Layer:**
- Purpose: Cross-cutting concerns (auth, logging, error recovery, CORS)
- Location: `internal/api/middleware/`
- Contains: auth.go (master key validation), logging.go (request logging), recovery.go (panic handler), cors.go (CORS headers)
- Depends on: model types
- Used by: HTTP router

**Logging Layer:**
- Purpose: Initialize structured logger with configurable format/level
- Location: `internal/logger/`
- Contains: logger initialization (logger.go) wrapping zerolog
- Depends on: zerolog library
- Used by: main entry point, throughout codebase via `log.Info()`, `log.Error()`

## Data Flow

**Chat Completion Request (Non-Streaming):**

1. Client sends POST to `/v1/chat/completions` with `Authorization: Bearer <master_key>`
2. Middleware chain: Recovery → Logging → CORS → Auth
3. Handler (`ChatCompletions`) receives decoded `ChatCompletionRequest`
4. Handler validates model name and messages
5. Router's `GetDeploymentWithRetry()` selects healthy deployment (filters cooldown, respects tried set)
6. Handler creates provider request with actual model name from deployment
7. `deployment.Provider.ChatCompletion()` calls provider-specific implementation
   - OpenAI provider: marshals request, POSTs to OpenAI API, unmarshals response
   - Anthropic provider: translates OpenAI format → Anthropic format, calls Anthropic API, translates response back
8. On success: `Router.ReportSuccess()` resets failure count, handler logs request to storage, writes JSON response
9. On failure: `Router.ReportFailure()` increments failure count, triggers cooldown if threshold met, retries with next deployment
10. After all retries exhausted: return error response

**Chat Completion Stream:**

1. Same initial flow through handler
2. Handler calls `ChatCompletionStream()` instead
3. Provider returns `Stream` interface (channel-based adapter)
4. Handler writes SSE headers, reads chunks in loop: `data: {json}\n\n`
5. On stream close or error: stops writing, handler logs final usage stats

**State Management:**
- **Router State:** Deployment registry (model name → list of deployments) guarded by RWMutex
- **Cooldown State:** Per-deployment failure count and cooldown deadline maps, updated atomically
- **Storage State:** SQLite database with request_logs and cost_overrides tables
- **CostMap State:** In-memory manager loaded from CDN, updated via API patches

## Key Abstractions

**Provider Interface:**
- Purpose: Abstract LLM backend specifics
- Examples: `internal/provider/openai/openai.go`, `internal/provider/anthropic/anthropic.go`
- Pattern: Each provider implements `ChatCompletion()`, `ChatCompletionStream()`, `Embeddings()`, `SupportsEmbeddings()`. Self-registers via `init()` using provider registry factory pattern.

**Stream Interface:**
- Purpose: Abstract streaming response iteration
- Examples: Used in `internal/api/handler/chat.go` for streaming responses
- Pattern: Channel-based adapter (`streamAdapter` in `internal/provider/provider.go`) wraps Go channels, implements `Recv()` (returns chunk or io.EOF) and `Close()`

**Deployment Struct:**
- Purpose: Represent a specific model instance with routing metadata
- Examples: Created during router initialization from config, stored in `Router.deployments` map
- Pattern: Contains Provider interface pointer, allows swapping implementations; includes rate limits (RPM, TPM) for future rate limiting

**Strategy Interface:**
- Purpose: Abstract load balancing algorithm
- Examples: `internal/router/shuffle.go`, `internal/router/roundrobin.go`
- Pattern: `Select(deployments)` picks one; called by router after filtering healthy deployments

**Storage Interface:**
- Purpose: Abstract persistence layer
- Examples: `internal/storage/storage.go` defines interface; `internal/storage/sqlite/sqlite.go` implements
- Pattern: Methods: `LogRequest()`, `GetLogs()`, `UpsertCostMapKey()`, `DeleteCostOverride()`, `ListCostOverrides()`

## Entry Points

**Main Entry Point:**
- Location: `cmd/proxy/main.go`
- Triggers: Process start with `-config` flag pointing to YAML file
- Responsibilities:
  1. Parse `-config` flag
  2. Load and parse YAML with environment variable expansion
  3. Initialize zerolog logger
  4. Create provider registry and router (deployments loaded from config)
  5. Initialize storage (SQLite) and run migrations
  6. Build OpenAPI spec
  7. Load and seed cost overrides from storage into costmap manager
  8. Create HTTP router with all routes and middleware
  9. Start HTTP server with graceful shutdown

**HTTP Router Setup:**
- Location: `internal/api/router.go`
- Triggers: Called from main.go during startup
- Responsibilities: Register all routes with Chi router, apply global middleware, group protected routes under auth middleware

**Handler Entry Points (HTTP Endpoints):**
- `/health` - GET public endpoint, returns server health
- `/openapi.json` - GET public endpoint, serves OpenAPI spec JSON
- `/v1/chat/completions` - POST protected, chat completion (streaming or non-streaming)
- `/v1/embeddings` - POST protected, embeddings
- `/v1/models` - GET protected, list available models
- `/v1/models/{model}` - GET protected, model details with cost info
- `/v1/completions` - POST protected, stub (not fully implemented)
- `/admin/status` - GET protected, deployment health and uptime
- `/admin/config` - GET protected, sanitized config (no secrets)
- `/admin/reload` - POST protected, reload config and reset cooldowns
- `/admin/logs` - GET protected, paginated request logs
- `/admin/costmap` - GET protected, current costmap state
- `/admin/costmap/models` - GET protected, available cost map models
- `/admin/costmap/reload` - POST protected, reload costmap from CDN
- `/admin/costmap/url` - PUT protected, set custom costmap URL

## Error Handling

**Strategy:** Structured error types in `internal/model/error.go`, middleware catches panics, handlers return JSON error responses with status codes.

**Patterns:**
- **Panic Recovery:** `middleware.Recovery()` catches panics, logs, returns 500 with error JSON
- **Validation Errors:** Handlers return 400 with `ErrBadRequest` for invalid input
- **Authentication:** Middleware returns 401 `ErrUnauthorized` if master key missing/invalid
- **Model Not Found:** Handler returns 404 `ErrModelNotFound` if model doesn't exist in registry
- **Provider Errors:** Handler returns error from provider, on retry exhaustion returns `ErrProviderError`
- **Error Format:** All errors written via `model.WriteError(w, err)` which formats as JSON with error message and code

## Cross-Cutting Concerns

**Logging:**
- Framework: `github.com/rs/zerolog/log` (structured JSON logging)
- Initialization: `internal/logger/logger.go` reads config (level, format, file output)
- Patterns:
  - Info-level: startup messages, config loads, request completion
  - Warn-level: cost overrides load failures, config reload failures
  - Error-level: provider errors, storage errors, shutdown errors
  - Request flow: logged by middleware on entry, storage logged after completion with usage stats

**Validation:**
- Input validation in handlers before provider calls (model required, messages required, request body parseable)
- Config validation during load (provider names must be registered, parsing model strings)
- No global validation layer; each handler responsible for its inputs

**Authentication:**
- Single master key approach: `middleware.Auth()` checks `Authorization: Bearer <key>` header
- Applied to all protected routes via route group in `internal/api/router.go`
- Key comes from config at startup; changing requires restart

---

*Architecture analysis: 2026-03-25*
