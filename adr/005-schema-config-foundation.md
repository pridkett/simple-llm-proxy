# ADR 005: Schema and Config Foundation for v1.1 Provider Pools and Webhooks

**Status:** Accepted
**Date:** 2026-03-27
**ADR Issue:** pridkett/simple-llm-proxy#31

---

## Context

Phase 4 is the schema and config foundation for v1.1 provider pools, webhook subscriptions, and enhanced cost tracking. All downstream phases (5–10) depend on the stable schema contracts established here.

The proxy currently operates with a single-pool routing model: a flat `model_list` where each entry is an individual deployment, and the global router applies a single strategy (simple-shuffle or round-robin) across all deployments for a given model name. v1.1 introduces multi-endpoint provider pools with weighted routing, budget caps, failover, sticky sessions, and outbound webhook notifications for routing events. These features require significant schema extensions and new YAML config constructs.

**NOT in scope for this ADR:** Any behavior changes — no routing logic, no provider code, no webhook delivery. Phase 4 is pure schema and config plumbing. This ADR documents the structural decisions that all downstream implementation phases build against.

**Success gate:** The proxy starts against a v1.0 database, applies all new migrations without error, parses a YAML config with `provider_pools:` and `webhooks:` sections, and exposes the new config structs to downstream packages.

This ADR documents all architectural decisions for Phase 4 before any implementation code is written, as required by the ADR-first mandate in CLAUDE.md.

---

## Decision

### Decision 1: Migration Numbering Strategy

New migrations continue the existing sequence from migration 13. All migrations in Phase 4 occupy slots 14 through 21. Downstream phases (5–10) reserve migration slots 22–29 for their own schema additions.

**Idempotency rule:** All new table creation statements use `CREATE TABLE IF NOT EXISTS`. This ensures the migration can be replayed safely if the migration tracking table ever gets out of sync.

**Exception — usage_logs recreation:** The `usage_logs` table uses `DROP TABLE IF EXISTS` followed by `CREATE TABLE` (without `IF NOT EXISTS`). This is intentional: the drop-and-recreate pattern is required to rename columns (`prompt_tokens` → `input_tokens`, `completion_tokens` → `output_tokens`), which SQLite does not support via `ALTER TABLE RENAME COLUMN` in older `modernc.org/sqlite` versions. The same pattern was applied to `api_keys` in migration 11, establishing a precedent for this project.

**Rationale:** SQLite does not support `ALTER TABLE DROP COLUMN` or `ALTER TABLE RENAME COLUMN` portably across all versions. For schema changes that require column renames or removals, the drop-and-recreate approach is the most reliable path — especially for tables where no production data exists.

### Decision 2: usage_logs Recreation (Migrations 14–15)

The `usage_logs` table is fully dropped and recreated across two migrations:

- **Migration 14:** `DROP TABLE IF EXISTS usage_logs` — removes the v1.0 table along with its indexes and triggers (if any).
- **Migration 15:** `CREATE TABLE usage_logs (...)` — recreates the table with the v1.1 schema and all required indexes.

**Column changes from v1.0:**

| v1.0 Column | v1.1 Column | Change |
|-------------|-------------|--------|
| `prompt_tokens INTEGER` | `input_tokens INTEGER NOT NULL DEFAULT 0` | Renamed; NOT NULL added |
| `completion_tokens INTEGER` | `output_tokens INTEGER NOT NULL DEFAULT 0` | Renamed; NOT NULL added |
| — | `is_streaming BOOLEAN NOT NULL DEFAULT 0` | New |
| — | `cache_read_tokens INTEGER NOT NULL DEFAULT 0` | New |
| — | `cache_write_tokens INTEGER NOT NULL DEFAULT 0` | New |
| — | `deployment_key TEXT` | New (nullable) |

All other columns (`id`, `request_id`, `api_key_id`, `model`, `provider`, `endpoint`, `total_cost`, `status_code`, `latency_ms`, `request_time`) are retained with their existing types.

**History discard:** The v1.0 usage history is intentionally discarded. No production data existed at the time of schema migration — the system had not yet reached a production deployment. If production data had existed, an `INSERT INTO new_usage_logs SELECT ...` migration would be required. That path is not needed here.

**deployment_key column:** This nullable TEXT column identifies which pool deployment served the request. It is populated in Phase 5. It is NULL for any rows written before Phase 5 is applied.

### Decision 3: New Table Schema (Migrations 16–21)

Seven new tables are created in migrations 16 through 21. All tables sit **dormant** until activated by downstream phases. The tables are created now to establish the stable schema contract that downstream phases implement against.

#### Migration 16: provider_pools

