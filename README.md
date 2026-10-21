# Simple LLM Proxy

A Go proxy for apps using OpenAI-compatible chat completions or Anthropic-compatible Messages. Keep upstream OpenAI, OpenRouter, Anthropic, and Ollama credentials in one service, and issue separate keys to client apps.

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
  database_url: ./proxy.db               # SQLite path, or a PostgreSQL URL
  port: 8080
```

### Environment Variable Expansion

Use `os.environ/VAR_NAME` syntax to reference environment variables:

```yaml
api_key: os.environ/OPENAI_API_KEY
master_key: os.environ/PROXY_MASTER_KEY
```

### PostgreSQL storage

SQLite remains the default. To use PostgreSQL 17 or newer, create a dedicated
database owned by the proxy's database role and configure:

```yaml
general_settings:
  database_url: os.environ/PROXY_DATABASE_URL
```

Set `PROXY_DATABASE_URL` in the proxy process's environment, for example
`postgresql://proxy:URL_ENCODED_PASSWORD@postgres.home:5432/llm_proxy?sslmode=verify-full`.
Configure a trusted CA with `sslrootcert` when required. Both `postgres://` and
`postgresql://` are accepted. The role needs schema creation/migration permissions;
tables and indexes are created automatically at startup. Connection and migration
failures stop startup. Storage changes require a restart.

PostgreSQL stores the same data as SQLite, including login sessions, usage logs,
configured body snippets, and background Responses jobs. It uses a pool of up
to 10 connections per process, with 5 idle connections and a 30-minute connection
lifetime. Keep this within the database's connection budget. For an encrypted
home-network connection, prefer `sslmode=verify-full`; `sslmode=disable` is an
explicit option for trusted development networks. Keep database credentials in
your environment/secret manager and include them in container environment
configuration if using Docker.

**Switching backends does not copy data.** Back up the SQLite database and stop
the proxy before changing the URL. PostgreSQL starts empty: sign in again and
recreate teams, applications, and API keys. If you need existing identities,
keys, history, or pending jobs, keep SQLite until you have a separately verified
data-import process. No automatic importer is provided. Verify login, key
creation, a request log, and spend after cutover. To roll back, stop the proxy,
restore the SQLite URL, and restart; PostgreSQL-only writes will not be copied
back. Operate and test PostgreSQL backups/restores separately from proxy backups.

PostgreSQL allows concurrent database operations and independent database
management. It does **not** yet make running multiple proxy replicas safe:
budgets, rate limits, caches, and worker coordination still have process-local
state. Full request/response archival is also separate from existing bounded
snippets. See [ADR 014](adr/014-postgresql-storage.md) for the design and limits.

To run integration tests against a dedicated PostgreSQL test database:

```sh
TEST_POSTGRES_URL='postgresql://proxy_test:password@localhost:5432/proxy_test?sslmode=disable' go test -race ./...
```

Tests create and remove isolated schemas within that database. CI runs them
against PostgreSQL 17; local runs skip PostgreSQL integration tests when the
variable is unset.

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
| POST | `/v1/messages` | Anthropic-compatible Messages to any configured chat model (streaming supported) |
| POST | `/v1/embeddings` | Generate embeddings (compatible providers) |

### Authentication

All `/v1/*` endpoints accept authentication via bearer token. `/v1/messages` also accepts proxy keys in `x-api-key` for Anthropic SDK compatibility. An explicit `Authorization` header takes precedence:

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

### Anthropic Messages API

Use `POST /v1/messages` with any configured `model_name`. The requested name
selects the existing route, so an Anthropic SDK can call OpenAI, OpenRouter,
Ollama, Anthropic, or another registered chat provider through the same endpoint.
Application key permissions, rate limits, budgets, failover, session logging,
and cost tracking apply as they do for chat completions.

```bash
curl -N http://localhost:8080/v1/messages \
  -H "x-api-key: your-app-key" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "max_tokens": 128,
    "system": "Be concise.",
    "messages": [{"role": "user", "content": "Count to 10"}],
    "stream": true
  }'
```

With the Python `anthropic` SDK, set `base_url` to the proxy root, without `/v1`:

