# Codebase Structure

**Analysis Date:** 2026-03-25

## Directory Layout

```
simple-llm-proxy/
├── cmd/
│   └── proxy/
│       └── main.go                    # Server entry point, initialization
├── internal/
│   ├── api/
│   │   ├── handler/                   # HTTP request handlers
│   │   ├── middleware/                # Middleware (auth, logging, CORS, recovery)
│   │   └── router.go                  # HTTP route registration (Chi)
│   ├── config/
│   │   ├── config.go                  # Config struct definitions
│   │   ├── loader.go                  # YAML parsing, environment expansion
│   │   └── reloader.go                # Hot-reload support
│   ├── costmap/
│   │   └── costmap.go                 # Cost map manager, CDN loading
│   ├── logger/
│   │   └── logger.go                  # Zerolog initialization
│   ├── model/
│   │   ├── request.go                 # Request types (ChatCompletionRequest, etc)
│   │   ├── response.go                # Response types (ChatCompletionResponse, etc)
│   │   └── error.go                   # Error types, WriteError helper
│   ├── openapi/
│   │   ├── openapi.go                 # OpenAPI spec builder
│   │   ├── schemas.go                 # JSON schema definitions
│   │   └── operations.go              # Endpoint operation definitions
│   ├── provider/
│   │   ├── provider.go                # Provider interface, Stream interface
│   │   ├── registry.go                # Provider factory registry
│   │   ├── openai/
│   │   │   └── openai.go              # OpenAI provider implementation
│   │   └── anthropic/
│   │       └── anthropic.go           # Anthropic provider with format translation
│   ├── router/
│   │   ├── router.go                  # Router: deployment registry, GetDeployment*
│   │   ├── strategy.go                # Strategy interface (Load balancing)
│   │   ├── shuffle.go                 # Simple random selection
│   │   ├── roundrobin.go              # Round-robin selection
│   │   └── cooldown.go                # Failure tracking, cooldown management
│   └── storage/
│       ├── storage.go                 # Storage interface
│       └── sqlite/
│           ├── sqlite.go              # SQLite implementation
│           ├── migrations.go          # Schema and migrations
│           └── cost_overrides.go      # Cost override queries
├── frontend/                           # Vue 3 + Vite web UI (separate)
│   ├── src/
│   │   ├── api/                       # API client functions
│   │   ├── components/                # Vue components
│   │   ├── composables/               # Composition API hooks
│   │   ├── views/                     # Page components
│   │   ├── router/                    # Frontend routing
│   │   ├── App.vue
│   │   └── main.js
│   ├── tests/                         # Vitest unit tests
│   └── vite.config.js
├── adr/                               # Architecture Decision Records
├── backlog/                           # Issue tracking (tasks, drafts, decisions)
├── config.yaml                        # Example config
├── config.yaml.example
├── Makefile                           # Build, test, run targets
├── go.mod, go.sum                     # Go module dependencies
├── CLAUDE.md                          # Project instructions for Claude
└── README.md
```

## Directory Purposes

**cmd/proxy:**
- Purpose: Server entry point
- Contains: main.go only
- Responsibilities: Parse config, initialize all components, start HTTP server

**internal/api:**
- Purpose: HTTP API layer
- Contains: handlers (business logic), middleware (cross-cutting), router (route registration)
- Key files:
  - `handler/`: One file per endpoint group (chat.go, models.go, embeddings.go, admin.go, health.go, costmap.go, openapi.go)
  - `middleware/`: Pluggable middleware (auth, logging, recovery, CORS)
  - `router.go`: Single file registering routes with Chi

**internal/config:**
- Purpose: Configuration loading and management
- Contains: Struct definitions, YAML parsing, hot-reload
- Key patterns:
  - `config.go`: Defines Config, ModelConfig, RouterSettings, GeneralSettings, LogSettings
  - `loader.go`: Parses YAML, expands `os.environ/VAR_NAME` syntax
  - `reloader.go`: Watches config file, supports live reload without restart

**internal/costmap:**
- Purpose: Model cost management
- Contains: Single manager that loads from CDN, caches locally, applies overrides
- Key concepts:
  - Loads LiteLLM cost map JSON from CDN (URL configurable)
  - Stores user overrides in SQLite (CostMapKey reference or CustomSpec JSON)
  - Provides API endpoints for management

