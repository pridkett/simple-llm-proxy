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
2. **Auth/identity** (ADR 003, ADR 004): `KeyAuth` middleware injects a `*keystore.CachedKey` (`internal/keystore/cache.go`, wrapping `storage.APIKey`, which has `ApplicationID` but not a resolved application/team *name*) into context — but only for the `/v1/*` route group (`internal/api/router.go:56-57`); it is **not** global middleware. `storage.Application` → `storage.Team` gives the project/application hierarchy the issue asks for, but resolving names today requires a DB lookup beyond what `CachedKey` currently carries. Master-key requests inject no key at all, so they have no project/application attribution — this is an existing gap (ADR 004 D-5), not something this ADR can close. Session-authenticated admin requests (`/admin/chat/completions`, `internal/api/router.go:94`) carry a `*storage.User` via `middleware.RequireSession` instead of a `CachedKey` — a separate identity source this ADR must also account for.
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

**Revision note:** an earlier draft of this ADR used `gen_ai.system` for provider identification. The spec has been moving that attribute to legacy status in favor of `gen_ai.provider.name` as the GenAI conventions stabilize. This ADR now targets `gen_ai.provider.name`. Because the spec is still in flux, the implementer must re-check the exact attribute name against the semconv version current at implementation time — `internal/otel/semconv.go`'s constants are the single place that needs updating if the spec renames things again.

Attributes set on the provider-call child span (see D-04 for exactly when each becomes available):

| Attribute | Source | Set at |
|---|---|---|
| `gen_ai.operation.name` | `"chat"` (fixed for `/v1/chat/completions`) or `"embeddings"` (fixed for `/v1/embeddings`, see D-04) | span creation |
| `gen_ai.request.model` | requested model name (user-facing, from `model_list`) — known before routing | span creation |
| `gen_ai.request.stream` | `true`/`false` from the request body | span creation |
| `gen_ai.provider.name` | `d.ProviderName` (e.g. `"openai"`, `"anthropic"`) | span end (only known once `Route()` picks a deployment) |
| `gen_ai.response.model` | `d.ActualModel` (provider-side model actually invoked) | span end |
| `gen_ai.usage.input_tokens` | from response/stream usage | span end |
| `gen_ai.usage.output_tokens` | from response/stream usage | span end |
| `server.address` | host portion of `provider.Deployment.APIBase`, parsed via `net/url` | span end |
| `server.port` | port portion of `provider.Deployment.APIBase` (default 443 for `https`) | span end |
| `error.type` | Go error type / provider error code, only set on the error branches (D-04) | on error, before `span.End()` |

**Span naming**, per the GenAI spec's recommended `{operation} {model}` format: the child span is named `chat {requested-model}` (or `embeddings {requested-model}`) at creation, using the user-facing requested model name (the only one known that early). The root HTTP span is named after the route, e.g. `POST /v1/chat/completions`.

Proxy-specific attributes that have no GenAI equivalent use an `llmproxy.*` namespace. Per D-04, **all of these are set on the child span**, not the root span, because the child span is created inside the handler — after whichever auth middleware ran (`KeyAuth` for `/v1/*`, `RequireSession` for `/admin/*`) — where identity is already in context via the normal `context.Value` accessors, with no cross-middleware propagation trick required:

| Attribute | Source | Notes |
|---|---|---|
| `llmproxy.team` | `middleware.APIKeyFromContext(ctx).Key.ApplicationID` resolved to a team name (see below) | Omitted for master-key requests (no `CachedKey`) and for session-authenticated admin requests unless `middleware.UserFromContext` carries an equivalent team association |
| `llmproxy.application` | resolved application name, same source | Omitted under the same conditions |
| `llmproxy.api_key_name` | `storage.APIKey.Name` (never the raw key or hash) | Omitted for master-key requests |
| `llmproxy.user_email` | `storage.User.Email` from `middleware.UserFromContext(ctx)` | Set only for session-authenticated `/admin/*` requests, as the closest available identity |
| `llmproxy.pool_name` | `RouteResult.PoolName` | Set at span end, after `Route()` returns |
| `llmproxy.cost.total_usd` | `costmap.Manager.GetEffectiveSpec()` result | Set at span end (see D-05) |
| `llmproxy.request_id` | `middleware.RequestIDFromContext(ctx)` | Set on **both** spans, for correlation with existing structured logs |

