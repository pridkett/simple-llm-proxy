# ADR 007: Provider Expansion Architecture

**Status:** Accepted
**Date:** 2026-03-31
**ADR Issue:** pridkett/simple-llm-proxy#36

---

## Context

Phase 6 expands the proxy from 2 providers (OpenAI, Anthropic) to 7 by adding OpenRouter, Ollama, vLLM, MiniMax, and Gemini AI Studio. The current architecture has two independent provider implementations: OpenAI (~230 lines of HTTP/SSE logic) and Anthropic (~540 lines with full request/response translation). Adding five new providers would require duplicating the OpenAI implementation five times — four of the five new providers are OpenAI-compatible and share identical HTTP request/response/streaming patterns, differing only in authentication, base URL, and minor response quirks.

The current `Factory` type (`func(apiKey, apiBase string) Provider`) is insufficient. OpenRouter requires extra HTTP headers for app attribution. Ollama needs optional authentication (no-op when running locally). Gemini requires safety settings configuration. MiniMax needs an XML tool-call toggle. The two-parameter factory cannot express any of these.

This ADR documents the eight architectural decisions that govern Phase 6 implementation: openaicompat base extraction, hook-based extension model, Factory signature change, provider package layout, Gemini translation layer, MiniMax XML tool-call parsing, Ollama optional authentication, and config extension for provider-specific options.

### Provider landscape

| Provider | API Compatibility | Auth Model | Key Differences |
|----------|-------------------|------------|-----------------|
| OpenAI | Native OpenAI | Bearer token | Reference implementation |
| Anthropic | Native Anthropic | x-api-key header | Full translation layer (existing) |
| OpenRouter | OpenAI-compatible | Bearer token | Extra headers for attribution; SSE keepalive comments |
| Ollama | OpenAI-compatible | Optional Bearer | No auth by default (local deployment); localhost:11434 |
| vLLM | OpenAI-compatible | Bearer token | Self-hosted; configurable base URL; no default endpoint |
| MiniMax | OpenAI-compatible | Bearer token | XML tool-call blocks in content for self-hosted models |
| Gemini AI Studio | Native Gemini (GenerateContent) | x-goog-api-key header | Full translation required; v1beta API; SAFETY finish reason |

---

## Decision

### D-01: openaicompat.BaseProvider Extraction

**Problem:** Five providers share identical OpenAI-compatible request/response/streaming patterns. Copy-pasting the OpenAI provider five times creates a maintenance burden — every bug fix to SSE parsing, 429 handling, or error parsing must be replicated.

**Decision:** Extract shared HTTP request/response/streaming logic from `internal/provider/openai/openai.go` into a new `internal/provider/openaicompat` package with a `BaseProvider` struct. The `BaseProvider` implements the full `provider.Provider` interface: `ChatCompletion`, `ChatCompletionStream`, `Embeddings`, `SupportsEmbeddings`, and `Name`. The existing OpenAI provider is refactored to embed `BaseProvider` as a thin wrapper (~30-40 lines).

The extraction covers:
- JSON request marshaling and HTTP POST execution
- Response body reading and status code checking
- HTTP 429 detection with `provider.RateLimitError` and `provider.ParseRetryAfter`
- OpenAI-format error response parsing (`model.APIError`)
- SSE line-by-line stream parsing via `bufio.NewReader`
- `[DONE]` sentinel detection
- Channel-based stream delivery via `provider.NewStream`
- Embeddings endpoint support

**Consequence:** One implementation of SSE parsing, 429 handling, error parsing, and request marshaling. All OpenAI-compatible providers inherit fixes and improvements automatically. The extraction is a refactor of existing code — no new functionality is added, just reorganization.

### D-02: Hook-Based Extension Model

**Problem:** Each OpenAI-compatible provider has small differences: auth headers, response post-processing, error format, stream termination. A pure inheritance model would require method overrides for each variation, defeating the purpose of the shared base.

**Decision:** `BaseProvider` exposes six configurable hook points, all optional (nil = no-op or sensible default):

| Hook | Type | Purpose | Default |
|------|------|---------|---------|
| `Auth` | `func(req *http.Request)` | Sets auth headers/params on outgoing requests | nil (no auth) |
| `TransformResponse` | `func(resp *ChatCompletionResponse) *ChatCompletionResponse` | Post-processes non-streaming responses | nil (pass-through) |
| `TransformStreamChunk` | `func(chunk *StreamChunk) *StreamChunk` | Post-processes streaming chunks | nil (pass-through) |
| `ParseError` | `func(statusCode int, body []byte) error` | Custom error response parsing | nil (default OpenAI-format parsing) |
| `DoneSentinel` | `string` | SSE stream termination marker | `"[DONE]"` |
| `ProviderName` | `string` | Name for errors/logs | Required (no default) |