**internal/logger:**
- Purpose: Structured logging initialization
- Contains: Single logger.go with Init() function
- Configures zerolog with level, format (console or JSON), file rotation

**internal/model:**
- Purpose: Data structures for requests/responses
- Contains:
  - `request.go`: ChatCompletionRequest, Message, Tool, FunctionDef, EmbeddingsRequest
  - `response.go`: ChatCompletionResponse, Choice, Delta, Usage, EmbeddingData, ModelsResponse
  - `error.go`: APIError, ErrorDetail types, error constructors (ErrBadRequest, ErrUnauthorized, etc), WriteError helper

**internal/openapi:**
- Purpose: OpenAPI specification generation
- Contains:
  - `openapi.go`: Builder with Build() method, returns Spec with JSON serialization
  - `schemas.go`: Reusable JSON schema definitions (ChatCompletionRequest, etc)
  - `operations.go`: Endpoint definitions (paths, parameters, responses)

**internal/provider:**
- Purpose: LLM provider abstraction
- Contains:
  - `provider.go`: Provider interface, Stream interface, Deployment struct, NewStream constructor
  - `registry.go`: Factory registry, Get() function for looking up providers
  - `openai/openai.go`: OpenAI implementation (API calls to api.openai.com)
  - `anthropic/anthropic.go`: Anthropic implementation with format translation (OpenAI ↔ Anthropic)

**internal/router:**
- Purpose: Load balancing and deployment management
- Contains:
  - `router.go`: Main Router struct, GetDeployment, GetDeploymentWithRetry, ReportSuccess/Failure, ListModels, GetStatus, Reload
  - `strategy.go`: Strategy interface (Select method)
  - `shuffle.go`: Random selection strategy
  - `roundrobin.go`: Round-robin selection strategy
  - `cooldown.go`: CooldownManager tracks failures, applies cooldown isolation

**internal/storage:**
- Purpose: Persistence layer
- Contains:
  - `storage.go`: Storage interface with methods: LogRequest, GetLogs, Upsert/Delete/ListCostOverrides
  - `sqlite/sqlite.go`: SQLite implementation (pure Go, no CGO)
  - `sqlite/migrations.go`: Schema creation (request_logs, cost_overrides tables)
  - `sqlite/cost_overrides.go`: Cost override SQL queries

**frontend:**
- Purpose: Web UI for admin functions
- Technologies: Vue 3, Vite, Tailwind CSS, Vitest
- Key paths:
  - `src/api/`: HTTP client functions for `/admin/*` and `/v1/*` endpoints
  - `src/views/`: Pages (Dashboard, Models, Logs, Config, Settings)
  - `tests/`: Unit tests (36 tests across 6 files)
- Note: Separate from Go backend but deployed together

## Key File Locations

**Entry Points:**
- `cmd/proxy/main.go`: Process entry, CLI flag parsing, component initialization, HTTP server startup

**Configuration:**
- `config.yaml`: Example configuration file
- `config.yaml.example`: Template
- `internal/config/config.go`: Struct definitions
- `internal/config/loader.go`: YAML parsing with environment expansion

**Core LLM Routing:**
- `internal/router/router.go`: Model registry, deployment selection, failure tracking
- `internal/provider/provider.go`: Provider interface
- `internal/provider/openai/openai.go`: OpenAI backend
- `internal/provider/anthropic/anthropic.go`: Anthropic backend with translation

**Request/Response Handling:**
- `internal/model/request.go`: Request types
- `internal/model/response.go`: Response types
- `internal/model/error.go`: Error handling
- `internal/api/handler/chat.go`: Chat completion endpoint (streaming + non-streaming)
- `internal/api/handler/embeddings.go`: Embeddings endpoint
- `internal/api/handler/models.go`: Model listing and details
- `internal/api/middleware/auth.go`: Master key validation

**Testing:**
- Go tests: `*_test.go` files alongside implementation (internal/config/loader_test.go, internal/router/router_test.go, etc)
- Frontend tests: `frontend/tests/unit/*.test.js` (Vue components)
- Run: `make test` for all, `go test ./...` for Go, `npm test` for frontend

## Naming Conventions

**Files:**
- Go files: lowercase_with_underscores.go (e.g., `cost_overrides.go`, `roundrobin.go`)
- Test files: same name + `_test.go` suffix (e.g., `loader_test.go`)
- Vue files: PascalCase.vue (e.g., `Dashboard.vue`, `ModelsList.vue`)