```sql
CREATE TABLE IF NOT EXISTS provider_pools (
    id               INTEGER  PRIMARY KEY AUTOINCREMENT,
    name             TEXT     NOT NULL UNIQUE,
    strategy         TEXT     NOT NULL DEFAULT 'weighted-round-robin',
    budget_cap_daily REAL,
    created_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);
```

Named pools with an optional daily budget cap. `strategy` values: `weighted-round-robin`, `round-robin`, `shuffle`. A NULL `budget_cap_daily` means unlimited. This table is primarily used by Phase 7 (pool routing) and Phase 8 (budget tracking). Pool config sourced from YAML at startup; whether pools are reflected into this table or remain purely in-memory is deferred to Phase 7.

#### Migration 17: routing_rules

```sql
CREATE TABLE IF NOT EXISTS routing_rules (
    id         INTEGER  PRIMARY KEY AUTOINCREMENT,
    pool_name  TEXT     NOT NULL,
    model_name TEXT     NOT NULL,
    weight     INTEGER  NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(pool_name, model_name)
);
```

Per-pool, per-member weight overrides. `pool_name` and `model_name` together form the join key. `UNIQUE(pool_name, model_name)` ensures at most one weight entry per member. This design is chosen over a JSON column in `provider_pools` to allow per-member updates without rewriting the entire pool record.

#### Migration 18: webhook_subscriptions

```sql
CREATE TABLE IF NOT EXISTS webhook_subscriptions (
    id         INTEGER  PRIMARY KEY AUTOINCREMENT,
    url        TEXT     NOT NULL,
    events     TEXT     NOT NULL,
    secret     TEXT,
    enabled    BOOLEAN  NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
```

UI-created webhooks only. YAML-configured webhooks are never written to this table — they are held in memory (see Decision 4). The `events` column stores a comma-separated or JSON-encoded list of event type strings. A `source` column is intentionally omitted: everything in this table is UI-created by definition; the YAML/DB separation is enforced at the application layer, not by a column value.

#### Migration 19: notification_events

```sql
CREATE TABLE IF NOT EXISTS notification_events (
    id         INTEGER  PRIMARY KEY AUTOINCREMENT,
    event_type TEXT     NOT NULL,
    payload    TEXT     NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_notification_events_created_at
    ON notification_events(created_at);

CREATE INDEX IF NOT EXISTS idx_notification_events_event_type
    ON notification_events(event_type);
```

Routing event log. Events are retained for 30 days; a background cleanup goroutine (Phase 9) purges rows where `created_at < datetime('now', '-30 days')`. The `payload` column stores the event body as a JSON blob (TEXT type). This avoids a proliferating set of columns for diverse event payloads. Events are indexed by `created_at` for time-range queries and by `event_type` for filtered queries.

#### Migration 20: webhook_deliveries

```sql
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id               INTEGER  PRIMARY KEY AUTOINCREMENT,
    subscription_id  INTEGER  NOT NULL REFERENCES webhook_subscriptions(id) ON DELETE CASCADE,
    event_id         INTEGER  NOT NULL REFERENCES notification_events(id) ON DELETE CASCADE,
    attempt_count    INTEGER  NOT NULL DEFAULT 0,
    last_attempt_at  DATETIME,
    status           TEXT     NOT NULL DEFAULT 'pending',
    response_code    INTEGER,
    created_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_subscription_id
    ON webhook_deliveries(subscription_id);
```

Delivery queue with retry tracking. `status` values: `pending`, `delivered`, `failed`. FKs to `webhook_subscriptions` and `notification_events` cascade on delete. Indexed on `subscription_id` for per-webhook delivery history queries. The retry strategy (max attempts, backoff) is deferred to Phase 9.

#### Migration 21: pool_budget_state

```sql
CREATE TABLE IF NOT EXISTS pool_budget_state (
    pool_name   TEXT NOT NULL PRIMARY KEY,
    spend_today REAL NOT NULL DEFAULT 0,
    reset_date  DATE NOT NULL
);
```

Per-pool daily spend accumulator. `pool_name` is the PRIMARY KEY — one row per pool. `reset_date` stores the UTC date (format `YYYY-MM-DD`) for which `spend_today` was last reset. Phase 8 reads and writes this table. At budget check time: if `reset_date < today (UTC)`, reset `spend_today = 0` and update `reset_date`.

#### Migration 22: sticky_routing_sessions

```sql
CREATE TABLE IF NOT EXISTS sticky_routing_sessions (
    session_key    TEXT     NOT NULL PRIMARY KEY,
    pool_name      TEXT     NOT NULL,
    deployment_key TEXT     NOT NULL,
    last_used_at   DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_sticky_routing_sessions_last_used_at
    ON sticky_routing_sessions(last_used_at);
```

