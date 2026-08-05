# ADR 011: OpenTelemetry Monitoring for LLM Requests

**Status:** Proposed
**Date:** 2026-08-05
**ADR Issue:** pridkett/simple-llm-proxy#30

---

## Context

Issue #30 asks for optional OpenTelemetry (OTEL) export of LLM request telemetry, compatible with standard OTEL collectors and LLM-observability backends such as Langfuse. The request specifically calls out enriching spans with proxy-specific metadata: project (team), application, the API key used, the host that served the request, and total cost.

The proxy has no tracing today. `context.Context` already threads through the request lifecycle (`internal/api/router.go`, `internal/api/middleware/`), but the only request-scoped values are request ID, API key, and user session (`middleware.RequestIDFromContext`, `middleware.APIKeyFromContext`, `middleware.UserFromContext`). There is no span, no trace ID, and no OTEL dependency in `go.mod`.

Three existing pieces of infrastructure are directly relevant:

1. **`middleware.RequestID()`** (`internal/api/middleware/requestid.go`) and **`middleware.Logging()`** (`internal/api/middleware/logging.go`) wrap every request and already capture the fields (status, duration, size) a root span needs.
2. **Auth/identity** (ADR 003, ADR 004): `KeyAuth` middleware injects a `*keystore.CachedKey` (wrapping `storage.APIKey`, which has `ApplicationID`) into context. `storage.Application` → `storage.Team` gives the project/application hierarchy the issue asks for. Master-key requests inject no key, so they have no project/application attribution — this is an existing gap (ADR 004 D-5), not something this ADR can close.
3. **Cost tracking** (ADR 002): `costmap.Manager.GetEffectiveSpec()` is called today only inside `handler.logRequest`, which runs as a detached goroutine (`go logRequest(...)` in `internal/api/handler/chat.go`) after both non-streaming responses and the `io.EOF` branch of streaming responses. The lookup itself is a cheap in-memory map read, not I/O — it is only wrapped in a goroutine because `logRequest` also does DB writes and webhook emission, which are correctly async.

The project already has one async background-delivery component to model against: the `WebhookDispatcher` from ADR 008 (buffered channel, background goroutine, `New() → Start(ctx) → defer Close()` lifecycle, drain-on-shutdown). OTEL's own SDK provides an equivalent primitive (`BatchSpanProcessor`), so this ADR reuses the *lifecycle pattern* from ADR 008 without reimplementing batching/export by hand.

---

## Decision

### D-01: Use the OpenTelemetry Go SDK with an OTLP Exporter, Not a Custom Client

**Decision:** Depend directly on `go.opentelemetry.io/otel` and its OTLP exporters (`otlptracehttp` and `otlptracegrpc`) rather than writing a bespoke span/export format. This is what "standard OTEL" in issue #30 means in practice, and it is what makes the feature Langfuse-compatible for free — Langfuse (and any other OTEL collector) accepts OTLP directly, so there is no proxy-specific integration code to write or maintain per backend.

**Rejected alternative:** A proxy-specific "monitoring events" system (à la the webhook `NotificationEvent`) that gets translated to OTLP at the edge. Rejected because it duplicates work the OTEL SDK already does correctly (batching, retries, protobuf/gRPC encoding) and would need a translation layer per backend instead of relying on OTLP being universally understood.

### D-02: Traces Only for v1 — No Metrics or Logs Pipeline

**Decision:** This ADR scopes the feature to **distributed tracing only**. Each LLM request produces a trace with a root HTTP span and a child provider-call span. OTEL metrics (e.g., request-duration histograms) and OTEL log export are explicitly out of scope.

**Rationale:** Traces are what LLM-observability backends (Langfuse, and the emerging GenAI semantic conventions) are built around — a trace with GenAI attributes is the unit Langfuse renders as a single "generation." The proxy already has metrics-shaped data available via `/admin/status` and structured logs via zerolog; duplicating those through OTEL metrics/logs is a separate, later decision if a specific backend needs it. Keeping v1 to traces keeps the config surface and SDK footprint small.

