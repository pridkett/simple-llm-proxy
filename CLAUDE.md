# CLAUDE.md

This file provides context for Claude when working on this codebase.

## Project Overview

Simple LLM Proxy is a lightweight Go-based LLM proxy server that provides OpenAI-compatible endpoints with multi-provider support (OpenAI, Anthropic). It uses LiteLLM-compatible YAML configuration.

## Build & Test Commands

```bash
# Build
make build                 # Build binary to bin/proxy
go build ./...             # Verify compilation

# Test
make test                  # Run all tests
go test ./... -v           # Verbose test output

# Run
make run                   # Build and run with config.yaml
./bin/proxy -config config.yaml
```

## Project Structure

```
cmd/proxy/main.go              # Entry point, server setup, graceful shutdown
internal/
  api/
    handler/                   # HTTP handlers (chat.go, models.go, health.go, embeddings.go)
    middleware/                # auth.go, logging.go, recovery.go
    router.go                  # Chi router setup, route registration
  config/
    config.go                  # Config structs (ModelConfig, RouterSettings, GeneralSettings)
    loader.go                  # YAML parsing, os.environ/ expansion
  model/
    request.go                 # OpenAI-compatible request types
    response.go                # OpenAI-compatible response types
    error.go                   # Error types and WriteError helper
  provider/
    provider.go                # Provider interface, Stream interface, Deployment struct
    registry.go                # Provider factory registry
    openai/openai.go           # OpenAI implementation
    anthropic/anthropic.go     # Anthropic implementation with message translation
  router/
    router.go                  # Load balancing router, deployment management
    strategy.go                # Strategy interface
    shuffle.go                 # Random selection strategy
    roundrobin.go              # Round-robin strategy
    cooldown.go                # Failure tracking and cooldown management
  storage/
    storage.go                 # Storage interface
    sqlite/                    # SQLite implementation with migrations
```

## Key Patterns

### Provider Interface
All LLM providers implement `provider.Provider`:
```go
type Provider interface {
    Name() string
    ChatCompletion(ctx, req) (*ChatCompletionResponse, error)
    ChatCompletionStream(ctx, req) (Stream, error)
    Embeddings(ctx, req) (*EmbeddingsResponse, error)
    SupportsEmbeddings() bool
}
```

### Provider Registration
Providers self-register via `init()`:
```go
func init() {
    provider.Register("openai", New)
}
```

### Config Environment Expansion
Config values like `os.environ/VAR_NAME` are expanded to environment variable values in `config/loader.go`.

### Request Flow
1. Request hits handler in `api/handler/`
2. Handler calls `router.GetDeploymentWithRetry()` to get a healthy deployment
3. Handler calls `deployment.Provider.ChatCompletion()` or `ChatCompletionStream()`
4. Router tracks success/failure via `ReportSuccess()`/`ReportFailure()`
5. Cooldown manager takes deployments offline after repeated failures

### Anthropic Translation
`provider/anthropic/anthropic.go` translates:
- OpenAI messages → Anthropic format (extracts system messages)
- Anthropic responses → OpenAI format
- Tool calls between formats
- Stop reasons (`end_turn` → `stop`, `max_tokens` → `length`)

### Streaming
Both providers return `provider.Stream` interface for SSE streaming. Handlers write `data: {json}\n\n` format and flush after each chunk.

## Configuration Format

```yaml
model_list:
  - model_name: gpt-4              # User-facing name
    litellm_params:
      model: openai/gpt-4          # provider/actual-model
      api_key: os.environ/OPENAI_API_KEY
    rpm: 100

router_settings:
  routing_strategy: simple-shuffle  # or round-robin
  num_retries: 2
  allowed_fails: 3
  cooldown_time: 30s

general_settings:
  master_key: os.environ/PROXY_MASTER_KEY
  database_url: ./proxy.db
  port: 8080
```

## Dependencies

- `github.com/go-chi/chi/v5` - HTTP router
- `gopkg.in/yaml.v3` - YAML parsing
- `modernc.org/sqlite` - Pure Go SQLite (no CGO)

## Adding a New Provider

1. Create `internal/provider/newprovider/newprovider.go`
2. Implement `provider.Provider` interface
3. Add `init()` function to register: `provider.Register("newprovider", New)`
4. Import in `cmd/proxy/main.go`: `_ "github.com/pwagstro/simple_llm_proxy/internal/provider/newprovider"`
