# Simple LLM Proxy - Implementation Plan

A lightweight Go-based LLM proxy server inspired by LiteLLM, providing OpenAI-compatible endpoints with multi-provider support.

## Project Structure

```
simple_llm_proxy/
├── cmd/
│   └── proxy/
│       └── main.go                 # Application entry point
├── internal/
│   ├── api/
│   │   ├── handler/
│   │   │   ├── chat.go             # POST /v1/chat/completions
│   │   │   ├── completions.go      # POST /v1/completions
│   │   │   ├── embeddings.go       # POST /v1/embeddings
│   │   │   ├── models.go           # GET /v1/models
│   │   │   └── health.go           # GET /health
│   │   ├── middleware/
│   │   │   ├── auth.go             # Master key authentication
│   │   │   ├── logging.go          # Request/response logging
│   │   │   └── recovery.go         # Panic recovery
│   │   └── router.go               # Chi router setup
│   ├── config/
│   │   ├── config.go               # Config structs (LiteLLM-compatible)
│   │   └── loader.go               # YAML parsing with env var expansion
│   ├── provider/
│   │   ├── provider.go             # Provider interface
│   │   ├── registry.go             # Provider registry
│   │   ├── openai/
│   │   │   └── openai.go           # OpenAI provider (priority)
│   │   └── anthropic/
│   │       └── anthropic.go        # Anthropic provider (priority, message translation)
│   ├── router/
│   │   ├── router.go               # Load balancing router
│   │   ├── strategy.go             # Routing strategy interface
│   │   ├── shuffle.go              # Simple shuffle strategy
│   │   ├── roundrobin.go           # Round-robin strategy
│   │   └── cooldown.go             # Cooldown manager
│   ├── model/
│   │   ├── request.go              # OpenAI-compatible request types
│   │   ├── response.go             # OpenAI-compatible response types
│   │   └── error.go                # Error response types
│   └── storage/
│       ├── storage.go              # Storage interface
│       └── sqlite/
│           ├── sqlite.go           # SQLite implementation
│           └── migrations.go       # Schema migrations
├── config.yaml.example
├── go.mod
├── go.sum
└── Makefile
```

## Configuration Format (LiteLLM-Compatible)

```yaml
model_list:
  # OpenAI models
  - model_name: gpt-4                    # User-facing name
    litellm_params:
      model: openai/gpt-4                # provider/model format
      api_key: os.environ/OPENAI_API_KEY # Env var expansion
    rpm: 100                             # Optional rate limit

  - model_name: gpt-4o
    litellm_params:
      model: openai/gpt-4o
      api_key: os.environ/OPENAI_API_KEY

  # Anthropic models
  - model_name: claude-3-sonnet
    litellm_params:
      model: anthropic/claude-3-5-sonnet-20240620
      api_key: os.environ/ANTHROPIC_API_KEY
    rpm: 50

  - model_name: claude-3-opus
    litellm_params:
      model: anthropic/claude-3-opus-20240229
      api_key: os.environ/ANTHROPIC_API_KEY

router_settings:
  routing_strategy: simple-shuffle       # or "round-robin"
  num_retries: 2
  allowed_fails: 3
  cooldown_time: 30s

general_settings:
  master_key: os.environ/PROXY_MASTER_KEY
  database_url: ./proxy.db               # SQLite path
  port: 8080
```

## Key Interfaces

**Provider Interface:**
```go
type Provider interface {
    Name() string
    ChatCompletion(ctx, req) (*ChatCompletionResponse, error)
    ChatCompletionStream(ctx, req) (Stream, error)
    Embeddings(ctx, req) (*EmbeddingsResponse, error)
}
```

**Router Interface:**
```go
type Router interface {
    GetDeployment(ctx, modelName) (*Deployment, error)
    ReportSuccess(deployment)
    ReportFailure(deployment)
    ListModels() []string
}
```

## API Endpoints

| Method | Path | Description | Priority |
|--------|------|-------------|----------|
| POST | /v1/chat/completions | Chat completions (streaming + non-streaming) | Core |
| GET | /v1/models | List available models | Core |
| GET | /health | Health check | Core |
| POST | /v1/embeddings | Generate embeddings | Phase 7 |

## Dependencies (Minimal)

- `github.com/go-chi/chi/v5` - Lightweight router
- `gopkg.in/yaml.v3` - YAML config parsing
- `modernc.org/sqlite` - Pure Go SQLite (no cgo)

## Database Schema (Future-Ready)

**api_keys** - For future multi-key support:
- key_hash, key_alias, created_at, expires_at, is_active
- max_rpm, max_tpm, max_budget, allowed_models

**usage_logs** - Request tracking:
- request_id, api_key_id, model, provider, endpoint
- prompt_tokens, completion_tokens, total_cost
- status_code, latency_ms, request_time

## Implementation Phases

### Phase 1: Foundation
- [ ] Initialize Go module and directory structure
- [ ] Implement config parsing with `os.environ/VAR` expansion
- [ ] Create OpenAI-compatible request/response types
- [ ] Set up chi router with health endpoint

### Phase 2: OpenAI Provider
- [ ] Define Provider interface with streaming support
- [ ] Implement provider registry
- [ ] Create OpenAI provider (chat completions + streaming)

### Phase 3: Load Balancing Router
- [ ] Implement deployment tracking
- [ ] Add simple-shuffle routing strategy
- [ ] Implement cooldown manager for failed deployments
- [ ] Add retry logic

### Phase 4: Core API Endpoints
- [ ] POST /v1/chat/completions (non-streaming + streaming SSE)
- [ ] GET /v1/models
- [ ] Authentication middleware (MASTER_KEY)
- [ ] Request/response logging middleware

### Phase 5: Anthropic Provider
- [ ] Implement OpenAI→Anthropic message format translation
- [ ] Handle Anthropic-specific requirements (max_tokens default)
- [ ] Support streaming with Anthropic's SSE format

### Phase 6: Storage & Logging
- [ ] SQLite initialization with migrations
- [ ] Usage tracking (tokens, latency, costs)
- [ ] Prepare schema for future multi-key support

### Phase 7: Polish
- [ ] POST /v1/embeddings endpoint
- [ ] Round-robin routing strategy
- [ ] Comprehensive error handling
- [ ] Example config and documentation

**Future (not in initial scope):**
- Azure OpenAI provider
- POST /v1/completions (legacy) endpoint
- Multi-key authentication

## Verification Plan

1. **Unit tests**: Run `go test ./...`
2. **Manual testing**:
   ```bash
   # Start server
   PROXY_MASTER_KEY=test-key ./proxy -config config.yaml

   # Test health
   curl http://localhost:8080/health

   # List models
   curl -H "Authorization: Bearer test-key" http://localhost:8080/v1/models

   # Chat completion
   curl -X POST http://localhost:8080/v1/chat/completions \
     -H "Authorization: Bearer test-key" \
     -H "Content-Type: application/json" \
     -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}'
   ```
3. **Streaming test**: Verify SSE chunks arrive correctly
4. **Failover test**: Stop one deployment, verify routing to others