**Team/application name resolution without a per-request DB call:** `CachedKey` (`internal/keystore/cache.go`) today stores only `*storage.APIKey` (which has `ApplicationID int64`, not a name) plus `AllowedModels`. Resolving `llmproxy.team`/`llmproxy.application` to human-readable names requires joining `applications`/`teams` — but `keystore.Cache.Get()` already performs a DB fetch on every cache miss (60s TTL, per `internal/keystore/cache.go`). This ADR extends `CachedKey` with two additional fields, `TeamName` and `AppName`, populated by the *existing* cache-fill query (extended to join `applications`/`teams`, mirroring the join `storage.AccessibleKey` already does) rather than a new per-request lookup. This keeps D-05's "in-memory, no I/O added to the hot path" property intact: the extra join cost is paid once per cache-fill (amortized over the 60s TTL), not once per request.

**Never included:** raw API key values, key hashes, or request/response body content. This mirrors the existing `RequestLog` design (ADR 002/004), which never persists secrets or payload bodies either.

### D-04: Two-Span Structure — Root HTTP Span + Child "Logical Operation" Span

**Decision:** Tracing hooks into two points in the request path, with each span's identity/attribute sourcing fixed to a point in the code where the required data actually exists (this supersedes an earlier draft that assumed identity was visible from the root span and that the child span mapped 1:1 to a single provider attempt):

1. **Root HTTP span**, started in a new `middleware.OTel()` middleware inserted into the global chain immediately after `RequestID()` (`internal/api/router.go:30-38`), sibling to `Logging()`. It wraps `next.ServeHTTP` the same way `Logging()` does and records only what is knowable at that point in the chain: `http.status_code`, `http.route`, response size, and `llmproxy.request_id`. **It carries no identity or GenAI attributes** — `KeyAuth` and `RequireSession` are group-scoped, not global (`internal/api/router.go:56-57,76`), and even where they do run, a middleware's `ctx = context.WithValue(...); next.ServeHTTP(w, r.WithContext(ctx))` only extends the context for handlers *further in* the chain, not for the caller's own `r` after `next.ServeHTTP` returns — so a global outer span cannot read values a group-scoped inner middleware set, without an explicit shared mutable-pointer pattern this ADR does not introduce. Keeping the root span's attribute set to only what is genuinely available where it's created avoids that trap entirely.

