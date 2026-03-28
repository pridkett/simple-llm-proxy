# ADR 006: Streaming Fixes & BackoffManager

**Status:** Accepted
**Date:** 2026-03-28
**ADR Issue:** pridkett/simple-llm-proxy#TBD

---

## Context

Phase 5 addresses four inter-related bugs in the streaming and routing path, plus a storage column alignment problem inherited from Phase 4. The bugs affect deployment health tracking, token cost logging, client disconnect handling, and rate-limit propagation. All bugs are present in production code and must be fixed before Phase 6 (provider expansion) adds more streaming paths.

### Pre-existing bugs requiring architectural decisions

**STREAM-01 — Premature ReportSuccess:** `r.ReportSuccess(deployment)` fires at line 152 of `handleStreamingResponse` immediately after `ChatCompletionStream()` returns without error. At that point, only the HTTP response headers have been received — no SSE data chunks have been read. A provider that sends a valid 200 response but then fails mid-stream (network drop, billing cutoff event, partial body) will have its success recorded before the failure is visible. The deployment's failure counter is reset to zero; it never enters cooldown regardless of how many mid-stream failures occur.

**STREAM-02 — Anthropic streaming loses all usage data:** Anthropic's SSE protocol sends token counts in two separate events: `message_start` carries `input_tokens`, and `message_delta` carries `output_tokens`. `translateStreamEvent` in `anthropic.go` parses both event types for content (stop reason, text delta) but discards the usage fields entirely. The handler creates an empty `&model.Usage{}` at stream EOF. All Anthropic streaming requests log zero cost to `usage_logs`.

**STREAM-04 — Client disconnect triggers ReportFailure:** When a client disconnects mid-stream, Go's `net/http` server cancels the request context. If cancellation occurs while the Anthropic streaming goroutine is blocked inside `reader.ReadString('\n')` (not inside the `select`), `ReadString` returns a context-cancellation error. This error propagates to `stream.Recv()`, which returns it to `handleStreamingResponse`, which returns it to `ChatCompletions`. `ChatCompletions` calls `r.ReportFailure(deployment)` on every non-nil error. A provider that correctly served a request is penalized for the client's disconnect.

**ROUTING-07 — 429 treated as hard failure:** When a provider returns HTTP 429 (rate limited), the error propagates identically to a 500 or network error. After `allowed_fails` consecutive 429s, the deployment enters cooldown for `cooldown_time`. This is semantically wrong: a 429 means "alive but overloaded, try again later". Cooldown is designed for truly broken deployments. Treating 429 as a hard failure causes deployments to exit the routing pool even after the provider's rate limit window resets, because the proxy doesn't distinguish "not responding" from "responding with backpressure".

**Storage column misalignment (D-04):** Phase 4 migration 15 renamed `usage_logs` columns `prompt_tokens`→`input_tokens` and `completion_tokens`→`output_tokens`. The Go layer was not updated. `LogRequest` and `GetLogs` in `sqlite.go` reference the old column names. The `RequestLog` struct in `storage.go` uses the old field names. This is a runtime failure — every call to `LogRequest` or `GetLogs` after migration 15 produces a SQL error.

---

## Decision

### D-01: ReportSuccess Timing Fix (STREAM-01)

**Problem:** `r.ReportSuccess(deployment)` fires on stream open, not on stream completion.

**Decision:** Remove `r.ReportSuccess(deployment)` from its current position (after `ChatCompletionStream()` returns). Move it into the `io.EOF` branch of the chunk-reading loop in `handleStreamingResponse`. Success is reported only after the full stream completes — all chunks have been received and the `[DONE]` SSE marker has been sent to the client. If an error is returned mid-stream, the caller (`ChatCompletions`) calls `r.ReportFailure(deployment)` as before (subject to the D-03 context-cancellation check below).

**Consequence:** A streaming request that establishes a connection successfully but terminates early with an error now correctly increments the failure counter. Deployments with intermittent mid-stream failures accumulate failures and enter cooldown after `allowed_fails` errors, as the system was designed to do. Non-streaming requests are unaffected — `ReportSuccess` is already in the correct location for that path.