Providers configure hooks in their constructor. No method overrides are needed. Each new OpenAI-compatible provider is 20-40 lines of constructor code that sets the relevant hooks and calls `provider.Register()` in `init()`.

**Example usage:**

```go
// OpenRouter: Bearer auth + extra headers
Auth: func(req *http.Request) {
    req.Header.Set("Authorization", "Bearer "+opts.APIKey)
},
ExtraHeaders: opts.ExtraHeaders, // HTTP-Referer, X-Title

// MiniMax: Bearer auth + XML tool-call transform
TransformResponse: minimaxTransformResponse, // XML parsing hook

// Ollama: conditional auth
Auth: func(req *http.Request) {
    if opts.APIKey != "" {
        req.Header.Set("Authorization", "Bearer "+opts.APIKey)
    }
},
```

**Consequence:** The hook model provides adequate extensibility for all known provider variations without requiring any method overrides. Future providers with OpenAI-compatible APIs can be added by writing a constructor function only.

### D-03: ProviderOptions and Factory Signature Change

**Problem:** The current `Factory` type `func(apiKey, apiBase string) Provider` cannot pass extra headers (OpenRouter attribution), optional auth (Ollama), safety settings (Gemini), or the XML toggle (MiniMax). Adding more positional parameters would be unwieldy and fragile.

**Decision:** Introduce a `ProviderOptions` struct in `internal/provider/provider.go`:

```go
type ProviderOptions struct {
    APIKey         string
    APIBase        string
    ExtraHeaders   map[string]string
    SafetySettings []SafetySetting  // Gemini only
    XMLToolCalls   *bool            // MiniMax: nil = default (enabled)
}

type SafetySetting struct {
    Category  string // e.g., "HARM_CATEGORY_HARASSMENT"
    Threshold string // e.g., "BLOCK_NONE"
}
```

Change the `Factory` type from `func(apiKey, apiBase string) Provider` to `func(opts ProviderOptions) Provider`. Update:

| File | Change |
|------|--------|
| `internal/provider/provider.go` | Add `ProviderOptions`, `SafetySetting` structs |
| `internal/provider/registry.go` | Update `Factory` type; change `Get()` to accept `ProviderOptions` |
| `internal/provider/openai/openai.go` | `New()` accepts `ProviderOptions` instead of `(apiKey, apiBase string)` |
| `internal/provider/anthropic/anthropic.go` | `New()` accepts `ProviderOptions` instead of `(apiKey, apiBase string)` |
| `internal/router/router.go` | `New()` and `Reload()` build `ProviderOptions` from config |
| Test files | Update mock provider registrations |

This is done as the first implementation plan before adding new providers. Clean foundation first.

**Consequence:** Breaking internal API change. All provider constructors, router wiring, and test mocks must update in one atomic change. No external API impact — the HTTP API surface is unchanged. The struct provides a natural extension point for future provider-specific options without further Factory signature changes.

### D-04: Provider Package Layout

**Decision:** Each provider gets its own package directory following the established codebase convention:

```
internal/provider/
  openaicompat/
    base.go                    # BaseProvider with configurable hooks
  openai/
    openai.go                  # Thin wrapper embedding BaseProvider
  anthropic/
    anthropic.go               # Standalone translation (existing, updated for new Factory)
  openrouter/
    openrouter.go              # Bearer auth, extra headers, openrouter.ai default
  ollama/
    ollama.go                  # Optional auth, localhost:11434 default
  vllm/
    vllm.go                    # Bearer auth, configurable base URL
  minimax/
    minimax.go                 # Bearer auth, XML tool-call TransformResponse
  gemini/
    gemini.go                  # Full translation layer, x-goog-api-key auth
```

Each provider registers via `init()` + `provider.Register()`. `cmd/proxy/main.go` adds blank imports:

```go
_ "github.com/pwagstro/simple_llm_proxy/internal/provider/openrouter"
_ "github.com/pwagstro/simple_llm_proxy/internal/provider/ollama"
_ "github.com/pwagstro/simple_llm_proxy/internal/provider/vllm"
_ "github.com/pwagstro/simple_llm_proxy/internal/provider/minimax"
_ "github.com/pwagstro/simple_llm_proxy/internal/provider/gemini"
```

**Consequence:** Follows the existing pattern established by `openai/` and `anthropic/`. Each provider is independently testable. Adding or removing a provider requires only adding/removing the blank import line in `main.go`.