Client-to-deployment session affinity. `session_key` is the PRIMARY KEY (typically a client-supplied header or derived from API key + user agent). `last_used_at` is indexed for expiry cleanup — a background goroutine (Phase 7) expires sessions inactive for more than 1 hour. The Phase 7 implementation determines the exact session key derivation strategy.

**Note:** This table occupies migration slot 22, not 21, to reserve a contiguous block of slots 14–21 for Phase 4 tables and allow Phase 4 and Phase 5 migrations to be committed independently without slot conflicts.

### Decision 4: YAML Webhook vs DB Webhook Separation

YAML-configured webhooks (declared in the `webhooks:` section of `config.yaml`) are held in memory at startup and are **never written to the `webhook_subscriptions` table**.

DB webhooks are created, updated, and deleted through the Admin UI via the Admin API. They live in `webhook_subscriptions` and are mutable at runtime.

At read/fire time, the Admin API and webhook delivery system merge both sources:
- YAML webhooks are surfaced with a `"source": "yaml"` label (read-only in the UI)
- DB webhooks are surfaced with a `"source": "ui"` label (editable in the UI)

**Rationale for separation:**

If YAML webhooks were written to the DB at startup, a sync problem arises: what happens if the YAML changes between restarts? Should existing DB rows be updated, deleted, or left as-is? Any answer requires startup sync logic that can corrupt UI-managed changes. The in-memory approach eliminates this entirely — the YAML is the source of truth for YAML webhooks; the DB is the source of truth for UI webhooks; they never overlap.

### Decision 5: deployment_key Format

The `deployment_key` is a string derived as `"provider:model:api_base"`:

- `provider`: the provider name as registered (e.g., `openai`, `anthropic`)
- `model`: the actual upstream model identifier (e.g., `gpt-4`, `claude-3-5-sonnet-20241022`)
- `api_base`: the `api_base` field from the model config, or empty string if not set

**When `api_base` is empty**, the key is `"provider:model:"` — the trailing colon is preserved to maintain a consistent three-field format. This avoids ambiguity between `"provider:model"` (two fields) and `"provider:model:"` (three fields, empty third).

**Stability requirement:** The `deployment_key` must be stable across config reloads — it is keyed by string value, not by pointer. Two `ModelConfig` instances representing the same deployment will produce identical `deployment_key` strings. This is required because the BackoffManager, PoolBudgetTracker, and sticky session lookup in downstream phases all use `deployment_key` as their map key.

**Derivation point:** `deployment_key` is derived in `router.New()` during deployment initialization, not in the provider itself. This keeps the derivation logic in one place and avoids coupling the provider implementations to the routing layer's identity scheme.

### Decision 6: provider_pools YAML Config Shape

Provider pools are declared in a new top-level `provider_pools:` section in `config.yaml`, parallel to the existing `model_list:`:

```yaml
model_list:
  - model_name: gpt-4-primary
    litellm_params:
      model: openai/gpt-4
      api_key: os.environ/OPENAI_API_KEY
  - model_name: gpt-4-fallback
    litellm_params:
      model: openai/gpt-4
      api_key: os.environ/OPENAI_API_KEY_FALLBACK

provider_pools:
  - name: gpt-4
    strategy: weighted-round-robin
    budget_cap_daily: 50.00
    members:
      - model_name: gpt-4-primary
        weight: 80
      - model_name: gpt-4-fallback
        weight: 20
```

`model_name` under `members` references a `model_list` entry. Standalone `model_list` entries without pool membership continue to be routed by the global router strategy. All existing `model_list`-only configs work without modification.

**Loader implementation:** The existing `loader.go` uses raw-map YAML parsing (not struct-tag-driven unmarshaling). The `provider_pools:` and `webhooks:` sections follow the same pattern: an `if section, ok := raw["provider_pools"].([]any)` block extracts each pool and its members into `ProviderPool` and `PoolMember` structs.

### Decision 7: Startup Validation

At startup (and on config reload), the proxy validates:

1. **Pool member references:** Every `pool.member.model_name` must have a corresponding entry in `model_list`. If a reference is missing, startup fails with a descriptive error: `"provider pool 'gpt-4': member model 'gpt-4-primary' not found in model_list"`.

2. **YAML webhook integrity:** Every YAML webhook must have a non-empty `url` and a non-empty `events` slice. An empty or missing `url` causes: `"webhook config at index N: url is required"`. An empty `events` slice causes: `"webhook config at index N: events list is required"`.