### D-02: Stream Usage Propagation (STREAM-02)

**Problem:** Anthropic streaming discards all usage data. All streaming requests log zero cost.

**Alternatives considered:**

- **Option A — Add `GetUsage() *model.Usage` to the `Stream` interface:** Callers query after loop completion. Pro: clean interface. Con: breaking change requiring all Stream implementations to be updated; channel-based `streamAdapter` must communicate usage via a second channel or mutex-protected field, adding complexity.
- **Option B — Add `Usage *model.Usage` to `StreamChunk` (chosen):** The last chunk before channel close carries usage. Handler tracks the last non-nil `chunk.Usage` seen in the loop and passes it to `logRequest` instead of the empty `&model.Usage{}`. Pro: no interface change (backward-compatible); matches OpenAI's format (OpenAI sends usage in a final chunk when `stream_options.include_usage=true`); keeps the goroutine self-contained. Con: `StreamChunk` is a wire-format type; adding a field increases JSON size if non-nil.
- **Option C — Callback into ChatCompletionStream:** A callback fires with usage on completion. Pro: no interface change. Con: more complex plumbing with no improvement over Option B; the callback lifetime is harder to reason about than a channel message.

**Decision (Option B):** Add `Usage *Usage \`json:"usage,omitempty"\`` to `model.StreamChunk`. The `omitempty` tag ensures nil usage is absent from JSON sent to clients — wire-compatible with existing OpenAI clients.

In Anthropic's streaming goroutine, track token counts in local variables:
- `message_start` event: set `accInputTokens = event.Message.Usage.InputTokens`
- `message_delta` event: set `accOutputTokens = event.Usage.OutputTokens`

Just before the goroutine closes the `chunks` channel (after `[DONE]` or `io.EOF`), send a final synthetic `StreamChunk` with `Usage` populated and no `Choices`. The handler detects `chunk.Usage != nil && len(chunk.Choices) == 0` as an internal usage chunk — it stores the usage but does not write the chunk to the SSE stream.

**Token counting rule:** Use `message_delta.usage.output_tokens` as the authoritative output count. Use `message_start.message.usage.input_tokens` as the authoritative input count. Never sum both — that would double-count input tokens. Input tokens are reported once at stream start; output tokens are reported as a cumulative count at the final `message_delta`.

**Handler consumption:**

```go
var streamUsage *model.Usage

for {
    chunk, err := stream.Recv()
    if err == io.EOF {
        // reportSuccess here (D-01), use streamUsage for logRequest
        if store != nil {
            usage := streamUsage
            if usage == nil {
                usage = &model.Usage{}
            }
            go logRequest(store, sa, cm, apiKeyID, deployment, endpoint, usage, http.StatusOK, startTime)
        }
        return nil
    }
    if err != nil {
        if ctx.Err() != nil { return nil }  // D-03
        return err
    }
    if chunk.Usage != nil && len(chunk.Choices) == 0 {
        streamUsage = chunk.Usage
        continue  // internal usage chunk — do not write to client
    }
    // write chunk to SSE stream
}
```

### D-03: Context Cancellation Handling (STREAM-04)

**Problem:** Client disconnect mid-stream causes `ReportFailure` on a healthy deployment.

**Goroutine lifecycle analysis:** The Anthropic streaming goroutine blocks on `reader.ReadString('\n')`. When context is cancelled:

1. If cancellation occurs while the goroutine is blocked inside `ReadString`, the HTTP response body's underlying connection is torn down (context cancellation propagates through `http.Client` to the response body). `ReadString` returns with a non-EOF error.
2. The goroutine sends the error to the `errs` channel and returns (triggering `defer close(chunks)` and `defer resp.Body.Close()`).
3. `stream.Recv()` in the handler returns the error from `errs`.
4. `handleStreamingResponse` returns the error.
5. `ChatCompletions` calls `r.ReportFailure(deployment)`.