### D-05: Gemini Translation Layer

**Problem:** Gemini's REST API (`GenerateContent`) uses fundamentally different request/response structures from OpenAI. The endpoint paths, request format, response format, streaming format, and authentication mechanism all differ. Forcing Gemini into the openaicompat base would add more complexity than it saves.

**Decision:** Gemini gets a standalone `Provider` struct (not embedding `BaseProvider`) in `internal/provider/gemini/` with full bidirectional translation, following the same pattern as the existing Anthropic provider:

**Request translation:**
- OpenAI `messages[]` -> Gemini `contents[]` with role mapping (`assistant` -> `model`)
- OpenAI system messages -> Gemini `systemInstruction` field
- OpenAI `tools[].function` -> Gemini `tools[].functionDeclarations[]`
- OpenAI tool result messages -> Gemini `functionResponse` parts
- `temperature`, `top_p`, `max_tokens` -> `generationConfig` fields

**Response translation:**
- Gemini `candidates[0].content.parts[]` -> OpenAI `choices[0].message`
- Gemini `functionCall` parts -> OpenAI `tool_calls` array
- Gemini `usageMetadata` -> OpenAI `usage` (promptTokenCount -> prompt_tokens, candidatesTokenCount -> completion_tokens)

**Finish reason mapping:**

| Gemini | OpenAI | Notes |
|--------|--------|-------|
| `STOP` | `stop` | Normal completion |
| `MAX_TOKENS` | `length` | Token limit reached |
| `SAFETY` | `content_filter` | Content blocked by safety filter |
| `RECITATION` | `stop` | Log warning |
| `MALFORMED_FUNCTION_CALL` | `stop` | Log warning |
| `OTHER` | `stop` | Default fallback |

**Streaming:** `POST /models/{model}:streamGenerateContent?alt=sse` endpoint. Each `data:` line contains a complete `GenerateContentResponse` JSON object. No `[DONE]` sentinel — stream ends on connection close (`io.EOF`).

**Authentication:** `x-goog-api-key` header (not Bearer token, not query parameter).

**API version:** `v1beta` at `https://generativelanguage.googleapis.com/v1beta`. Despite the "beta" label, this is the production-recommended path for AI Studio with the latest features including tool use and system instructions.

**Safety settings:** Configurable via `ProviderOptions.SafetySettings`. When populated, the `safetySettings` array is included in every request. When empty, Gemini's defaults apply.

**Embeddings:** Not supported. `SupportsEmbeddings()` returns `false`. Gemini's embedding API uses a different endpoint format and is deferred.

**Consequence:** ~300+ line provider file, similar in structure and complexity to the Anthropic provider. The standalone implementation avoids forcing Gemini's translation logic into the openaicompat framework where it does not fit.

### D-06: MiniMax XML Tool-Call Parsing

**Problem:** When MiniMax models are self-hosted via vLLM/SGLang without the tool-call parser configured, tool calls are returned as XML blocks in the content string rather than in the standard `tool_calls` array. The hosted `api.minimax.io` API returns standard OpenAI `tool_calls`, but the XML parser is needed as a safety net for self-hosted deployments.

**Decision:** MiniMax provider uses the `BaseProvider` `TransformResponse` hook with a parser that:

1. Detects `<minimax:tool_call>` blocks via regex (`(?s)<minimax:tool_call>(.*?)</minimax:tool_call>`)
2. Skips parsing when the `tool_calls` array is already populated (hosted API case)
3. Parses XML using `encoding/xml` into invoke elements with name and parameters:
   ```xml
   <minimax:tool_call>
     <invoke name="function_name">
       <parameter name="param_key">param_value</parameter>
     </invoke>
   </minimax:tool_call>
   ```
4. Builds `model.ToolCall` array from parsed invocations (IDs generated as `call_0`, `call_1`, etc.)
5. Strips XML from the content string
6. Fixes `finish_reason` from `"stop"` to `"tool_calls"` when XML tool calls are detected

**Toggle:** Controlled by `ProviderOptions.XMLToolCalls` (`*bool`). Default behavior (nil): enabled. Operators can set `xml_tool_calls: false` in config to disable if MiniMax fixes their API.

**Embeddings:** Supported since MiniMax's API is OpenAI-compatible.

**Streaming:** `TransformStreamChunk` is nil for MiniMax initially. Streaming XML tool-call behavior is deferred per CONTEXT.md decision.

**Consequence:** The parser is defensive — it runs only when XML is detected and tool_calls is empty. False positives are prevented by the dual-check. The `encoding/xml` stdlib parser handles the nested structure reliably.

### D-07: Ollama Optional Authentication

