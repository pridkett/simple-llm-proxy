# ADR 008: Webhooks & Notifications Architecture

**Status:** Accepted
**Date:** 2026-03-31
**ADR Issue:** pridkett/simple-llm-proxy#39

---

## Context

Phase 9 adds outbound webhook delivery for routing events with HMAC signing and retry, plus a queryable notification event feed accessible via the Admin API. Three routing events need notification: budget exhaustion (all pools exhausted for a model), provider failover (request re-routed after a deployment failure or 429), and full pool cooldown (all members of a pool are in cooldown state). These events are already detectable in the handler layer via `RouteResult.FailoverReasons` — Phase 9 wires event emission, delivery, and persistence around that existing infrastructure.

### Two webhook sources

Webhooks come from two independent sources:

1. **YAML config** (`webhooks:` section) — parsed at startup, held in memory, never written to the database. These are the operator's static webhook configuration. The `secret` field supports `os.environ/VAR` expansion, already handled by `config/loader.go`.

2. **Database** (`webhook_subscriptions` table) — created, updated, and deleted through Admin API endpoints. These are UI-managed webhooks with full CRUD.

ADR 005 (Decision 4) established this separation: YAML webhooks and DB webhooks never overlap. At read/fire time, both sources are merged — YAML webhooks surfaced with `"source": "yaml"` (read-only), DB webhooks with `"source": "ui"` (editable). This ADR does not revisit that decision; it documents how the delivery system merges both sources at dispatch time.

### Existing schema

The Phase 4 migrations (ADR 005) created three tables that this phase activates:

- `webhook_subscriptions` (migration 21) — UI-created webhooks with `url`, `events`, `secret`, `enabled` columns
- `notification_events` (migrations 22-24) — Routing event log with `event_type`, `payload` (JSON), indexed by `created_at` and `event_type`
- `webhook_deliveries` (migrations 25-26) — Delivery queue with `subscription_id`, `event_id`, `attempt_count`, `status`, `response_code`

The `webhook_deliveries` table requires a schema fix: `subscription_id` must be nullable (to support YAML webhook deliveries that have no DB subscription row) and the ON DELETE CASCADE must be added to both FK constraints. This is addressed in the implementation plan, not this ADR — the fix is a migration correction, not an architectural decision.

### Admin API endpoints

Phase 9 adds two groups of Admin API endpoints:

1. **Webhook CRUD** — `GET/POST/PUT/DELETE /admin/webhooks` for managing UI-created webhooks, with YAML webhooks included as read-only entries in listing responses
2. **Notification events feed** — `GET /admin/events` with pagination and `event_type` filtering

Both endpoint groups are protected by the existing master key authentication middleware.

---

## Decision

### D-01: Handler-Layer Event Emission

**Problem:** Routing events (failover, budget exhaustion, pool cooldown) must be captured and emitted without coupling the router to the webhook delivery system.

**Decision:** Routing events are captured in the **handler layer** after `Route()` returns. The handler already inspects `RouteResult.FailoverReasons` for response headers (via `SetRouteHeaders`); event emission hooks into the same inspection point. The `Route()` method itself stays transport-agnostic and does not emit events directly.

After `Route()` returns a `RouteResult`, the handler calls an `emitRoutingEvents(dispatcher, result)` helper that inspects the result and sends zero or more events to the `WebhookDispatcher` channel. This keeps event emission logic in one place and avoids scattering webhook concerns across the router internals.

### D-02: Three Event Types

**Decision:** Three event types are emitted, each corresponding to a distinct routing condition:

| Event Type | Trigger | When |
|---|---|---|
| `budget_exhausted` | `RouteResult.FailoverReasons` contains `FailoverBudgetExhausted` | All pools exhausted for a model |
| `provider_failover` | `RouteResult.FailoverReasons` is non-empty AND a response was ultimately served | Failover succeeded after one or more deployment failures |
| `pool_cooldown` | All members of a pool are in cooldown state | Full pool cooldown detected during routing |

These types match the NOTIFY-06 requirement. The `provider_failover` event is the most common — it fires whenever the router tries a second deployment after the first fails or returns 429.

### D-03: Every-Occurrence Firing (No Debounce)