2. **Child span**, covering the **logical operation** (matching the GenAI spec's guidance that a span covering automatic retries should span the full retried operation, not one span per attempt) — created in `handler.ChatCompletions` / `handler.Embeddings` *before* calling `r.Route(ctx, ...)`, not inside the `RouteCallback` closure passed to it. `RouteCallback` (`internal/router/route.go:36-37`) receives only `*provider.Deployment` per attempt and `RouteResult` (with `PoolName`, `DeploymentUsed`) is only available after `Route()` returns — so nothing that depends on routing outcome can be set at span-creation time regardless of where inside the call the span starts. Concretely:
   - **At span creation** (before `Route()` is called): `gen_ai.operation.name`, `gen_ai.request.model`, `gen_ai.request.stream` — all known from the parsed request body. This is also where identity attributes are set, because at this point in `handler.ChatCompletions`/`handler.Embeddings` the request has already passed through whichever auth middleware applies (`KeyAuth` for `/v1/*`, `RequireSession` for `/admin/*`), so `middleware.APIKeyFromContext(ctx)` / `middleware.UserFromContext(ctx)` are populated in the *same* context the handler already holds — no cross-middleware propagation needed.
   - **Per attempt, inside the `RouteCallback` closure**: each failed attempt records a span **event** (not a separate span) with `{provider, model, error}`, using `RouteResult.FailoverReasons`-equivalent data already computed per-attempt by the router. This preserves failover visibility without violating the spec's single-logical-span guidance.
   - **At span end** (after `Route()` returns and, for streaming, after the stream completes): `gen_ai.provider.name`, `gen_ai.response.model`, `gen_ai.usage.*`, `server.address`/`server.port`, `llmproxy.pool_name`, `llmproxy.cost.total_usd` (see D-05) are all set immediately before `span.End()`.

**Span lifecycle and error paths (streaming):** the child span's `End()` is registered via a single `defer span.End()` placed immediately after the span is created, so it fires on every return path out of the streaming handler, not only the `io.EOF` branch — including the missing-flusher early return (`chat.go:249-251`), the `context.Canceled`/`DeadlineExceeded` branch (`chat.go:299-301`), and the mid-stream provider-error branch (`chat.go:303-304`). Each of those three non-happy-path returns additionally calls `span.RecordError(err)` and `span.SetStatus(codes.Error, ...)` with `error.type` set before the deferred `End()` runs. The root span's `End()` is likewise deferred immediately after creation in the middleware, so it covers panics recovered by `middleware.Recovery()` (which runs outside the OTel middleware in the chain) the same way `Logging()` already does.

For streaming requests, the child span stays open for the full duration of the stream (ends at `io.EOF`, or at one of the error returns above), and the root span ends when `ServeHTTP` returns, which for streaming handlers is after the stream completes. No change to the streaming code path's control flow is required beyond the `defer`-based span closing described above.

**Rejected alternative:** A single flat span per request. Rejected because it can't distinguish time spent in proxy-side routing/auth/logging from time spent waiting on the upstream provider, which is the single most useful thing a trace adds over the existing structured request log.

**Rejected alternative:** One child span per provider attempt (siblings under the root span). Rejected because it contradicts the GenAI spec's explicit guidance that a span covering automatic retries "SHOULD cover the duration of the logical operation with all retries" — per-attempt spans would also fragment cost/usage attribution, since only the winning attempt has usage data.

### D-05: Synchronous Cost Computation Before Span End, Decoupled From Async Persistence

**Problem:** `costmap.Manager.GetEffectiveSpec()` today runs only inside the detached `logRequest` goroutine (`chat.go`), which is the wrong place to source a span attribute — spans must be closed (`span.End()`) before or as the HTTP handler returns, and cannot have attributes added afterward.

**Decision:** Extract the cost lookup out of `logRequest` into a small synchronous helper called from `handleNonStreamingResponse` / `handleStreamingResponse` **before** the child span is ended and **before** `go logRequest(...)` is fired. The helper's signature must match what `GetEffectiveSpec` actually needs — the current call site (`chat.go:381`) is `cm.GetEffectiveSpec(p.Deployment.ModelName, []string{p.Deployment.ActualModel})`, where the second argument is a candidate list that lets cost mapping resolve aliased/pool-member model names. The extracted helper is therefore `computeCost(cm *costmap.Manager, deployment *provider.Deployment, usage *model.Usage) float64`, calling `cm.GetEffectiveSpec(deployment.ModelName, []string{deployment.ActualModel})` internally and multiplying by usage — not a simplified `computeCost(model, usage)` as an earlier draft of this ADR proposed, which would have dropped the alias-resolution candidate and silently diverged cost values for aliased/pooled deployments. The computed value is passed as a parameter into `logRequest` instead of being recomputed there, so there is exactly one call site for the lookup, not two.

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

### D-07: TracerProvider Lifecycle — Explicit Shutdown After `server.Shutdown`, Not a Startup-Time Defer

**Problem (from an earlier draft of this ADR):** `WebhookDispatcher.Close()` (`internal/webhook/dispatcher.go:109,113`) takes **no arguments** and builds its own internal `drainCtx` with a bounded timeout — it is not handed a caller-supplied `ctx`. It is registered via `defer dispatcher.Close()` at construction time (`main.go:242`), which means Go's LIFO defer ordering runs it *after* the explicit `server.Shutdown(ctx)` call in the signal-handling block (`main.go:320`), not before. An earlier draft of this ADR proposed `defer provider.Shutdown(ctx)` positioned "immediately before `server.Shutdown(ctx)`" while also claiming to mirror the dispatcher — those two claims are inconsistent with each other and with how defers actually execute.

**Decision:** `otel.Provider.Shutdown()` takes **no context argument** and builds its own bounded internal context (5s, matching `webhook.drainTimeout`), exactly like `WebhookDispatcher.Close()`. It is **not** deferred at construction time. Instead, it is called explicitly, in-line, in the signal-handling shutdown sequence in `cmd/proxy/main.go`, immediately *after* `server.Shutdown(ctx)` returns (i.e., right after `main.go:320`) and before the function returns (which is when the startup-time defers — `store.Close()`, `r.Close()`, `dispatcher.Close()` — fire). This ordering is deliberate: `server.Shutdown(ctx)` stops accepting new connections and waits for in-flight requests to finish, which is exactly when their spans finish and need to be exported; calling `otelProvider.Shutdown()` only after that point guarantees spans from requests that were in-flight at shutdown time get flushed, not dropped.

An `internal/otel` package exposes `otel.NewProvider(cfg) (*otel.Provider, error)`, constructed in `cmd/proxy/main.go` right after storage init (so its own startup can be logged). Internally, `otel.Provider` wraps the standard SDK's `sdktrace.TracerProvider` configured with a `BatchSpanProcessor` wrapping the configured OTLP exporter — the SDK's own batching/retry logic is used as-is rather than reimplementing the `WebhookDispatcher`'s hand-rolled channel+goroutine+backoff. When `otel_settings.enabled` is `false`, `NewProvider` returns a no-op provider (SDK's `noop.NewTracerProvider()`), so downstream code (the middleware, the chat handler) always calls the same API regardless of whether tracing is actually enabled — no `if cfg.OTel.Enabled` branches scattered through the request path. `Shutdown()` on the no-op provider is a no-op itself.