**Problem:** Ollama is typically deployed locally without authentication. The proxy's Factory currently requires an API key, but forcing a dummy key for Ollama is poor UX.

**Decision:** Ollama's `AuthFunc` is a no-op when `APIKey` is empty. When `APIKey` is provided, it sends a standard `Authorization: Bearer` token. This matches Ollama's typical deployment:

```go
Auth: func(req *http.Request) {
    if opts.APIKey != "" {
        req.Header.Set("Authorization", "Bearer "+opts.APIKey)
    }
},
```

Default base URL: `http://localhost:11434/v1` (overridable via `APIBase`).

**Consequence:** Ollama can be configured in YAML with `api_key: ""` or with `api_key` omitted entirely. The proxy does not require an API key for local Ollama deployments.

### D-08: Config Extension for Provider-Specific Options

**Problem:** The current `LiteLLMParams` struct has only `Model`, `APIKey`, and `APIBase`. There is no mechanism to pass provider-specific options (Gemini safety settings, MiniMax XML toggle, OpenRouter extra headers) through the config file.

**Decision:** Extend `LiteLLMParams` with two new fields:

```go
type LiteLLMParams struct {
    Model        string            `yaml:"model"`
    APIKey       string            `yaml:"api_key"`
    APIBase      string            `yaml:"api_base,omitempty"`
    ExtraHeaders map[string]string `yaml:"extra_headers,omitempty"`
    ExtraParams  map[string]any    `yaml:"extra_params,omitempty"`
}
```

The router parses `ExtraParams` to populate `ProviderOptions` fields:
- `extra_params.safety_settings` -> `ProviderOptions.SafetySettings` (Gemini)
- `extra_params.xml_tool_calls` -> `ProviderOptions.XMLToolCalls` (MiniMax)

Environment variable expansion (`os.environ/VAR_NAME`) applies to `extra_headers` values.

**YAML example:**

```yaml
model_list:
  - model_name: gemini-pro
    litellm_params:
      model: gemini/gemini-2.0-flash
      api_key: os.environ/GOOGLE_API_KEY
      extra_params:
        safety_settings:
          - category: HARM_CATEGORY_HARASSMENT
            threshold: BLOCK_NONE

  - model_name: openrouter-claude
    litellm_params:
      model: openrouter/anthropic/claude-3.5-sonnet
      api_key: os.environ/OPENROUTER_API_KEY
      extra_headers:
        HTTP-Referer: "https://myapp.example.com"
        X-Title: "My App"

  - model_name: local-llama
    litellm_params:
      model: ollama/llama3
      # api_key omitted — Ollama no-auth mode
```

**Alternatives considered:**
- Provider-specific config sections: rejected as incompatible with LiteLLM YAML format
- Flat fields on `LiteLLMParams`: rejected as not extensible for future providers
- Generic `map[string]any` only: chosen as the extension mechanism, with typed extraction in the router

**Consequence:** The config format remains LiteLLM-compatible. Provider-specific options are passed through the generic `extra_params` map, with typed extraction happening in the router layer. No config schema changes are needed for future providers — they can read from `extra_params` directly.

---

## Alternatives Considered

### Monolithic provider file

**Rejected.** Putting all OpenAI-compatible providers in one large file would reduce the number of files but make the code harder to navigate, test, and maintain. The package-per-provider pattern is established in the codebase and matches Go conventions.

### Interface-based extension (method overrides) instead of hooks

**Rejected.** Defining an interface with methods like `SetAuth(req)`, `TransformResponse(resp)`, etc. and requiring each provider to implement it would be more Go-idiomatic in some ways, but it forces every provider to implement every method (even as no-ops). The hook/callback model lets providers set only what they need and leaves everything else as nil (default behavior). For thin wrappers that differ in only 1-2 ways from the base, hooks are cleaner.

### Gemini via openaicompat with heavy overrides

**Rejected.** Gemini's API differs in: endpoint path format (`/models/{model}:generateContent` vs `/chat/completions`), request structure (contents/parts vs messages), response structure (candidates/parts vs choices), streaming query parameter (`?alt=sse`), authentication header name, and error format. Overriding these many aspects of `BaseProvider` would effectively rewrite the base, adding indirection without reducing code.

### Adding provider-specific config types to the top-level Config struct

**Rejected.** Adding `GeminiSettings`, `MinimaxSettings`, etc. at the `Config` struct level would break LiteLLM YAML compatibility and create coupling between the config layer and provider implementations. The `extra_params` map preserves flexibility and keeps provider-specific knowledge in the provider layer.

---

## Consequences