**Decision:** Events fire on **every qualifying occurrence**, not debounced. If a model experiences 10 consecutive failovers, 10 `provider_failover` events are emitted. Webhook subscribers can filter or deduplicate on their side. The proxy does not suppress repeated events for the same condition.

**Rationale:** Debounce requires state tracking (last emission time per event type per model), timer management, and decisions about debounce windows. Every-occurrence firing is stateless in the handler, keeps the event system simple, and gives receivers complete visibility into the frequency of routing events. Receivers that want debounce can implement it trivially (ignore events within N seconds of the last identical event).

### D-04: IFTTT/Zapier-Compatible Event Payload

**Decision:** Webhook payloads use a dual-layer JSON schema with IFTTT-compatible summary fields and a structured context object:

```json
{
  "event_type": "provider_failover",
  "timestamp": "2026-03-31T14:30:00Z",
  "value1": "gpt-4",
  "value2": "openai/gpt-4 -> anthropic/claude-3-haiku",
  "value3": "rate_limited",
  "context": {
    "model": "gpt-4",
    "pool_name": "gpt-4-pool",
    "providers_tried": ["openai/gpt-4", "anthropic/claude-3-haiku"],
    "provider_used": "anthropic/claude-3-haiku",
    "failover_reasons": ["rate_limited"],
    "budget_remaining": null
  }
}
```

The `value1`/`value2`/`value3` fields are IFTTT-compatible summary fields. Simple automation platforms (IFTTT, Zapier, Make) can map these directly to trigger values without parsing nested JSON. The `context` object carries structured metadata for programmatic consumers who need full routing details.

**Field semantics by event type:**

| Event Type | value1 | value2 | value3 |
|---|---|---|---|
| `budget_exhausted` | model name | pool name | "budget_exceeded" |
| `provider_failover` | model name | "from -> to" provider path | primary failover reason |
| `pool_cooldown` | model name | pool name | "all_members_in_cooldown" |

### D-05: Async Channel-Based WebhookDispatcher

**Problem:** Webhook delivery must never block the LLM response path. A synchronous HTTP POST to a slow or unresponsive webhook endpoint would add latency to every proxied request.

**Decision:** A `WebhookDispatcher` struct receives events from handlers via a buffered channel and delivers them in a background goroutine. The dispatcher is created once, started with `Start(ctx)`, and stopped with `Close()`.

```go
type WebhookDispatcher struct {
    events  chan NotificationEvent
    store   storage.Storage
    cfg     *config.Config
    client  *http.Client
    wg      sync.WaitGroup
}
```

The channel buffer size is set to 256. If the channel is full (delivery goroutine is backed up), the handler logs a warning and drops the event. This preference for request throughput over event completeness is intentional — webhook delivery is best-effort. The 256-entry buffer provides headroom for burst scenarios (cascading failures producing many events in quick succession).

### D-06: Full-Jitter Exponential Backoff Retry (5 Attempts)

**Decision:** Failed webhook deliveries retry up to 5 times using full-jitter exponential backoff, matching the `BackoffManager` pattern established in ADR 006 (D-05):

```
cap_n = min(maxDelay, baseDelay * 2^attempt)
sleep = rand.Int63n(cap_n)
```

**Parameters:** Base delay 1 second, max delay 5 minutes. This produces retry windows of approximately:

| Attempt | Max Delay | Expected Delay |
|---|---|---|
| 1 | 2s | ~1s |
| 2 | 4s | ~2s |
| 3 | 8s | ~4s |
| 4 | 16s | ~8s |
| 5 | 32s | ~16s |

Each attempt is recorded in the `webhook_deliveries` table with `attempt_count`, `last_attempt_at`, `status` ("pending", "delivered", "failed"), and `response_code`. After 5 failed attempts, the delivery status is set to "failed" permanently.

A webhook delivery is considered successful if the receiver responds with any HTTP 2xx status code. Any other status code (including 3xx redirects) is treated as a failure and triggers retry.

### D-07: Dispatcher Lifecycle Pattern (Start/Close)

**Decision:** The `WebhookDispatcher` follows the Router lifecycle pattern established in Phase 7: `New() -> Start(ctx) -> defer Close()`.

