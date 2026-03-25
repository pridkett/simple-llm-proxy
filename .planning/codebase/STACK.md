# Technology Stack

**Analysis Date:** 2026-03-25

## Languages

**Primary:**
- Go 1.25.4 - Backend proxy server, CLI, core business logic
- JavaScript/TypeScript (Vue 3) - Frontend web UI in `frontend/` directory

## Runtime

**Environment:**
- Go 1.25.4 runtime (compiled binaries available for Linux, macOS, Windows)
- Node.js (frontend development and build)

**Package Manager:**
- Go modules (`go.mod`, `go.sum`)
- npm (frontend - `frontend/package.json`)

## Frameworks

**Backend:**
- Chi v5.2.4 - HTTP router and middleware framework (`internal/api/router.go`)
- gopkg.in/yaml.v3 v3.0.1 - YAML configuration parsing (`internal/config/`)

**Frontend:**
- Vue 3 ^3.5.13 - Frontend framework (`frontend/package.json`)
- Vite 6.0.7 - Build tool and dev server (`frontend/package.json`)
- Vue Router 4.5.0 - Frontend routing (`frontend/package.json`)
- Tailwind CSS 3.4.17 - Utility-first CSS framework (`frontend/package.json`)

**Testing:**
- Go: `go test` built-in (stdlib)
- Frontend: Vitest 2.1.8 (`frontend/package.json`)
- Frontend: Vue Test Utils 2.4.6 (`frontend/package.json`)
- Frontend: jsdom 26.0.0 - DOM simulation for tests (`frontend/package.json`)

**Build/Dev:**
- Go: `make` for build automation (`Makefile`)
- Frontend: npm scripts (dev, build, test)
- Frontend: PostCSS 8.4.49 + Autoprefixer 10.4.20 (CSS processing)

## Key Dependencies

**Critical:**
- modernc.org/sqlite v1.44.3 - Pure Go SQLite driver (no CGO dependency). Used in `internal/storage/sqlite/sqlite.go` for request logging and cost overrides.
- github.com/getkin/kin-openapi v0.128.0 - OpenAPI specification parsing and generation (`internal/openapi/`)
- github.com/rs/zerolog v1.34.0 - Structured JSON logging (`internal/logger/`, replaces stdlib log)
- gopkg.in/natefinch/lumberjack.v2 v2.2.1 - Log file rotation and management (`internal/logger/`)

**Infrastructure:**
- github.com/go-chi/chi/v5 v5.2.4 - HTTP request routing and middleware
- gopkg.in/yaml.v3 v3.0.1 - YAML configuration parsing
- github.com/google/uuid v1.6.0 - UUID generation (transitive)

## Configuration

**Environment:**
- Configured via YAML file passed as `-config` flag: `./bin/proxy -config config.yaml`
- Environment variable expansion in config via `os.environ/VAR_NAME` syntax (parsed in `internal/config/loader.go`)
- Master key, database URL, port, logging, routing strategy, rate limits all config-driven

**Build:**
- `Makefile` for all build targets
- Multi-platform builds: Linux (amd64, arm64), macOS (amd64, arm64), Windows (amd64)
- Build command: `make build` → outputs `bin/proxy`

## Platform Requirements

**Development:**
- Go 1.25.4
- Node.js/npm (for frontend)
- Make (Unix-like development)
- Optional: golangci-lint (for `make lint`)

**Production:**
- Linux (amd64 or arm64), macOS, or Windows system
- Standalone binary (no runtime dependencies - pure Go, no CGO)
- SQLite database file (created at path specified in config, default `./proxy.db`)
- Internet connectivity (for OpenAI/Anthropic API calls, LiteLLM cost map downloads)

---

*Stack analysis: 2026-03-25*