### D-08: No Trace-Context Propagation to Upstream Providers in v1

**Decision:** The proxy does not inject W3C `traceparent` headers into outbound requests to OpenAI/Anthropic. Providers are opaque third-party APIs; there's nothing on the other end to receive or use a propagated trace context, and doing so would leak proxy-internal trace IDs into upstream request logs for no benefit.

### D-09: `/v1/embeddings` and `/admin/embeddings` Are In Scope, Not Just Chat Completions

**Decision:** `internal/api/handler/embeddings.go` follows the identical shape to `chat.go` — `Route(...)` → `embResp.Usage` → `logRequest(...)` (`embeddings.go:78-130`) — and the GenAI spec defines a distinct `embeddings` operation with the same attribute set minus streaming/output-token fields. An earlier draft of this ADR only mentioned `chat.go` in its file list, which would have silently left embeddings requests untraced despite issue #30 asking for coverage of "all LLM requests." This ADR extends D-03/D-04's child-span design to `handler.Embeddings` with `gen_ai.operation.name = "embeddings"` and no `gen_ai.request.stream`/streaming-related span-lifecycle concerns (embeddings responses are not streamed in this API). `/v1/completions` remains out of scope — it already returns HTTP 410 (Gone) and has no request path to instrument.

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

- **Zero overhead when disabled.** `otel_settings.enabled: false` (the default) results in a no-op `TracerProvider`; existing request handling is unaffected beyond the negligible cost of no-op span-start calls (a few hundred nanoseconds each) that are always paid once the middleware/handler instrumentation exists, whether or not tracing is enabled.
- **`logRequest` signature changes.** Cost becomes a parameter, computed via the extracted `computeCost(cm, deployment, usage)` helper instead of being computed inside the function (D-05). This is a small, mechanical refactor of `internal/api/handler/chat.go` and `embeddings.go`, not a behavior change — the DB-persisted cost value is identical, including its existing (unchanged) behavior of returning $0 if `costmap.Manager` hasn't finished its initial async load yet.
- **`CachedKey` grows two fields.** `TeamName`/`AppName` are added to `keystore.CachedKey` and populated by extending the existing cache-fill query (D-03) — this is the one place identity resolution costs anything beyond an in-memory read, and it's amortized over the 60s cache TTL, not paid per request.
- **New dependency surface.** `go.mod` gains `go.opentelemetry.io/otel` and the OTLP HTTP/gRPC exporter packages. These are widely used, actively maintained, and dependency-light relative to what a hand-rolled OTLP client would require.
- **Identity attributes are best-effort.** Master-key requests (no `CachedKey`) produce child spans without `llmproxy.team`/`llmproxy.application`/`llmproxy.api_key_name` — this is a pre-existing attribution gap (ADR 004 D-5), not something this ADR introduces or can fix. The root span never carries identity attributes at all (D-04) — this is a deliberate scoping decision, not a gap.
- **`/v1/embeddings` and `/admin/embeddings` are in scope for v1** (D-09), unlike an earlier draft of this ADR. `/v1/completions` (HTTP 410) is not.
- **GenAI attribute names may drift.** Because the GenAI semantic conventions are still "Development" status upstream, the hand-declared constants in `internal/otel/semconv.go` may need updating if the spec changes attribute names before it stabilizes — this ADR already had to correct `gen_ai.system` → `gen_ai.provider.name` once during review (D-03). This is an accepted, low-cost maintenance burden versus depending on an unstable generated package.