### D-03: GenAI Semantic Convention Attribute Names, Hand-Declared as Constants

**Decision:** Span attributes use the OpenTelemetry GenAI semantic convention names (namespace `gen_ai.*`), declared as local Go string constants in `internal/otel/semconv.go` rather than importing a generated semconv package. As of this ADR, the GenAI conventions are still marked "Development" in the OTEL spec and are not guaranteed to exist as stable generated constants across `go.opentelemetry.io/otel/semconv` versions; hand-declaring the current attribute names avoids a fragile dependency on an unstable package while still emitting spec-shaped data that Langfuse and other GenAI-aware backends recognize.

Attributes set on the provider-call child span:

| Attribute | Source |
|---|---|
| `gen_ai.system` | `d.ProviderName` (e.g. `"openai"`, `"anthropic"`) |
| `gen_ai.request.model` | requested model name (user-facing, from `model_list`) |
| `gen_ai.response.model` | `d.ActualModel` (provider-side model actually invoked) |
| `gen_ai.usage.input_tokens` | from response/stream usage |
| `gen_ai.usage.output_tokens` | from response/stream usage |
| `gen_ai.operation.name` | `"chat"` (fixed for `/v1/chat/completions`) |

Proxy-specific attributes that have no GenAI equivalent use an `llmproxy.*` namespace, set on the **root HTTP span** (available at auth time, before the provider is even selected) except where noted:

| Attribute | Source | Notes |
|---|---|---|
| `llmproxy.team` | `storage.Team.Name` via `CachedKey.Key.ApplicationID` → `Application.TeamID` lookup | Omitted for master-key requests (no `CachedKey`) |
| `llmproxy.application` | `storage.Application.Name` | Omitted for master-key requests |
| `llmproxy.api_key_name` | `storage.APIKey.Name` (never the raw key or hash) | Omitted for master-key requests |
| `llmproxy.deployment.host` | `provider.Deployment.APIBase` | Set on the child span once a deployment is routed |
| `llmproxy.pool_name` | `RouteResult.PoolName` | Set on the child span |
| `llmproxy.cost.total_usd` | `costmap.Manager.GetEffectiveSpec()` result | Set on the child span at the point the span ends (see D-05) |
| `llmproxy.request_id` | `middleware.RequestIDFromContext(ctx)` | Set on the root span for correlation with existing structured logs |

**Never included:** raw API key values, key hashes, or request/response body content. This mirrors the existing `RequestLog` design (ADR 002/004), which never persists secrets or payload bodies either.

### D-04: Two-Span Structure — Root HTTP Span + Child Provider Span

**Decision:** Tracing hooks into two existing points in the request path:

1. **Root HTTP span**, started in a new `middleware.OTel()` middleware inserted into the global chain immediately after `RequestID()` (`internal/api/router.go:30-38`), sibling to `Logging()`. It wraps `next.ServeHTTP` the same way `Logging()` does, and records the same fields (`http.status_code`, `http.route`, response size) as span attributes, plus `llmproxy.request_id` and the identity attributes from D-03 (identity is known by this point because `KeyAuth` runs before `/v1/*` handlers, but the OTel middleware sits in the global chain — it reads whatever identity is in context by the time the span closes, so identity attributes are attached at span-end, not span-start).
2. **Child provider-call span**, started inside `handler.ChatCompletions` around the closure passed to `r.Route(ctx, ...)` (`internal/api/handler/chat.go:129-138`) that calls `d.Provider.ChatCompletion(ctx, ...)` / `ChatCompletionStream(ctx, ...)`. This span naturally has `d.ProviderName`, `d.ActualModel`, and (after routing) `RouteResult.PoolName` and `Deployment.APIBase` available to set as attributes at creation time.

For streaming requests, both spans stay open for the full duration of the stream — the child span ends when the `io.EOF` branch of `handleStreamingResponse` (`chat.go:261-294`) is reached, and the root span ends when `ServeHTTP` returns, which for streaming handlers is after the stream completes. No change to the streaming code path's control flow is required; tracing wraps existing boundaries.

