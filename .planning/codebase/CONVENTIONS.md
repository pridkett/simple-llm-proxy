# Coding Conventions

**Analysis Date:** 2026-03-25

## Naming Patterns

**Files:**
- Go source files use lowercase with underscores: `openai.go`, `auth_test.go`, `cost_overrides.go`
- Test files follow pattern: `{module}_test.go` (e.g., `health_test.go`)
- Package directories use lowercase: `provider/`, `handler/`, `middleware/`, `storage/`

**Functions:**
- Public functions (exported) use PascalCase: `New()`, `ChatCompletion()`, `GetDeployment()`, `ListModels()`
- Private functions use camelCase: `handleStreamingResponse()`, `handleNonStreamingResponse()`, `seedCostOverrides()`
- Handler functions return `http.HandlerFunc`: `Health()`, `ChatCompletions()`, `AdminStatus()`
- Middleware functions return middleware chains: `Auth()`, `Logging()`, `Recovery()`

**Variables:**
- Local variables use camelCase: `startTime`, `deployment`, `masterKey`, `statusCode`
- Exported struct fields use PascalCase: `ModelName`, `APIKey`, `Provider`, `Stream`
- Private struct fields use camelCase: `apiKey`, `baseURL`, `status`, `size`
- Constants use UPPER_SNAKE_CASE: `defaultBaseURL`, `testJSON` (in test files)

**Types:**
- Struct names use PascalCase: `ChatCompletionRequest`, `ProxyError`, `Deployment`, `RouterSettings`
- Interface names use PascalCase: `Provider`, `Strategy`, `Storage`, `Stream`
- Method receivers use short names (1-2 chars): `func (p *Provider)`, `func (r *Router)`, `func (m *mockProvider)`

## Code Style

**Formatting:**
- Go standard formatter via `go fmt` - invoked by `make fmt`
- Line length: Default Go convention (no hard limit, favor readability)
- Indentation: Tabs (Go standard)
- Import organization: Standard library first, then external packages, then internal packages

**Linting:**
- Tool: `golangci-lint` - invoked by `make lint`
- Error checking: All errors explicitly handled or deliberately ignored with `//nolint:errcheck` annotation
- Example: `json.NewEncoder(w).Encode(resp) //nolint:errcheck` in `handler/models.go` where error is non-critical
- Unused variables: Avoid `_` prefix for unused receiver names; use underscore parameter: `func (p *Provider) ChatCompletion(_ context.Context, _ *model.ChatCompletionRequest)`

## Import Organization

**Order:**
1. Standard library imports (e.g., `"context"`, `"encoding/json"`, `"net/http"`)
2. External packages (e.g., `"github.com/go-chi/chi/v5"`, `"github.com/rs/zerolog"`)
3. Internal packages (e.g., `"github.com/pwagstro/simple_llm_proxy/internal/..."`)
4. Blank imports for side effects: `_ "github.com/pwagstro/simple_llm_proxy/internal/provider/anthropic"` (provider registration)

**Example from `cmd/proxy/main.go`:**
```go
import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/pwagstro/simple_llm_proxy/internal/api"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	// ... more internal imports

	// Register providers
	_ "github.com/pwagstro/simple_llm_proxy/internal/provider/anthropic"
	_ "github.com/pwagstro/simple_llm_proxy/internal/provider/openai"
)
```

**Path Aliases:**
- No custom aliases used; full import paths with module prefix `github.com/pwagstro/simple_llm_proxy/internal/...`

## Error Handling

**Patterns:**
- Explicit error wrapping with context: `fmt.Errorf("marshaling request: %w", err)` in `internal/provider/openai/openai.go`
- No panic in production code; all errors returned or logged
- Custom error types: `ProxyError` struct in `internal/model/error.go` with:
  - `Error()` method for string representation
  - `Unwrap()` method for error chain traversal
  - `ToAPIError()` method for API response conversion

**Error Constructors:**
- Named constructors for common errors: `ErrBadRequest()`, `ErrUnauthorized()`, `ErrModelNotFound()`, `ErrProviderError()`
- Each constructor sets appropriate HTTP status code
- Example: `ErrBadRequest(message string) *ProxyError` with `StatusCode: http.StatusBadRequest`

**Response Writing:**
- Centralized error response: `model.WriteError(w http.ResponseWriter, err *ProxyError)` in handler functions
- Success responses use `json.NewEncoder(w).Encode(resp)`
- All response handlers set `Content-Type: application/json` header before writing

## Logging

**Framework:** `github.com/rs/zerolog` (structured logging)