If cancellation occurs while the goroutine is blocked inside `select { case chunks <- chunk: case <-ctx.Done(): return }`, the goroutine returns cleanly (no error sent), `chunks` is closed, `stream.Recv()` returns `io.EOF`, and the handler returns `nil` — `ReportFailure` is NOT called. This path is already correct.

The bug affects only the `ReadString`-blocking path.

**Decision:** In `handleStreamingResponse`, after receiving a non-EOF error from `stream.Recv()`, check `ctx.Err() != nil`. If the context is cancelled, return `nil` (not the error). This causes `ChatCompletions` to treat the request as complete (no retry, no `ReportFailure`). The deployment health state is unaffected by client disconnects.

```go
if err != nil {
    if ctx.Err() != nil {
        return nil  // client disconnected — not a provider failure
    }
    return err
}
```

**Note on D-01 interaction:** With the D-01 fix applied, `ReportSuccess` has NOT yet fired when the mid-stream error occurs. Returning `nil` from the context-cancel path means neither `ReportSuccess` nor `ReportFailure` fires. This is the correct behavior — an incomplete request due to client disconnect should neither credit nor penalize the deployment.

**Goroutine cleanup:** When `handleStreamingResponse` returns (for any reason), `defer stream.Close()` fires, calling `resp.Body.Close()` via the stream's closer function. This unblocks any goroutine blocked in `ReadString`, causing it to return with a body-closed error. The goroutine exits. No leak. The double-close of `resp.Body` (once from `stream.Close()`, once from the goroutine's own `defer resp.Body.Close()`) is safe — `http.Response.Body.Close()` is idempotent.

### D-04: Storage Column Alignment

**Problem:** Phase 4 migration 15 renamed `usage_logs` columns but the Go layer was never updated, causing runtime SQL errors on every `LogRequest` and `GetLogs` call.

**Decision:** Fix all column-name mismatches in Plan 05-01. The following changes are required:

1. **`internal/storage/storage.go` — `RequestLog` struct:** Rename `PromptTokens int` → `InputTokens int`; rename `CompletionTokens int` → `OutputTokens int`. Add `IsStreaming bool` and `DeploymentKey string` to match new schema columns (`is_streaming`, `deployment_key`) established in migration 15.

2. **`internal/storage/sqlite/sqlite.go` — `LogRequest` INSERT:** Change column names from `prompt_tokens, completion_tokens` to `input_tokens, output_tokens`. Add `is_streaming, deployment_key` to the INSERT. Update bound values from `log.PromptTokens, log.CompletionTokens` to `log.InputTokens, log.OutputTokens, log.IsStreaming, log.DeploymentKey`.

3. **`internal/storage/sqlite/sqlite.go` — `GetLogs` SELECT:** Change `prompt_tokens, completion_tokens` to `input_tokens, output_tokens` in the SELECT column list. Update `Scan` targets from `&entry.PromptTokens, &entry.CompletionTokens` to `&entry.InputTokens, &entry.OutputTokens`.

4. **`internal/api/handler/admin.go` — `LogResponse` struct:** Update field names and their mapping from `RequestLog`. JSON keys for the admin API are renamed from `prompt_tokens`/`completion_tokens` to `input_tokens`/`output_tokens` — this is a v1.1 admin API change; no v1.0 clients depend on the admin log field names.

5. **`internal/api/handler/chat.go` — `logRequest` body:** Update `RequestLog` construction to use `InputTokens` and `OutputTokens`. Set `IsStreaming` based on whether the request was a streaming request. Set `DeploymentKey` from `deployment.DeploymentKey()`.

6. **`internal/model/response.go` — `Usage` struct:** NOT changed. `PromptTokens`/`CompletionTokens` are the OpenAI wire-protocol names sent to API clients; they must remain as-is. Only `RequestLog` (the internal DB struct) changes names.

**Scope boundary:** The `model.Usage` struct uses OpenAI protocol names (`PromptTokens`, `CompletionTokens`). The `RequestLog` struct uses DB column names (`InputTokens`, `OutputTokens`). The mapping in `logRequest` bridges these two naming spaces. Both naming systems are correct in their respective domains.

