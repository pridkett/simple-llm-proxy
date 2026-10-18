# Simple LLM Proxy

A Go proxy for apps using OpenAI-compatible chat completions. Keep upstream OpenAI, OpenRouter, Anthropic, and Ollama credentials in one service, and issue separate keys to client apps.

For the LiteLLM replacement checklist, tested behavior, limitations, and staged OpenWebUI cutover, see [the pilot guide](deploy/openwebui-pilot/README.md).

## Features

- **OpenAI-compatible API** - Chat completions, streamed tools, and embeddings
- **Multi-provider support** - Route requests to OpenAI, OpenRouter, Anthropic, and Ollama
- **Automatic message translation** - Converts OpenAI format to Anthropic format transparently
- **Streaming support** - Full SSE streaming for both providers
- **Load balancing** - Simple shuffle or round-robin routing strategies
- **Failover & retries** - Automatic retry with cooldown for failed deployments
- **LiteLLM-compatible config** - Uses familiar `model_list` YAML format with `os.environ/` expansion
- **Usage logging** - SQLite-based request tracking
- **App keys** - Per-app keys with model restrictions, revocation, and usage attribution; OIDC protects administration
- **OpenAPI specification** - Dynamic OpenAPI 3.0 spec available at `/openapi.json`

## Installation

### From Source

```bash
git clone https://github.com/pridkett/simple-llm-proxy.git
cd simple-llm-proxy
just build
```

### Dependencies

- Go 1.25.4+
- No CGO required (uses pure Go SQLite)

## Quick Start

1. **Create a config file:**

```bash
cp config.yaml.example config.yaml
```

2. **Set environment variables:**

```bash
export PROXY_MASTER_KEY=your-secret-key
export OPENAI_API_KEY=sk-...
export ANTHROPIC_API_KEY=sk-ant-...
```

3. **Run the proxy:**

```bash
just run
# or
./bin/proxy -config config.yaml
```

4. **Test it:**

```bash
# Health check
curl http://localhost:8080/health

# List models
curl -H "Authorization: Bearer your-secret-key" http://localhost:8080/v1/models

# Chat completion
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer your-secret-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

## Configuration

The proxy uses a subset of LiteLLM’s YAML configuration format. Configure explicit client-facing model aliases; this does not import every LiteLLM feature or wildcard route:

```yaml
model_list:
  # OpenAI models
  - model_name: gpt-4                    # User-facing name
    litellm_params:
      model: openai/gpt-4                # provider/model format
      api_key: os.environ/OPENAI_API_KEY # Env var expansion
    rpm: 100                             # Optional rate limit

  # Anthropic models
  - model_name: claude-3-sonnet
    litellm_params:
      model: anthropic/claude-3-5-sonnet-20240620
      api_key: os.environ/ANTHROPIC_API_KEY
    rpm: 50

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

### Environment Variable Expansion

Use `os.environ/VAR_NAME` syntax to reference environment variables:

```yaml
api_key: os.environ/OPENAI_API_KEY
master_key: os.environ/PROXY_MASTER_KEY
```

### OpenTelemetry traces

Tracing is disabled by default. To export HTTP and LLM operation spans to an
OTLP collector, add:

```yaml
otel_settings:
  enabled: true
  service_name: simple-llm-proxy
  sampling_ratio: 1.0                 # 0.0–1.0; parent-based sampling
  exporter:
    protocol: http                    # http or grpc
    endpoint: https://collector.example.com:4318
    headers:
      Authorization: os.environ/OTEL_AUTH_HEADER
```

For OTLP/HTTP, a bare host endpoint sends to `/v1/traces`; an endpoint with a
path uses that path as-is. OTLP/gRPC endpoints use a host URL without a path.
For a local plaintext collector, use an `http://` endpoint and set
`insecure: true`. Header values support `os.environ/VAR_NAME` expansion.
The child LLM span covers retries and records failed attempts as events;
request bodies, API key values, and key hashes are never attached to spans.

### Multiple Deployments

You can configure multiple deployments for the same model name for load balancing:

```yaml
model_list:
  - model_name: gpt-4
    litellm_params:
      model: openai/gpt-4
      api_key: os.environ/OPENAI_API_KEY_1

  - model_name: gpt-4
    litellm_params:
      model: openai/gpt-4
      api_key: os.environ/OPENAI_API_KEY_2
```

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Health check (no auth required) |
| GET | `/openapi.json` | OpenAPI 3.0 specification (no auth required) |
| GET | `/v1/models` | List available models |
| POST | `/v1/chat/completions` | Chat completions (streaming supported) |
| POST | `/v1/embeddings` | Generate embeddings (compatible providers) |

### Authentication

All `/v1/*` endpoints require authentication via bearer token:

```bash
curl -H "Authorization: Bearer your-app-key" http://localhost:8080/v1/models
```

### Streaming

Request streaming by setting `"stream": true`:

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "Count to 10"}],
    "stream": true
  }'
```

### OpenAPI Specification

The proxy serves a dynamic OpenAPI 3.0 specification at `/openapi.json`:

```bash
curl http://localhost:8080/openapi.json | jq .
```

The spec describes the API surface; consult the pilot guide for compatibility boundaries. You can use it with tools like Swagger UI or import it into API clients.

## Supported Providers

### OpenAI

Supported paths:
- Chat completions (`/v1/chat/completions`)
- Embeddings (`/v1/embeddings`)
- Streaming

### Anthropic

Supported paths:
- Chat completions (automatically translated from OpenAI format)
- Streaming
- Tool/function calling

The proxy automatically:
- Extracts system messages into Anthropic's `system` parameter
- Sets `max_tokens` default (4096) when not specified
- Translates tool calls between formats
- Converts stop reasons (`end_turn` → `stop`, etc.)

OpenRouter and Ollama use their OpenAI-compatible endpoints. Ollama `api_base` must include `/v1`. Anthropic requests are translated: common chat and tools are supported, but arbitrary provider-specific options are not passed through.

## Load Balancing

### Routing Strategies

**simple-shuffle** (default): Randomly selects from healthy deployments.

**round-robin**: Cycles through deployments in order.

Deployment `rpm` limits count upstream attempts in a process-local minute window. Chat and embedding `tpm` limits reserve estimated input plus maximum output and reconcile reported usage. Estimates are conservative for text and are not tokenizer or multimodal accounting. Quotas are not shared across proxy replicas; Responses TPM reservations are not covered by this migration.

### Cooldown

When a deployment fails `allowed_fails` times, it enters cooldown for `cooldown_time` and won't receive requests until the cooldown expires.

## Development

```bash
# Run tests
just test

# Run tests with coverage
just test-coverage

# Format code
just fmt

# Build for all platforms
just build-all
```

## Project Structure

```
simple_llm_proxy/
├── cmd/proxy/main.go           # Application entry point
├── internal/
│   ├── api/
│   │   ├── handler/            # HTTP request handlers
│   │   ├── middleware/         # Auth, logging, recovery
│   │   └── router.go           # Chi router setup
│   ├── config/                 # YAML config parsing
│   ├── model/                  # Request/response types
│   ├── openapi/                # OpenAPI 3.0 spec builder
│   ├── provider/
│   │   ├── openai/             # OpenAI provider
│   │   └── anthropic/          # Anthropic provider
│   ├── router/                 # Load balancing logic
│   └── storage/sqlite/         # SQLite storage
├── config.yaml.example
├── justfile
└── go.mod
```

## License

MIT