```python
from anthropic import Anthropic

client = Anthropic(api_key="your-app-key", base_url="http://localhost:8080")
with client.messages.stream(
    model="gpt-4",  # Any configured proxy model_name
    max_tokens=128,
    messages=[{"role": "user", "content": "Hello!"}],
) as stream:
    for text in stream.text_stream:
        print(text, end="", flush=True)
    message = stream.get_final_message()
    print(message.usage)
```

Use `client.messages.create(...)` for a non-streaming Message response.
Streaming returns named Anthropic events, including incremental text and tool
JSON deltas, and ends with `message_stop`. An upstream failure emits an `error`
event. Responses use the requested model alias; `X-Provider-Used` identifies
the actual deployment. `request-id` and `X-Request-ID` carry the same correlation ID.

Supported portable fields include string or text-block system prompts, text,
base64/URL user images, client tool schemas, assistant `tool_use`, user
`tool_result` with string/text content, `tool_choice` (`auto`, `any`, `tool`,
`none`), `disable_parallel_tool_use`, `max_tokens`, `temperature`, `top_p`,
`stop_sequences`, and `metadata.user_id`. Tool errors are conveyed to the model
with a `Tool error:` prefix. Vision and tool use require a capable target model.

This endpoint implements the portable Messages subset. Unsupported fields and
content types return HTTP 400, including extended thinking, `top_k`, server
tools, documents, and beta-only options. Token counting, batches, and files
endpoints are not implemented. Cache-control hints are accepted but do not
guarantee provider caching. A matched `stop_sequence` is returned only when the
upstream reports it; other providers may report `end_turn` instead. Usage comes
from the upstream; initial streaming usage may be zero until final counts arrive.
If the upstream omits usage, response counts are zero and stored usage remains
unknown. See [ADR 014](adr/014-anthropic-messages-api.md) for the design and plan.

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

## Team administration

The interface follows your browser's light or dark color preference automatically,
including changes while the page is open.

Use **Teams** to create a team by name, then manage its applications, API keys,
and members in one workspace. Creating an application automatically creates a
**Default** API key and displays it once for copying. Keys allow all models with
no rate or budget limits unless you set them in key settings.

Breadcrumbs and shareable URLs preserve your location within a team. Existing
`/#/applications` and `/#/keys?team_id=...&app_id=...` links redirect into the
workspace. If first-key creation fails, the application remains available and
the form offers a retry without creating another application. Team, application,
key, and membership changes remain restricted to system administrators.

## Session logs

Group requests from a story playthrough, conversation, or agent run by sending
one unique session ID on every related request. Chat completions, embeddings,
and Responses requests (including streams and background jobs) support:

```python
client.chat.completions.create(
    model="your-model",
    messages=[{"role": "user", "content": "Continue the story"}],
    extra_body={"session_id": "thecanyon:playthrough-123"},
)
```

You can instead send `X-Session-ID: thecanyon:playthrough-123` or use LiteLLM's
`litellm_session_id` at the top level or inside `metadata`. `metadata.session_id`
is also accepted. Precedence is header, top-level `session_id`, top-level
`litellm_session_id`, `metadata.session_id`, then `metadata.litellm_session_id`;
blank and null values are skipped. IDs must be strings of at most 256 UTF-8 bytes without
control characters; surrounding whitespace is trimmed. Invalid values return 400.
Session fields are consumed by the proxy; other metadata is preserved upstream.

In **Logs**, expand a request and click **Session ID** under **Attribution** to
view all retained requests with that ID across dates, models, providers, and
keys. The individual requests remain expandable and paginated. You can also
paste an exact ID into the session filter, combine it with existing filters,
and share the `/#/logs?session_id=...` URL. **Clear session** removes only the
session filter; **Clear Filters** resets everything. The admin API accepts
`GET /admin/logs?session_id=...` and includes `session_id` in list and detail
responses.

Session IDs are stored independently of request body capture. Existing logs
remain ungrouped; sessions obey the normal log retention policy. Use globally
unique IDs (for example, an application prefix plus a UUID): reusing an ID
combines its requests, even across keys. Session attribution does not change
routing affinity or infer sessions from `user`, message contents, or
`previous_response_id`; clients must supply the ID on every request.