- `New(store, cfg)` — creates the dispatcher with an HTTP client (30-second timeout) and buffered channel
- `Start(ctx)` — launches the delivery goroutine that reads from the channel
- `Close()` — signals the delivery goroutine to stop, drains remaining events from the channel, and waits for the goroutine to exit via `sync.WaitGroup`

In `main.go`, the dispatcher is created after storage and config are initialized, started alongside the router, and deferred for cleanup:

```go
dispatcher := webhook.NewDispatcher(store, cfg)
dispatcher.Start(ctx)
defer dispatcher.Close()
```

The dispatcher reference is passed to `api.NewRouter()` so that handlers can send events to it.

### D-08: HMAC-SHA256 Signing

**Decision:** Every webhook delivery is HMAC-SHA256 signed using the webhook's configured secret. The signature is sent as an `X-Webhook-Signature` header with the format `sha256=<hex-digest>`. The HMAC is computed over the raw JSON payload body bytes.

```go
func signPayload(secret string, payload []byte) string {
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(payload)
    return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
```

If a webhook has no configured secret (empty string), the `X-Webhook-Signature` header is omitted. This allows receivers that do not need signature verification to skip the check entirely.

**Verification on the receiver side** (not implemented in this project, documented for reference):

```python
import hmac, hashlib
expected = "sha256=" + hmac.new(secret.encode(), body, hashlib.sha256).hexdigest()
assert hmac.compare_digest(expected, request.headers["X-Webhook-Signature"])
```

### D-09: Secret Sourcing

**Decision:** YAML webhooks use the `secret` field from config, which supports `os.environ/VAR` expansion already handled by `config/loader.go`'s `expandEnvVar()`. UI-created webhooks store the secret in the `webhook_subscriptions.secret` column as plaintext.

**Security consideration:** Webhook secrets are shared secrets for HMAC computation — they must be available in plaintext for signing. This is fundamentally different from API keys (which can be hashed). Storing webhook secrets in plaintext in SQLite is the correct approach for this use case. The `secret` column is nullable; a NULL or empty secret means unsigned deliveries.

### D-10: YAML/DB Webhook Merging at Dispatch Time

**Decision:** At dispatch time, the `WebhookDispatcher` merges both webhook sources:

1. In-memory YAML webhooks from `cfg.Webhooks` — always available, no DB query needed
2. DB webhooks queried from `webhook_subscriptions` via `store.GetEnabledWebhooksByEvent(eventType)`

Both sources are filtered by event type match before delivery. The dispatcher iterates over the merged set and delivers the event to each matching webhook independently. A failure to deliver to one webhook does not affect delivery to other webhooks.

No sync occurs between YAML and DB sources. They are independent, as established in ADR 005 Decision 4.

**Optimization:** DB webhooks are queried once per event (not once per webhook). The `GetEnabledWebhooksByEvent` method returns all enabled DB webhooks that subscribe to the given event type in a single query.

### D-11: Source and Read-Only Fields in API Responses

**Decision:** Admin API responses for webhook listing include computed fields:

- `source` — `"yaml"` for YAML-configured webhooks, `"ui"` for DB-managed webhooks
- `read_only` — `true` for YAML webhooks, `false` for DB webhooks

These fields are computed at API response time, not stored in the database. This follows ADR 005's design: the `webhook_subscriptions` table has no `source` column because everything in that table is UI-created by definition.

YAML webhooks are assigned synthetic negative IDs (`-1`, `-2`, etc.) in API responses to distinguish them from DB webhooks (which have positive auto-incremented IDs). This allows the frontend to reliably identify YAML webhooks without a separate field.

### D-12: Webhook CRUD Endpoints

**Decision:** Five Admin API endpoints for webhook management:

| Method | Path | Purpose | YAML Behavior |
|---|---|---|---|
| `GET` | `/admin/webhooks` | List all webhooks (YAML + DB merged) | Included with `source: "yaml"`, `read_only: true` |
| `POST` | `/admin/webhooks` | Create a UI webhook | N/A |
| `GET` | `/admin/webhooks/:id` | Get a single webhook | 403 for negative (YAML) IDs |
| `PUT` | `/admin/webhooks/:id` | Update a UI webhook | 403 with "YAML webhooks are read-only" message |
| `DELETE` | `/admin/webhooks/:id` | Delete a UI webhook | 403 with "YAML webhooks cannot be deleted" message |