**Rejected alternative:** A single flat span per request. Rejected because it can't distinguish time spent in proxy-side routing/auth/logging from time spent waiting on the upstream provider, which is the single most useful thing a trace adds over the existing structured request log.

### D-05: Synchronous Cost Computation Before Span End, Decoupled From Async Persistence

**Problem:** `costmap.Manager.GetEffectiveSpec()` today runs only inside the detached `logRequest` goroutine (`chat.go`), which is the wrong place to source a span attribute — spans must be closed (`span.End()`) before or as the HTTP handler returns, and cannot have attributes added afterward.

**Decision:** Extract the cost lookup out of `logRequest` into a small synchronous helper (`computeCost(model, usage) float64`) called from `handleNonStreamingResponse` / `handleStreamingResponse` **before** the child span is ended and **before** `go logRequest(...)` is fired. The computed value is passed as a parameter into `logRequest` instead of being recomputed there, so there is exactly one call site for the lookup, not two.

This is safe because `GetEffectiveSpec()` is an in-memory map read (ADR 002) — no I/O, no meaningful latency added to the request path. Everything that *is* I/O bound (the SQLite write, webhook emission) stays exactly as async as it is today; this ADR does not change `logRequest`'s async design, only where the cost number is computed.

### D-06: `otel_settings` Config Block, Disabled by Default

**Decision:** A new top-level `otel_settings` block in `config.Config`, following the existing flat-typed-struct pattern (cf. `OIDCSettings`, `LogSettings`):

```yaml
otel_settings:
  enabled: false                 # default: off, zero SDK overhead when unset
  service_name: simple-llm-proxy # resource attribute service.name
  exporter:
    protocol: http                # http | grpc
    endpoint: https://otel.example.com:4318
    insecure: false
    headers:                      # supports os.environ/VAR expansion, same as webhook secrets
      Authorization: os.environ/OTEL_EXPORTER_AUTH_HEADER
  sampling_ratio: 1.0              # 0.0-1.0, parent-based ratio sampler
```

`enabled: false` is the default so that OTEL support is fully opt-in — no exporter is constructed, no background goroutine starts, and the middleware becomes a no-op pass-through when unset. `headers` uses the same `expandEnvVar()` mechanism `config/loader.go` already applies to webhook secrets and OIDC client secrets, since OTLP auth (e.g., Langfuse's basic-auth-via-header scheme) is typically a bearer/basic token that must not live in plaintext YAML.

### D-07: TracerProvider Lifecycle Mirrors the Webhook Dispatcher Pattern

**Decision:** An `internal/otel` package exposes `otel.NewProvider(ctx, cfg) (*otel.Provider, error)`, constructed in `cmd/proxy/main.go` right after storage init (so its own startup can be logged) and shut down via `defer provider.Shutdown(ctx)` immediately before `server.Shutdown(ctx)` (`main.go:346`) — the same position in the shutdown sequence the webhook dispatcher's `Close()` and the spend-flush ticker already occupy, ensuring buffered spans are flushed before the process exits.

Internally, `otel.Provider` wraps the standard SDK's `sdktrace.TracerProvider` configured with a `BatchSpanProcessor` wrapping the configured OTLP exporter — the SDK's own batching/retry logic is used as-is rather than reimplementing the `WebhookDispatcher`'s hand-rolled channel+goroutine+backoff. When `otel_settings.enabled` is `false`, `NewProvider` returns a no-op provider (SDK's `noop.NewTracerProvider()`), so downstream code (the middleware, the chat handler) always calls the same API regardless of whether tracing is actually enabled — no `if cfg.OTel.Enabled` branches scattered through the request path.

### D-08: No Trace-Context Propagation to Upstream Providers in v1

**Decision:** The proxy does not inject W3C `traceparent` headers into outbound requests to OpenAI/Anthropic. Providers are opaque third-party APIs; there's nothing on the other end to receive or use a propagated trace context, and doing so would leak proxy-internal trace IDs into upstream request logs for no benefit.

---

## Alternatives Considered

### Attach cost via a span event instead of restructuring `logRequest`