---

## Implementation Files (proposed)

| File | Role |
|------|------|
| `internal/otel/provider.go` | `Provider` struct: TracerProvider construction, OTLP exporter setup, no-arg `Shutdown()` with internal bounded context (D-07) |
| `internal/otel/semconv.go` | Hand-declared `gen_ai.*` and `llmproxy.*` attribute-key constants (D-03) |
| `internal/api/middleware/otel.go` | Root HTTP span middleware — status/route/duration/request-id only, no identity (D-04) |
| `internal/api/handler/chat.go` | Child logical-operation span wrapping `Route()` (not the per-attempt callback), per-attempt failure events, `computeCost()` call, error-path `SetStatus`/`RecordError` on all streaming exit branches (D-04, D-05) |
| `internal/api/handler/embeddings.go` | Same child-span treatment as `chat.go`, `gen_ai.operation.name = "embeddings"` (D-09) |
| `internal/keystore/cache.go` | `CachedKey.TeamName`/`AppName` fields, cache-fill query extended to join `applications`/`teams` (D-03) |
| `internal/config/config.go` | `OTelSettings` struct, `Config.OTel` field (D-06) |
| `internal/config/loader.go` | `expandEnvVar()` applied to `otel_settings.exporter.headers` values |
| `cmd/proxy/main.go` | `otel.NewProvider()` at startup; explicit `otelProvider.Shutdown()` call after `server.Shutdown(ctx)` returns in the signal-handling block, not a startup-time defer (D-07) |

---

## References

- `adr/002-cost-map-model-mapping.md` — cost computation via `costmap.Manager.GetEffectiveSpec()`
- `adr/003-auth-identity-design.md`, `adr/004-api-keys-enforcement.md` — key/application/team identity model, master-key attribution gap (D-5)
- `adr/008-webhooks-notifications.md` — `WebhookDispatcher.Close()`'s no-arg/internal-bounded-context pattern, mirrored by D-07 (`internal/webhook/dispatcher.go:109,113`)
- `internal/api/router.go:30-38,56-57,76,94` — global vs. group-scoped middleware boundaries (`KeyAuth` on `/v1/*` only, `RequireSession` on `/admin/*` only) that drove D-04's root-span-carries-no-identity decision
- `internal/api/middleware/requestid.go`, `internal/api/middleware/logging.go` — existing request-scoped middleware the OTel middleware sits alongside
- `internal/api/middleware/keyauth.go` — `KeyAuth`'s `context.WithValue` + `r.WithContext` pattern, which does not propagate to an outer middleware after `next.ServeHTTP` returns
- `internal/keystore/cache.go` — `CachedKey` struct extended with `TeamName`/`AppName` (D-03)
- `internal/router/route.go:25-37` — `RouteResult`/`RouteCallback` shapes that constrain when routing-outcome attributes become available (D-04)
- `internal/api/handler/chat.go`, `internal/api/handler/embeddings.go` — request handling, `logRequest`, streaming/non-streaming response paths, all handler-level return branches relevant to span lifecycle
- `cmd/proxy/main.go:242,320` — `dispatcher.Close()` defer position vs. `server.Shutdown(ctx)` call site, the ordering that drove D-07's correction
- OpenTelemetry GenAI semantic conventions (attribute namespace `gen_ai.*`, including the `gen_ai.system` → `gen_ai.provider.name` migration) — https://opentelemetry.io/docs/specs/semconv/gen-ai/
