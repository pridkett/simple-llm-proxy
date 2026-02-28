# Simple LLM Proxy

A lightweight Go-based LLM proxy server inspired by LiteLLM, providing OpenAI-compatible endpoints with multi-provider support.

## Features

- **OpenAI-compatible API** - Drop-in replacement for OpenAI API clients
- **Multi-provider support** - Route requests to OpenAI or Anthropic
- **Automatic message translation** - Converts OpenAI format to Anthropic format transparently
- **Streaming support** - Full SSE streaming for both providers
- **Load balancing** - Simple shuffle or round-robin routing strategies
- **Failover & retries** - Automatic retry with cooldown for failed deployments
- **LiteLLM-compatible config** - Uses familiar `model_list` YAML format with `os.environ/` expansion
- **Usage logging** - SQLite-based request tracking
- **Master key auth** - Simple bearer token authentication
- **OpenAPI specification** - Dynamic OpenAPI 3.0 spec available at `/openapi.json`

## Installation

### From Source

```bash
git clone https://github.com/pwagstro/simple_llm_proxy.git
cd simple_llm_proxy
make build
```

### Dependencies

- Go 1.21+
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
make run
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

The proxy uses a LiteLLM-compatible YAML configuration format:

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
| POST | `/v1/embeddings` | Generate embeddings (OpenAI only) |

### Authentication

All `/v1/*` endpoints require authentication via bearer token:

```bash
curl -H "Authorization: Bearer your-master-key" http://localhost:8080/v1/models
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

The spec documents all endpoints, request/response schemas, and authentication requirements. You can use it with tools like Swagger UI or import it into API clients.

## Supported Providers

### OpenAI

Full support for:
- Chat completions (`/v1/chat/completions`)
- Embeddings (`/v1/embeddings`)
- Streaming

### Anthropic

Full support for:
- Chat completions (automatically translated from OpenAI format)
- Streaming
- Tool/function calling

The proxy automatically:
- Extracts system messages into Anthropic's `system` parameter
- Sets `max_tokens` default (4096) when not specified
- Translates tool calls between formats
- Converts stop reasons (`end_turn` → `stop`, etc.)

## Load Balancing

### Routing Strategies

**simple-shuffle** (default): Randomly selects from healthy deployments.

**round-robin**: Cycles through deployments in order.

### Cooldown

When a deployment fails `allowed_fails` times, it enters cooldown for `cooldown_time` and won't receive requests until the cooldown expires.

## Development

```bash
# Run tests
make test

# Run tests with coverage
make test-coverage

# Format code
make fmt

# Build for all platforms
make build-all
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
├── Makefile
└── go.mod
```

## License

MIT
