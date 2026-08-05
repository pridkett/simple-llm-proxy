# ADR 010: OpenAI Responses API Support (Background/Long-Running Requests)

**Status:** Accepted
**Date:** 2026-08-05
**ADR Issue:** pridkett/simple-llm-proxy#80
**Implementation Issue:** pridkett/simple-llm-proxy#5

---

## Context

The proxy currently exposes only the Chat Completions surface (`POST /v1/chat/completions`, streaming and non-streaming) plus `/v1/embeddings` and `/v1/models`. OpenAI's newer **Responses API** (`POST {base}/responses`, `GET {base}/responses/{id}`) is a distinct request/response schema (`input` instead of `messages`, a `status` field of `queued | in_progress | completed | failed | cancelled`, an `output` array instead of `choices`) and, critically, supports `background: true` — the caller gets back a `queued` response immediately and polls `GET /v1/responses/{id}` until the job finishes, rather than holding a connection open for the duration of a long-running generation (issue #5's stated need).

No code in this repo references the Responses API today (confirmed by grep across `internal/` and `adr/`). This is a clean, unstarted feature area, not a refactor of existing behavior.

The repo's existing architecture (`internal/provider`, `internal/router`, `internal/storage`, `internal/api/handler`) was built around a single request/response shape per call — a client sends a request, a provider is selected, a response or SSE stream comes back, and the HTTP handler's goroutine lives exactly as long as the request. Background mode breaks that assumption: a job must outlive the HTTP request that created it, be persisted so it survives a proxy restart, and be advanced by something other than the handler goroutine.

Only OpenAI implements the Responses API. No other provider in this repo (Anthropic, OpenRouter, Ollama, vLLM, MiniMax, Gemini) has an equivalent surface, so this is intentionally scoped as an OpenAI-only feature — consistent with the "don't abstract for a single consumer" precedent set in ADR-007 D-01 for `openaicompat.BaseProvider`.

## Decision

### D-01: New `ResponsesProvider` interface, not an extension of `Provider`

Adding `CreateResponse`/`GetResponse`/`CancelResponse` methods directly to the `provider.Provider` interface (`internal/provider/provider.go:15`) would force all eight existing provider implementations (OpenAI, Anthropic, OpenRouter, Ollama, vLLM, MiniMax, Gemini, plus any test doubles) to grow no-op stub methods for a capability only one of them has.

Instead, a separate interface is added to `internal/provider/provider.go`:

```go
type ResponsesProvider interface {
    CreateResponse(ctx context.Context, req *model.ResponsesRequest) (*model.ResponsesResponse, error)
    CreateResponseStream(ctx context.Context, req *model.ResponsesRequest) (Stream, error)
    GetResponse(ctx context.Context, responseID string) (*model.ResponsesResponse, error)
    CancelResponse(ctx context.Context, responseID string) (*model.ResponsesResponse, error)
}
```

The `/v1/responses` handler and the background worker resolve a deployment via the router exactly as `chat.go` does, then type-assert `deployment.Provider.(provider.ResponsesProvider)`. If the assertion fails (any non-OpenAI provider is routed for a `model_name` mapped to Responses-incapable backend), the handler returns a `400` with a clear "model does not support the Responses API" error — the same shape as the existing model-allowlist rejection path in `chat.go:59-72`.

`internal/provider/openai/openai.go` implements `ResponsesProvider` directly (new methods on the existing `openai` struct, calling `https://api.openai.com/v1/responses` and `/v1/responses/{id}`), bypassing `openaicompat.BaseProvider` since no other provider shares this schema.

### D-02: New model types in `internal/model/responses.go`

The Responses API's `input`/`output`/`status` shape is different enough from `ChatCompletionRequest`/`ChatCompletionResponse` that overloading the existing structs would require optional-everything sprawl. New types:

- `ResponsesRequest` — `Model`, `Input` (string or structured item array), `Background bool`, `Stream bool`, `PreviousResponseID *string`, plus pass-through fields (`Temperature`, `MaxOutputTokens`, `Tools`, etc.) mirrored from `ChatCompletionRequest` where OpenAI's schema names them differently.
- `ResponsesResponse` — `ID`, `Status` (`queued|in_progress|completed|failed|cancelled|incomplete`), `Output []ResponseOutputItem`, `Usage`, `Error *APIError`, `CreatedAt`, `CompletedAt *int64`.
- `ResponseOutputItem` — discriminated by `Type` (`message`, `reasoning`, `function_call`, etc.), matching OpenAI's output-item union closely enough to pass through unmodified in the common case (the proxy does not need to fully understand every item type, only to store/forward it).
- `ResponsesStreamEvent` — the Responses API's typed SSE events (`response.created`, `response.output_text.delta`, `response.completed`, etc.), distinct from `model.StreamChunk`'s chat-completions delta shape.

These are proxied close to verbatim (marshal client JSON → provider request struct → forward; provider response JSON → response struct → client) rather than translated field-by-field like the Anthropic provider does for chat completions, since the proxy's job here is pass-through plus persistence, not cross-vendor format translation.

### D-03: Background jobs persisted in a new `responses_jobs` SQLite table

A background job must survive a proxy restart (the client may poll `GET /v1/responses/{id}` minutes or hours later, possibly against a different proxy process in an HA deployment). A new migration is appended to `internal/storage/sqlite/migrations.go` (never editing existing migrations, per existing convention):

```sql
CREATE TABLE responses_jobs (
    id TEXT PRIMARY KEY,              -- OpenAI's response_id, used directly as our primary key
    api_key_id TEXT,                  -- for cost attribution / access control on GET
    deployment_key TEXT NOT NULL,     -- provider:model:api_base, so polling re-resolves the same deployment
    model_name TEXT NOT NULL,
    status TEXT NOT NULL,             -- queued|in_progress|completed|failed|cancelled|incomplete
    request_json TEXT NOT NULL,
    response_json TEXT,               -- last known full response body, updated on each poll
    error_json TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    completed_at INTEGER
);
CREATE INDEX idx_responses_jobs_status ON responses_jobs(status);
```

`Storage` (`internal/storage/storage.go`) gains `CreateResponsesJob`, `GetResponsesJob(id)`, `UpdateResponsesJob(id, status, responseJSON, errorJSON)`, `ListPendingResponsesJobs()` (for worker recovery on startup), implemented in a new `internal/storage/sqlite/responses.go` (mirroring the one-file-per-table pattern used by `sqlite/webhooks.go` / `sqlite/sticky.go`).

`GET /v1/responses/{id}` is satisfied primarily from this table (not a live upstream call) so retrieval is cheap and works even if the deployment is temporarily in cooldown; the background worker is what keeps the row fresh.

### D-04: Background worker as a polling goroutine, not upstream webhooks

OpenAI's Responses API does not push completion webhooks to third parties; the only way to learn a background job finished is to poll `GET {base}/responses/{id}`. A new package `internal/responses` provides a `Worker` with a `Run(ctx)` loop, started in `cmd/proxy/main.go` alongside the existing retention/session-cleanup tickers (`main.go:144-186`):

- On startup, calls `store.ListPendingResponsesJobs()` to resume polling jobs that were in flight when the process last stopped (handles proxy restarts mid-job).
- On a short interval (default 2s, configurable), polls each pending job's deployment via `GetResponse`, and on status transition to a terminal state (`completed`/`failed`/`cancelled`/`incomplete`) writes the final row and — mirroring `chat.go`'s `logRequest` pattern — logs cost/usage to `usage_logs` via the existing `costmap`/`keystore` accumulators, since a completed background job has real token usage to bill just like a synchronous one.
- Applies the same success/failure reporting to the router (`ReportSuccess`/`ReportFailure`) as `chat.go` does, per the ADR-006 precedent that failures should affect cooldown/backoff state.
- On shutdown, `cmd/proxy/main.go` drains the worker the same way it drains the existing `shutdownFlush` channel (`main.go:260-295, 312-314`) — in-flight polls are allowed to finish (bounded by a shutdown timeout) rather than being abandoned mid-write, since the SQLite row is the source of truth a restarted worker will pick back up regardless.