**Patterns:**
- Structured fields instead of string formatting: `log.Info().Str("addr", addr).Msg("starting server")`
- Log levels by severity: `log.Error()`, `log.Warn()`, `log.Info()`, `log.Debug()`, `log.Trace()`
- HTTP request logging in middleware with status-based levels:
  - 5xx errors: `log.Error()`
  - 4xx warnings: `log.Warn()`
  - 2xx/3xx: `log.Info()`
- Example from `internal/api/middleware/logging.go`:
  ```go
  ev.Str("method", r.Method).
      Str("path", r.URL.Path).
      Int("status", rw.status).
      Str("duration", duration.Truncate(time.Microsecond).String()).
      Int("bytes", rw.size).
      Msg("request")
  ```

**When to Log:**
- Server startup/shutdown events
- Configuration load/reload
- Provider errors and retries
- HTTP request/response (middleware handles automatically)
- Storage operations (non-critical failures at warn level)
- Never log secrets (master key, API keys are excluded)

## Comments

**When to Comment:**
- Explain "why" not "what" - code itself should be clear
- Document non-obvious design decisions
- Example from `internal/provider/anthropic/anthropic.go`: Explain message translation format
- Example from `cmd/proxy/main.go`: Line 36 explains logger initialization timing

**JSDoc/TSDoc:**
- Not used (Go project)
- Use godoc comments for exported functions: `// SomeFunction does something.` immediately before declaration
- Example: `// Logging returns middleware that logs requests with structured fields.` in `middleware/logging.go`

## Function Design

**Size:**
- Keep functions under 50 lines where practical
- Handlers typically 20-40 lines
- Helper functions (e.g., `handleNonStreamingResponse`) extracted from main handler logic
- Test setup helpers like `newTestServer()`, `configForTest()`, `makeMockConfig()` are 10-20 lines

**Parameters:**
- Limit to 3-4 parameters maximum
- Use dependency injection for required services (e.g., `func ChatCompletions(r *router.Router, store storage.Storage)`)
- Group related parameters: provider/config parameters passed as structs (e.g., `req *model.ChatCompletionRequest`)
- Context always first parameter in async operations: `func (p *Provider) ChatCompletion(ctx context.Context, req *model.ChatCompletionRequest)`

**Return Values:**
- Error always last return value: `(*model.ChatCompletionResponse, error)`
- Use pointer return types for structs that may be large or modified: `*provider.Deployment`
- Multiple return values limited to 2 typically (value, error)
- Handlers return `error` for control flow (nil on success, error triggers failure handling)

## Module Design

**Exports:**
- Only types and functions that form the public API are exported (PascalCase)
- Implementation details remain private (lowercase)
- Example: `internal/provider/provider.go` exports `Provider` interface and `Stream` interface
- Example: `internal/router/router.go` exports `Router` type with methods like `GetDeployment()`, `Reload()`

**Barrel Files:**
- Not used; direct imports from specific modules
- No `internal/api/__init__.go` style aggregation files
- Import paths are explicit: `import "github.com/pwagstro/simple_llm_proxy/internal/api/handler"`

**Package Structure:**
- Each subdirectory is a distinct package
- Related functionality grouped by domain: `api/`, `provider/`, `router/`, `storage/`
- Handlers live in `internal/api/handler/`
- Providers live in `internal/provider/{provider_name}/`

## Concurrency

**Patterns:**
- Goroutines used for:
  - Server startup/shutdown: `go func() { server.ListenAndServe() }`
  - Cost map loading (non-blocking): `go func() { cm.Load(ctx) }`
  - Request logging (async): `go logRequest(store, deployment, ...)`
- No explicit mutex usage in application code; SQLite handles concurrency via WAL mode
- Channels used in streaming responses via `provider.Stream` interface

## Testing Conventions

**Naming:**
- Test function: `Test{FunctionName}` - e.g., `TestHealth()`, `TestAdminStatus()`
- Table-driven tests: `TestAuth()` uses `[]struct{ name, masterKey, authHeader, wantStatus }`
- Subtests: `t.Run(tt.name, func(t *testing.T) { ... })`

**Assertions:**
- Manual assertions (no assertion library): `if actual != expected { t.Errorf("Expected %v, got %v", expected, actual) }`
- Status code checks: `if rr.Code != http.StatusOK { t.Errorf(...) }`
- Fatality for setup errors: `t.Fatalf()` when test cannot proceed
- Errors for assertion failures: `t.Errorf()` for check failures

**Mocking:**
- Custom mock types embedded in test files
- Example: `type mockProvider struct{ name string }` in `router_test.go`
- Example: `type testProvider struct{ name string }` in `handler/admin_test.go`
- Mocks implement full interface methods (even if no-op)
- Provider mock registration via `init()` block before tests run

---

*Convention analysis: 2026-03-25*