YAML webhooks are protected by returning HTTP 403 with a descriptive error body on any modification attempt. The handler checks for negative IDs (YAML) before proceeding with DB operations.

All endpoints are protected by the existing master key authentication middleware (same as other `/admin/*` routes).

### D-13: Notification Events Feed Endpoint

**Decision:** A paginated notification events feed endpoint:

```
GET /admin/events?limit=50&offset=0&event_type=provider_failover
```

- `limit` — maximum events to return (default 50, max 1000)
- `offset` — pagination offset (default 0)
- `event_type` — optional filter; when present, only events of this type are returned

Events are returned ordered by `created_at DESC` (newest first). The response includes a `total` count for pagination:

```json
{
  "events": [...],
  "total": 142,
  "limit": 50,
  "offset": 0
}
```

Each event in the response includes `id`, `event_type`, `payload` (parsed from JSON text to object), and `created_at` timestamp.

### D-14: Storage Interface Extension (10 Methods)

**Decision:** The `storage.Storage` interface is extended with 10 methods across three tables:

**Webhook subscriptions (5 methods):**
- `ListWebhookSubscriptions() ([]WebhookSubscription, error)` — all subscriptions, for admin listing
- `CreateWebhookSubscription(sub *WebhookSubscription) (int64, error)` — insert, return ID
- `UpdateWebhookSubscription(sub *WebhookSubscription) error` — update by ID
- `DeleteWebhookSubscription(id int64) error` — delete by ID
- `GetEnabledWebhooksByEvent(eventType string) ([]WebhookSubscription, error)` — enabled subscriptions matching event type, for dispatch

**Notification events (3 methods):**
- `InsertNotificationEvent(event *NotificationEvent) (int64, error)` — insert, return ID
- `ListNotificationEvents(limit, offset int, eventType string) ([]NotificationEvent, int, error)` — paginated with optional type filter, returns events + total count
- `DeleteOldNotificationEvents(olderThan time.Time) (int64, error)` — cleanup, returns rows deleted

**Webhook deliveries (2 methods):**
- `InsertWebhookDelivery(delivery *WebhookDelivery) (int64, error)` — insert, return ID
- `UpdateWebhookDeliveryStatus(id int64, status string, responseCode int, attemptCount int) error` — update attempt tracking

### D-15: 30-Day Event Retention Cleanup

**Decision:** A background goroutine deletes `notification_events` older than 30 days. The cleanup runs once every 24 hours on a ticker. It can be integrated into the `WebhookDispatcher`'s delivery goroutine (since that goroutine already runs for the lifetime of the process) using a secondary ticker.

The cleanup calls `store.DeleteOldNotificationEvents(time.Now().UTC().Add(-30 * 24 * time.Hour))`, which executes:

```sql
DELETE FROM notification_events WHERE created_at < ?
```

The 30-day retention period matches the design established in ADR 005 (notification_events table documentation).

### D-16: Cascade Delete via Foreign Key

**Decision:** `webhook_deliveries` rows for events deleted by the retention cleanup are cascade-deleted via the FK constraint defined in the migration:

```sql
REFERENCES notification_events(id) ON DELETE CASCADE
```

No application-layer code is needed to clean up delivery records — SQLite handles it automatically when notification events are deleted. This is the correct approach because delivery records have no value after their associated event is purged.

---

## Alternatives Considered

### Synchronous webhook delivery in the handler

**Rejected.** Making an HTTP POST to a webhook endpoint synchronously in the request handler would add latency to every LLM response that triggers an event. A slow or unresponsive webhook endpoint would directly degrade proxy throughput. The async channel-based approach (D-05) decouples delivery from the request path entirely.

### Debounced event emission

**Rejected.** Debounce requires state tracking (last emission time per event type per model), adds complexity, and reduces visibility for receivers who want to track event frequency. Every-occurrence firing (D-03) is simpler and gives receivers complete data. Receivers that want debounce can implement it trivially on their side.