### D-05: BackoffManager Design (ROUTING-07)

**Problem:** HTTP 429 (rate limited) responses are treated as hard failures, placing deployments in cooldown even when the provider will recover after its rate-limit window resets.

**BackoffManager design:**

```go
// internal/router/backoff.go

type BackoffEntry struct {
    attempt      int
    backoffUntil time.Time
}

type BackoffManager struct {
    mu      sync.RWMutex
    entries map[string]*BackoffEntry  // key: deployment.DeploymentKey()
    base    time.Duration             // default 1s
    cap     time.Duration             // default 60s
}
```

**Algorithm:** Full-jitter exponential backoff (AWS Architecture Blog "Exponential Backoff And Jitter"):

```
cap_n   = min(maxDelay, baseDelay * 2^attempt)
sleep   = rand.Int63n(cap_n)
```

`rand.Int63n` produces uniform jitter in `[0, cap_n)`. This avoids thundering-herd problems when multiple deployments share a rate limit — the full random range ensures they do not all retry at the same moment.

**Go implementation:**

```go
func (b *BackoffManager) computeDelay(attempt int) time.Duration {
    exp := b.base << uint(attempt)  // base * 2^attempt
    if exp > b.cap || exp <= 0 {    // overflow guard
        exp = b.cap
    }
    return time.Duration(rand.Int63n(int64(exp)))
}
```

**Retry-After header:** Providers may include a `Retry-After` header (integer seconds) or `Retry-After-Ms` (milliseconds). When present, use `max(computed_backoff, retry_after_duration)` as the actual backoff duration. The `RateLimitError` type (D-06) carries this duration from the provider layer up to the router.

**Key stability:** `BackoffManager` uses `deployment.DeploymentKey()` string as the map key, not the `*Deployment` pointer. The `CooldownManager` uses pointer keys because cooldown state is tied to a specific in-memory deployment instance; it is intentionally reset on `Reload()`. The `BackoffManager` uses string keys because rate-limit state is provider-side — it must survive config reloads. A deployment pointer changes on reload; the string key is stable.

**BackoffManager does NOT replace CooldownManager.** These are parallel, independent systems tracking different phenomena:
- `CooldownManager` (pointer-keyed): tracks transient hard failures (5xx, network errors). Reset on `Reload()`.
- `BackoffManager` (string-keyed): tracks provider rate limits (429). Persists across `Reload()`.

A deployment can be in both states simultaneously.

**Key operations:**

```go
// IsBackedOff returns true if the deployment should not receive requests yet.
func (b *BackoffManager) IsBackedOff(key string) bool

// RecordRateLimit records a 429 and advances the backoff state.
// retryAfter may be zero (use computed jitter only).
func (b *BackoffManager) RecordRateLimit(key string, retryAfter time.Duration)

// RecordSuccess resets the attempt count for the key.
func (b *BackoffManager) RecordSuccess(key string)
```

**Router integration:** `BackoffManager` is created once in `router.New()` and held on the `Router` struct. It is NOT re-created in `Reload()`. `GetDeploymentWithRetry` adds a second filter alongside the existing cooldown filter:

```go
// existing cooldown check:
if !r.cooldown.InCooldown(d) && !tried[d]
// add backoff check:
if !r.cooldown.InCooldown(d) && !r.backoff.IsBackedOff(d.DeploymentKey()) && !tried[d]
```

**When all deployments are in backoff:** If all deployments for a model are in backoff (but none are in cooldown), the router selects the one whose backoff expires soonest and tries it immediately. If it returns another 429, the backoff is extended. This prevents a total stall when the full deployment pool is rate-limited.

**In `ChatCompletions` retry loop:**

```go
var rl *provider.RateLimitError
if errors.As(err, &rl) {
    r.RecordRateLimit(deployment, rl.RetryAfter)
    // do NOT call ReportFailure — 429 is not a hard failure
    continue
}
// context cancelled
if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
    // client disconnected — no penalty
    return
}
// genuine hard failure
r.ReportFailure(deployment)
```