- **After D-01/D-02:** The `openaicompat.BaseProvider` becomes the single source of truth for OpenAI-compatible HTTP/SSE logic. Bug fixes to SSE parsing or 429 handling are automatically inherited by all five OpenAI-compatible providers (OpenAI, OpenRouter, Ollama, vLLM, MiniMax). The OpenAI provider is reduced from ~230 lines to ~30-40 lines.

- **After D-03:** The `ProviderOptions` struct replaces the two-parameter Factory. This is a one-time breaking internal change that touches the registry, router, and all existing providers. After this change, adding new provider-specific options requires only adding a field to `ProviderOptions` — no further signature changes.

- **After D-04:** Seven provider packages exist under `internal/provider/`. Each is independently testable. Provider addition/removal is a matter of adding/removing a package and a blank import.

- **After D-05:** Gemini AI Studio is accessible through the same OpenAI-compatible API surface as all other providers. Clients make standard `/v1/chat/completions` requests. The proxy handles all translation transparently.

- **After D-06:** Self-hosted MiniMax models that emit XML tool calls are handled correctly. The parser is defensive and disabled when the API already provides standard `tool_calls`.

- **After D-07:** Ollama works out of the box for local deployments without requiring a dummy API key.

- **After D-08:** Provider-specific configuration flows through the established YAML format without breaking LiteLLM compatibility. New providers can read from `extra_params` without config schema changes.

- **No new external dependencies.** All provider implementations use Go standard library only (`net/http`, `encoding/json`, `encoding/xml`, `bufio`).

- **Phase 7+ impact:** The provider pool routing (Phase 7) and per-provider budget enforcement (Phase 8) work against the `Provider` interface. These phases are unaffected by the internal refactoring — they see the same `Provider` interface with the same methods.

---

## Implementation Files

| File | Role |
|------|------|
| `internal/provider/provider.go` | Add `ProviderOptions`, `SafetySetting` structs (D-03) |
| `internal/provider/registry.go` | Update `Factory` type and `Get()` signature (D-03) |
| `internal/provider/openaicompat/base.go` | New: `BaseProvider` with hooks (D-01, D-02) |
| `internal/provider/openai/openai.go` | Refactor to embed `BaseProvider` (D-01) |
| `internal/provider/anthropic/anthropic.go` | Update constructor for `ProviderOptions` (D-03) |
| `internal/provider/openrouter/openrouter.go` | New: Bearer auth + extra headers (D-04) |
| `internal/provider/ollama/ollama.go` | New: optional auth, localhost default (D-07) |
| `internal/provider/vllm/vllm.go` | New: Bearer auth, configurable base (D-04) |
| `internal/provider/minimax/minimax.go` | New: XML tool-call TransformResponse (D-06) |
| `internal/provider/gemini/gemini.go` | New: full translation layer (D-05) |
| `internal/config/config.go` | Extend `LiteLLMParams` with `ExtraHeaders`, `ExtraParams` (D-08) |
| `internal/config/loader.go` | Parse `extra_headers`, `extra_params` from YAML (D-08) |
| `internal/router/router.go` | Build `ProviderOptions` from config; pass to `provider.Get()` (D-03) |
| `cmd/proxy/main.go` | Add blank imports for new providers (D-04) |

---

## References

- `.planning/phases/06-provider-expansion/06-CONTEXT.md` — All user decisions D-01 through D-19
- `.planning/phases/06-provider-expansion/06-RESEARCH.md` — Provider API details, XML format, pitfalls
- `adr/006-streaming-backoff.md` — Phase 5 decisions; RateLimitError contract that all new providers must follow
- `adr/005-schema-config-foundation.md` — Schema decisions; DeploymentKey format used by all providers
- `internal/provider/openai/openai.go` — Source implementation for BaseProvider extraction
- `internal/provider/anthropic/anthropic.go` — Pattern reference for Gemini translation layer
- [Gemini API reference](https://ai.google.dev/api/generate-content) — GenerateContent endpoint and response format
- [OpenRouter API reference](https://openrouter.ai/docs/api/reference/overview) — Auth, streaming, extra headers
- [Ollama OpenAI compatibility](https://docs.ollama.com/api/openai-compatibility) — Base URL, auth behavior
- [MiniMax OpenAI API](https://platform.minimax.io/docs/api-reference/text-openai-api) — Base URL, auth
- [MiniMax tool calling guide](https://github.com/MiniMax-AI/MiniMax-M2.5/blob/main/docs/tool_calling_guide.md) — XML format
- [vLLM OpenAI-compatible server](https://docs.vllm.ai/en/stable/serving/openai_compatible_server/) — Streaming, auth