Polling interval and backoff-on-repeated-errors reuse the router's existing `BackoffManager` concepts conceptually but operate on a per-job timer, not per-deployment — a slow/stuck job does not throttle unrelated jobs.

### D-05: HTTP surface

Three new routes registered in the existing `/v1/*` KeyAuth group (`internal/api/router.go:56-71`), following the same `handler.X(r, store, sa, cm, dispatcher, cfg.GeneralSettings)` constructor-closure pattern as `ChatCompletions`:

- `POST /v1/responses` — non-streaming: blocks for a synchronous result exactly like `chat.go`'s non-streaming path. Streaming (`stream: true`): SSE loop mirroring `chat.go:217-342`, forwarding the Responses API's typed events. Background (`background: true`): creates the upstream job, inserts a `queued` row via `CreateResponsesJob`, returns the `queued` response body immediately (HTTP 200, matching OpenAI's own behavior) without waiting for completion.
- `GET /v1/responses/{id}` — reads from `responses_jobs`; 404 if the ID is unknown or belongs to a different API key (access-controlled the same way `GetLogByID` scopes by key).
- `DELETE /v1/responses/{id}` — cancels: calls `CancelResponse` upstream, updates the row to `cancelled`.

Non-background synchronous and streaming calls do not touch `responses_jobs` at all — that table exists only for the async lifecycle, keeping the common case (most Responses API calls will still be synchronous, per OpenAI's own guidance that `background` is for long-running/reasoning-heavy requests) free of extra writes.

### D-06: OpenAPI spec and coverage bar

`internal/openapi` gains the three new paths and the new schemas. Per issue #5's stated bar, new code must exceed 80% test coverage — this applies to `internal/model` (marshal/unmarshal round-trips), `internal/provider/openai` (new Responses methods against a test HTTP server), `internal/storage/sqlite/responses.go` (CRUD against a temp SQLite DB, matching existing storage test patterns), `internal/api/handler/responses.go` (sync/streaming/background/poll/cancel paths with a fake `ResponsesProvider`), and `internal/responses` (worker loop against fakes, including the restart-recovery path).

## Consequences

**Positive**
- Long-running Responses API calls (reasoning-heavy models, deep research style requests) no longer require holding an HTTP connection open for the request's full duration — the stated problem in issue #5.
- Background jobs survive proxy restarts because state lives in SQLite, not in-memory.
- No changes required to any existing provider, the `Provider` interface, or existing chat-completions behavior — this is purely additive.
- Cost/usage accounting for background jobs reuses the existing `costmap`/`keystore`/`usage_logs` pipeline rather than inventing a parallel one.

**Negative / accepted trade-offs**
- A new polling goroutine and table add a small amount of steady-state load (empty-table poll, or a handful of active-job polls) even when no background jobs are in flight; mitigated by a conservative default poll interval and by only running the worker's per-job loop while `responses_jobs` has non-terminal rows.
- Only OpenAI supports this endpoint; requests routed to a `model_name` whose only deployments are non-OpenAI providers will 400 on `/v1/responses` even though they succeed on `/v1/chat/completions`. This is inherent to the upstream API landscape, not a proxy limitation.
- `response_json`/`request_json` stored as raw TEXT (JSON blobs) rather than normalized columns, trading queryability for schema simplicity — acceptable since the admin UI's needs (issue #5 doesn't require an admin view) are limited to status/id/timestamps, all of which are normal columns.

**Out of scope**
- Cross-provider translation of the Responses API to Anthropic/Gemini/etc. equivalents (none of those vendors expose an equivalent background-job API today).
- An admin UI view for browsing background jobs (can follow in a later issue if needed).
- Webhook notifications on background job completion (the existing `webhook_subscriptions`/`notification_events` system could be extended to fire on job completion in a follow-up, but is not required by issue #5).