**Directories:**
- Go packages: lowercase no underscores (e.g., `provider`, `costmap`, `openapi`)
- Domain-grouped: organize by feature (e.g., `provider/openai`, `storage/sqlite`)

**Functions:**
- Exported: PascalCase (e.g., `New()`, `GetDeployment()`, `ChatCompletion()`)
- Unexported: camelCase (e.g., `seedCostOverrides()`, `handleNonStreamingResponse()`)

**Variables:**
- Constants: UPPER_SNAKE_CASE or camelCase depending on scope (e.g., `defaultBaseURL`, `http.StatusOK`)
- Receiver names: single letter or short abbreviation (e.g., `p *Provider`, `c *CooldownManager`, `r *Router`)

**Struct Fields:**
- JSON tags: snake_case (e.g., `"model_name"`, `"api_key"`, `"max_tokens"`)
- Exported fields: PascalCase (e.g., `Model`, `APIKey`, `Provider`)

**Types:**
- Interfaces: -er suffix or noun (e.g., `Provider`, `Stream`, `Strategy`, `Storage`)
- Structs: descriptive nouns (e.g., `ChatCompletionRequest`, `Deployment`, `CostOverride`)

## Where to Add New Code

**New LLM Provider:**
1. Create `internal/provider/newprovider/newprovider.go`
2. Implement `provider.Provider` interface
3. Add `init()` to register: `provider.Register("newprovider", New)`
4. Import in `cmd/proxy/main.go`: `_ "github.com/pwagstro/simple-llm-proxy/internal/provider/newprovider"`
5. Test: `internal/provider/newprovider/newprovider_test.go`

**New API Endpoint:**
1. Add handler function to `internal/api/handler/` (create new file or extend existing)
2. Handler signature: `func HandlerName() http.HandlerFunc { return func(w http.ResponseWriter, r *http.Request) { ... } }`
3. Register in `internal/api/router.go` under appropriate route group (public or protected)
4. Add test: `internal/api/handler/handlername_test.go`
5. Update OpenAPI spec: `internal/openapi/operations.go`

**New Load Balancing Strategy:**
1. Create `internal/router/newstrategy.go`
2. Implement `router.Strategy` interface (Select method)
3. Add instantiation in `internal/router/router.go` New() and Reload() switches
4. Test: `internal/router/newstrategy_test.go`

**New Storage Backend:**
1. Create `internal/storage/newbackend/newbackend.go`
2. Implement `storage.Storage` interface
3. Create initialization like `internal/storage/sqlite/sqlite.go`
4. Register in `cmd/proxy/main.go` initialization based on config
5. Test: `internal/storage/newbackend/newbackend_test.go`

**Utilities/Helpers:**
- Shared helpers: `internal/utils/` or domain-specific subdirectory
- Cross-package utils: Consider if they belong in `internal/model/` (types) or create `internal/utils/`

**Frontend Changes:**
- New page: `frontend/src/views/PageName.vue`
- New component: `frontend/src/components/ComponentName.vue`
- API client functions: `frontend/src/api/client.js` or separate `apiFunction.js`
- Tests: `frontend/tests/unit/ComponentName.test.js`

## Special Directories

**adr/:**
- Purpose: Architecture Decision Records (markdown documents)
- Generated: No (manually created)
- Committed: Yes
- Pattern: One ADR per major architectural decision (e.g., `002-cost-map-model-mapping.md`)

**backlog/:**
- Purpose: Issue tracking and planning outside GitHub
- Generated: No (manually managed)
- Committed: Yes
- Subdirectories: tasks/, drafts/, decisions/, archive/

**.planning/codebase/**
- Purpose: GSD codebase documentation (this file and others)
- Generated: Yes (created by GSD agents)
- Committed: Yes (part of .planning/ directory)

**frontend/**
- Purpose: Vue 3 web UI (separate from Go backend)
- Generated: Partially (package-lock.json, dist/ on build)
- Committed: src/, tests/, config files committed; node_modules/ and dist/ not committed
- Build: `npm run build` outputs to dist/

**bin/**
- Purpose: Compiled binaries
- Generated: Yes (by `make build` or `go build`)
- Committed: No (in .gitignore)

---

*Structure analysis: 2026-03-25*