`r.RecordRateLimit` is a new `Router` method that delegates to `r.backoff.RecordRateLimit(deployment.DeploymentKey(), retryAfter)`.

### D-06: RateLimitError Type

**Problem:** No typed mechanism exists to distinguish 429 responses from other errors in the retry loop.

**Decision:** Add a typed error to the `provider` package:

```go
// internal/provider/provider.go

// RateLimitError is returned when a provider responds with HTTP 429.
// RetryAfter is the duration from the Retry-After header, or 0 if absent.
type RateLimitError struct {
    Provider   string
    RetryAfter time.Duration
    Message    string
}

func (e *RateLimitError) Error() string {
    if e.RetryAfter > 0 {
        return fmt.Sprintf("%s: rate limited — %s (retry after %s)", e.Provider, e.Message, e.RetryAfter)
    }
    return fmt.Sprintf("%s: rate limited — %s", e.Provider, e.Message)
}
```

Both Anthropic and OpenAI providers: on HTTP 429, parse the `Retry-After` header (integer seconds or HTTP-date), return `&provider.RateLimitError{Provider: p.Name(), RetryAfter: d, Message: msg}`.

**Retry-After parsing:** Define an unexported `parseRetryAfter(header string) time.Duration` helper in `internal/provider/provider.go` (accessible to all provider subpackages via the `provider` import):

```go
func ParseRetryAfter(header string) time.Duration {
    if header == "" {
        return 0
    }
    // Try integer seconds first (most common)
    if secs, err := strconv.Atoi(header); err == nil {
        return time.Duration(secs) * time.Second
    }
    // Try HTTP-date format
    if t, err := http.ParseTime(header); err == nil {
        if d := time.Until(t); d > 0 {
            return d
        }
    }
    return 0
}
```

`http.ParseTime` is stdlib (`net/http`). No new external dependencies are introduced.

**Detection in ChatCompletions:**

```go
var rlErr *provider.RateLimitError
if errors.As(err, &rlErr) {
    r.RecordRateLimit(deployment, rlErr.RetryAfter)
    continue
}
```

---

## Alternatives Considered

### Moving ReportSuccess to handler (non-stream) for symmetry

**Rejected.** For non-streaming requests, `ChatCompletion()` returns only after the full response body is read and parsed. There is no "partial response" scenario. Moving `ReportSuccess` in the non-streaming path would be a no-op change with no benefit. Only the streaming path has the premature-success problem.

### Option A: GetUsage() on Stream interface for usage propagation

**Rejected.** See D-02 rationale. Requires breaking interface change and more complex `streamAdapter` implementation. Option B (field on StreamChunk) is equivalent in effect with lower complexity.

### Using context.Canceled check in ChatCompletions instead of handleStreamingResponse

**Considered.** Checking `ctx.Err()` in the `ChatCompletions` retry loop (after `handleStreamingResponse` returns an error) would also prevent `ReportFailure` on client disconnect. Rejected because: (1) `ChatCompletions` cannot distinguish which error came from context cancel vs. a genuine mid-stream failure without the error type carrying that information; (2) placing the check in `handleStreamingResponse` is closer to the error source and makes the intent explicit.

### Replacing CooldownManager with BackoffManager

**Rejected.** The two systems address different scenarios. `CooldownManager` is correctly keyed by pointer — cooldown state is transient and should reset when config reloads. `BackoffManager` is keyed by string — rate-limit state is provider-side and must survive reloads. Merging them would require complicating either the key type or the reset semantics.

### Configurable BackoffManager parameters