Validation runs in `router.New()` after all config structs are populated. Invalid configs cause startup failure rather than silent misconfiguration. The same validation is invoked on config reload to prevent runtime injection of invalid pool or webhook configs.

---

## Alternatives Considered

### ALTER TABLE ADD COLUMN for usage_logs schema changes

**Rejected.** SQLite supports `ALTER TABLE ADD COLUMN` for adding new columns, but does not support renaming or dropping existing columns portably. Since the v1.1 schema renames `prompt_tokens` → `input_tokens` and `completion_tokens` → `output_tokens`, `ALTER TABLE ADD COLUMN` alone would leave the old column names alongside the new names, creating confusion for all downstream query code. Query paths would need to handle both column names for backward compat. The drop-and-recreate approach eliminates this ambiguity cleanly. Because no production data existed in usage_logs at migration time, no data migration path is required.

### Storing YAML webhooks in the DB at startup

**Rejected.** Persisting YAML webhooks to `webhook_subscriptions` at startup creates a sync problem: if the YAML file is modified between restarts (URL changed, event type added), the DB will contain stale rows. Options for resolving this — delete all YAML-sourced rows and re-insert, update matching rows by URL, or leave conflicting rows — all require startup logic that is fragile and can corrupt UI-managed changes. The in-memory separation is simpler, stateless, and free of the sync problem. The Admin API merges both sources at read time, which is sufficient for the v1.1 use case.

### JSON column for routing_rules weights in provider_pools

**Rejected.** Storing per-member weights as a JSON blob in the `provider_pools` row would require deserializing and reserializing the entire JSON to update a single member's weight. It would also make per-member SQL queries (e.g., "find all pools where gpt-4-fallback has weight > 0") impossible without JSON function support. The separate `routing_rules` table with `UNIQUE(pool_name, model_name)` allows targeted per-member updates and standard SQL queries. The join cost is negligible — pool configs are loaded at startup and held in memory.

### Deriving deployment_key in the provider package

**Rejected.** Placing the derivation in `internal/provider/` would require the provider package to know about the routing layer's identity scheme. The routing layer is the consumer of `deployment_key` (BackoffManager, budget tracker, sticky sessions). Deriving it in `router.New()` keeps the coupling in the correct direction: the router knows how to identify its deployments; providers do not.

---

## Consequences

- **Tables sit dormant until activated by downstream phases.** The schema is established in Phase 4 but no application code reads or writes the new tables until the relevant phases are implemented. This ensures schema stability without premature coupling.
- **Phase 5 wires deployment_key into usage_logs population.** Every request log written after Phase 5 will include the `deployment_key` identifying the serving deployment. Rows written before Phase 5 will have NULL `deployment_key`.
- **Phase 7 activates pool routing.** The `provider_pools`, `routing_rules`, and `sticky_routing_sessions` tables become active. Phase 7 also decides whether pool config is reflected into the DB or remains purely in-memory.
- **Phase 8 activates budget tracking.** The `pool_budget_state` table becomes active. Daily spend accumulation and budget enforcement use this table.
- **Phase 9 activates webhook delivery.** The `webhook_subscriptions`, `notification_events`, and `webhook_deliveries` tables become active. Phase 9 implements the delivery queue, retry logic, and 30-day event retention cleanup.
- **Existing configs are backward-compatible.** No changes to `model_list:` format; no new required fields. A config without `provider_pools:` or `webhooks:` continues to work exactly as before.
- **All new Go struct types are in the config package.** Downstream packages import `config.ProviderPool`, `config.PoolMember`, and `config.WebhookConfig` — there is no circular dependency risk.

---

## Implementation Files

| File | Role |
|------|------|
| `internal/storage/sqlite/migrations.go` | Migrations 14–22: usage_logs recreation, 7 new tables, indexes |
| `internal/config/config.go` | New types: `ProviderPool`, `PoolMember`, `WebhookConfig`; extend `Config` struct |
| `internal/config/loader.go` | Parse blocks for `provider_pools:` and `webhooks:` sections |
| `internal/router/router.go` | `deployment_key` derivation in `New()`; startup validation for pool members and YAML webhooks |

---

## References

- `.planning/phases/04-schema-config-foundation/04-CONTEXT.md` — Locked decisions D-01 through D-11
- `.planning/phases/04-schema-config-foundation/04-RESEARCH.md` — Migration sequence details and schema design
- `internal/storage/sqlite/migrations.go` — Migration sequence; new migrations start at 14
- `adr/004-api-keys-enforcement.md` — Phase 2 ADR; migration 11 drop-and-recreate pattern referenced in Decision 1