**Rejected.** OTEL span *events* (as opposed to attributes) are timestamped annotations added while a span is still open — same constraint as attributes: the span must not have ended. Since cost isn't known until the response/stream completes, an event has the identical timing problem as an attribute. The real fix is computing cost synchronously at the point spans and logs both need it (D-05), not choosing a different span API to attach it through.

### Hold the span open until the async `logRequest` goroutine finishes

**Considered.** Would let the *existing* `logRequest` cost computation feed the span without refactoring. Rejected because it re-couples span lifetime to DB write latency, which is exactly the coupling ADR 008 rejected for webhook delivery on the same grounds (don't let a background persistence path add latency or block a foreground resource's teardown, in this case the HTTP span/response).

### Metrics-first (OTEL Meter/histograms) instead of traces-first

**Rejected for v1.** Issue #30 explicitly asks for behavior comparable to Langfuse, which is trace-oriented (each LLM call renders as a "generation" span with GenAI attributes). Metrics can be added later without revisiting this ADR's span design.

---

## Consequences

- **Zero overhead when disabled.** `otel_settings.enabled: false` (the default) results in a no-op `TracerProvider`; existing request handling is unaffected.
- **`logRequest` signature changes.** Cost becomes a parameter instead of being computed inside the function (D-05). This is a small, mechanical refactor of `internal/api/handler/chat.go`, not a behavior change — the DB-persisted cost value is identical.
- **New dependency surface.** `go.mod` gains `go.opentelemetry.io/otel` and the OTLP HTTP/gRPC exporter packages. These are widely used, actively maintained, and dependency-light relative to what a hand-rolled OTLP client would require.
- **Identity attributes are best-effort.** Master-key requests (no `CachedKey`) produce spans without `llmproxy.team`/`llmproxy.application`/`llmproxy.api_key_name` — this is a pre-existing attribution gap (ADR 004 D-5), not something this ADR introduces or can fix.
- **GenAI attribute names may drift.** Because the GenAI semantic conventions are still "Development" status upstream, the hand-declared constants in `internal/otel/semconv.go` may need updating if the spec changes attribute names before it stabilizes. This is an accepted, low-cost maintenance burden versus depending on an unstable generated package.

---

## Implementation Files (proposed)

| File | Role |
|------|------|
| `internal/otel/provider.go` | `Provider` struct: TracerProvider construction, OTLP exporter setup, `Shutdown(ctx)` (D-07) |
| `internal/otel/semconv.go` | Hand-declared `gen_ai.*` and `llmproxy.*` attribute-key constants (D-03) |
| `internal/api/middleware/otel.go` | Root HTTP span middleware (D-04) |
| `internal/api/handler/chat.go` | Child provider-call span around `Route()`/`ChatCompletion(Stream)` calls; `computeCost()` extraction (D-04, D-05) |
| `internal/config/config.go` | `OTelSettings` struct, `Config.OTel` field (D-06) |
| `internal/config/loader.go` | `expandEnvVar()` applied to `otel_settings.exporter.headers` values |
| `cmd/proxy/main.go` | `otel.NewProvider()` → `defer provider.Shutdown(ctx)` lifecycle wiring (D-07) |

---

## References

- `adr/002-cost-map-model-mapping.md` — cost computation via `costmap.Manager.GetEffectiveSpec()`
- `adr/003-auth-identity-design.md`, `adr/004-api-keys-enforcement.md` — key/application/team identity model, master-key attribution gap (D-5)
- `adr/008-webhooks-notifications.md` — async background-delivery lifecycle pattern (`New() → Start(ctx) → defer Close()`) mirrored by D-07
- `internal/api/middleware/requestid.go`, `internal/api/middleware/logging.go` — existing request-scoped middleware the OTel middleware sits alongside
- `internal/api/handler/chat.go` — request handling, `logRequest`, streaming/non-streaming response paths
- OpenTelemetry GenAI semantic conventions (attribute namespace `gen_ai.*`) — https://opentelemetry.io/docs/specs/semconv/gen-ai/