**Deferred.** Base delay (1s) and cap (60s) are hardcoded defaults for Phase 5. Making them config-file fields is deferred to Phase 7 (pool routing), where per-pool backoff configuration will be natural. Phase 5 uses sensible defaults that cover all known provider rate-limit windows (OpenAI's shortest window is 60s).

---

## Consequences

- **After D-01:** Streaming requests that fail mid-stream will correctly increment the failure counter and trigger cooldown after `allowed_fails` errors. Streaming requests that complete normally still call `ReportSuccess`.
- **After D-02:** Anthropic streaming requests log accurate token counts and non-zero cost to `usage_logs`. The `StreamChunk` wire format gains an optional `usage` field (nil for all non-final chunks; non-nil for the internal usage chunk which is filtered before SSE output).
- **After D-03:** Client disconnects mid-stream no longer penalize deployments. The deployment health state reflects only genuine provider failures.
- **After D-04:** `LogRequest` and `GetLogs` produce valid SQL. Requests begin logging to the v1.1 schema columns. `DeploymentKey` is populated in `usage_logs` starting in Phase 5, enabling per-deployment cost attribution in Phase 8.
- **After D-05/D-06:** 429 responses route to `BackoffManager` instead of `CooldownManager`. Rate-limited deployments experience exponential backoff before being retried, rather than a fixed cooldown window. Deployments that are rate-limited but otherwise healthy remain in the routing pool for non-rate-limited request slots (when the backoff window has passed).
- **No new external dependencies.** `BackoffManager` uses only stdlib (`math/rand`, `sync`, `time`). `ParseRetryAfter` uses stdlib `net/http.ParseTime`.
- **Phase 6 impact:** Provider implementations added in Phase 6 must return `*provider.RateLimitError` on HTTP 429. The pattern is established in Phase 5 for Anthropic and OpenAI; Phase 6 providers follow the same pattern.

---

## Implementation Files

| File | Role |
|------|------|
| `internal/model/response.go` | Add `Usage *Usage \`json:"usage,omitempty"\`` to `StreamChunk` (D-02) |
| `internal/provider/provider.go` | Add `RateLimitError` type, `ParseRetryAfter` helper (D-06) |
| `internal/provider/anthropic/anthropic.go` | Accumulate usage in streaming goroutine, emit usage chunk; return `RateLimitError` on 429 (D-02, D-06) |
| `internal/provider/openai/openai.go` | Return `RateLimitError` on 429 for both streaming and non-streaming paths (D-06) |
| `internal/storage/storage.go` | Rename `RequestLog.PromptTokens`→`InputTokens`, `CompletionTokens`→`OutputTokens`; add `IsStreaming`, `DeploymentKey` (D-04) |
| `internal/storage/sqlite/sqlite.go` | Update `LogRequest` INSERT and `GetLogs` SELECT column names (D-04) |
| `internal/api/handler/admin.go` | Update `LogResponse` struct and field mapping (D-04) |
| `internal/api/handler/chat.go` | Move `ReportSuccess` to EOF branch; context-cancel check; accumulate `streamUsage`; update `logRequest` call; detect `RateLimitError` in retry loop (D-01, D-02, D-03, D-04) |
| `internal/router/backoff.go` | New file: `BackoffManager`, `BackoffEntry`, `fullJitterDelay` (D-05) |
| `internal/router/router.go` | Add `backoff *BackoffManager` field; initialize in `New()` (not `Reload()`); add `RecordRateLimit()` method; update `GetDeploymentWithRetry` filter (D-05) |

---

## References

- `.planning/phases/05-streaming-fixes-backoffmanager/05-RESEARCH.md` — Full analysis of all bugs, goroutine lifecycle, and BackoffManager design
- `.planning/phases/05-streaming-fixes-backoffmanager/05-00-PLAN.md` — This ADR plan
- `adr/005-schema-config-foundation.md` — Phase 4 decisions; migration 15 renamed usage_logs columns (D-04 context)
- `internal/api/handler/chat.go` — Source of STREAM-01 bug (line 152) and STREAM-04 bug
- `internal/provider/anthropic/anthropic.go` — Source of STREAM-02 bug (`translateStreamEvent`)
- `internal/storage/sqlite/sqlite.go` — Source of D-04 misalignment
- AWS Architecture Blog: "Exponential Backoff And Jitter" — Full-jitter formula referenced in D-05