### Separate delivery worker goroutine pool

**Considered.** A pool of N worker goroutines reading from the event channel would increase delivery throughput. Rejected for v1.1: a single delivery goroutine with sequential processing is sufficient for the expected event volume (routing events are infrequent relative to request volume). The channel buffer (256 entries) provides burst absorption. If delivery throughput becomes a bottleneck, the single goroutine can be replaced with a worker pool without changing the channel-based interface.

### Storing YAML webhook source in the DB

**Rejected.** This was already rejected in ADR 005 Decision 4. Writing YAML webhooks to the DB at startup creates sync problems on restart if the YAML changes. The in-memory approach eliminates this entirely.

### Webhook payload as a flat key-value map

**Rejected.** A flat map loses the structured metadata (arrays like `providers_tried`, nested objects). The dual-layer approach (D-04) with IFTTT-compatible `value1`/`value2`/`value3` fields plus a structured `context` object serves both simple and advanced consumers.

---

## Consequences

- **Webhook delivery is eventually-consistent.** Events are delivered asynchronously via a buffered channel. If the channel is full (dispatcher backed up), events are dropped with a warning log. The system prioritizes request throughput over event completeness.
- **YAML webhooks are truly immutable via API.** The only way to change a YAML webhook is to modify the config file and reload/restart the proxy. The Admin API returns 403 on any modification attempt.
- **No deduplication.** Receivers must handle duplicate events. The proxy does not guarantee exactly-once delivery — network retries, process restarts, and other edge cases may produce duplicate deliveries.
- **Migration fix required.** The `webhook_deliveries` table from Phase 4 needs `subscription_id` made nullable and ON DELETE CASCADE added to both FKs. This is implemented as a drop-and-recreate migration (migrations 30-32), which is acceptable since no production data exists in these tables yet.
- **Storage interface grows by 10 methods.** This follows the established pattern from Phases 4, 5, 7, and 8 where each phase extends `storage.Storage` with methods for its domain tables.
- **WebhookDispatcher is a new lifecycle component.** Like the Router and PoolBudgetManager, it follows the `New() -> Start(ctx) -> defer Close()` pattern. It must be wired into `main.go` alongside the existing lifecycle components.

---

## Implementation Files

| File | Role |
|------|------|
| `internal/webhook/dispatcher.go` | WebhookDispatcher: channel, delivery loop, retry, HMAC signing (D-05, D-06, D-07, D-08) |
| `internal/webhook/events.go` | Event types, payload builders, NotificationEvent struct (D-02, D-04) |
| `internal/webhook/signing.go` | HMAC-SHA256 signing helper (D-08, D-09) |
| `internal/storage/storage.go` | Storage interface extension with 10 new methods (D-14) |
| `internal/storage/sqlite/webhooks.go` | SQLite implementations for webhook/notification/delivery methods (D-14) |
| `internal/storage/sqlite/migrations.go` | Migration fix for webhook_deliveries (nullable subscription_id, cascade FKs) |
| `internal/api/handler/webhooks.go` | Webhook CRUD handlers (D-12) |
| `internal/api/handler/events.go` | Notification events feed handler (D-13) |
| `internal/api/handler/chat.go` | Event emission after Route() returns (D-01) |
| `internal/api/router.go` | Route registration for /admin/webhooks and /admin/events |
| `cmd/proxy/main.go` | Dispatcher lifecycle: New(), Start(), defer Close() (D-07) |

---

## References

- `adr/005-schema-config-foundation.md` — Phase 4 schema decisions: webhook_subscriptions (no source column), notification_events (30-day retention), webhook_deliveries (FK cascade), YAML/DB webhook separation (Decision 4)
- `adr/006-streaming-backoff.md` — BackoffManager full-jitter exponential backoff pattern (D-05) referenced for webhook retry design
- `.planning/phases/09-webhooks-notifications/09-CONTEXT.md` — All 16 decisions formalized in this ADR
- `internal/router/route.go` — RouteResult struct, FailoverReason enum, Route() method
- `internal/router/headers.go` — SetRouteHeaders integration point for event emission
- `internal/config/config.go` — WebhookConfig struct, Config.Webhooks field
