# ADR 004: API Keys and Enforcement Architecture

**Status:** Accepted
**Date:** 2026-03-25
**ADR Issue:** pridkett/simple-llm-proxy#TBD

---

## Context

The proxy currently accepts only the master key on `/v1/*`. Any client holding the master key can use any model with no budget or rate controls. This is appropriate for the initial bootstrap and admin operations, but as teams onboard multiple applications, the following gaps become significant:

- No per-application identity: all machine client requests share a single credential
- No model access controls: any key holder can call any configured model
- No spend controls: a runaway application can exhaust the entire upstream API budget
- No rate limits: a single misbehaving client can starve other applications
- No audit trail by application: impossible to answer "which app spent $50 this week?"

Phase 2 introduces per-application API keys that allow each application to have its own scoped credential with configurable model allowlists, rate limits (RPM/RPD), and hard budget caps. The enforcement must not materially increase per-request latency. Every `/v1/*` request must be validated without a synchronous DB lookup on the hot path.

This ADR documents all architectural decisions for Phase 2 (API Keys & Enforcement) before any implementation code is written, as required by the ADR-first mandate in CLAUDE.md.

---

## Decision

### Decision 1: Schema — api_keys Table (D-01, D-02)

Drop the placeholder `api_keys` table stub (created in migration 1) and recreate it with the full Phase 2 schema in new migrations. This is safe because no production key data exists in the current table.

New `api_keys` table fields:

```sql
CREATE TABLE api_keys (
    id             INTEGER  PRIMARY KEY AUTOINCREMENT,
    application_id INTEGER  NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    name           TEXT     NOT NULL,
    key_prefix     TEXT     NOT NULL,          -- first 8 chars after sk-app- for display
    key_hash       TEXT     NOT NULL UNIQUE,   -- SHA-256 of full key
    max_rpm        INTEGER,                    -- NULL = unlimited
    max_rpd        INTEGER,                    -- NULL = unlimited
    max_budget     REAL,                       -- hard cap in USD, NULL = unlimited
    soft_budget    REAL,                       -- alert threshold in USD, NULL = none
    is_active      BOOLEAN  NOT NULL DEFAULT TRUE,
    created_at     DATETIME NOT NULL DEFAULT (datetime('now'))
);
```

The existing `api_keys` stub is dropped and recreated because the previous migration was a placeholder with an incomplete schema. No production data is at risk.

### Decision 2: Schema — Model Allowlists (D-03)

Model allowlists are stored in a separate `key_allowed_models` table rather than a JSON column:

```sql
CREATE TABLE key_allowed_models (
    key_id     INTEGER NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    model_name TEXT    NOT NULL,
    PRIMARY KEY (key_id, model_name)
);
```

An empty set (no rows for a given `key_id`) means all models are allowed. The normalized table is queryable and avoids JSON parsing on the enforcement path.

### Decision 3: Schema — usage_logs Integration (D-04)

The `usage_logs` table already has an `api_key_id` FK column (added in Phase 1 migrations). This column is retained and the FK target is updated to reference the new `api_keys` table. No new column is required; the FK relationship is preserved.

### Decision 4: Key Format and Storage (D-12, D-13)

**Format:** `sk-app-{48 hex chars}` (e.g., `sk-app-a3f9b2c1d4e5...`). The `sk-app-` prefix makes proxy-issued keys visually distinct from upstream provider keys (`sk-` for OpenAI), reducing the risk of accidentally using a proxy key as an OpenAI key or vice versa.

**Generation:** 24 random bytes from `crypto/rand`, hex-encoded to produce the 48-char suffix. This yields 192 bits of entropy — sufficient to make brute-force infeasible regardless of hashing algorithm.

**Storage:** Backend stores only:
1. `key_prefix` — first 8 chars of the hex suffix, used for display in the keys list (e.g., `sk-app-a3f9b2c1...`)
2. `key_hash` — SHA-256 of the full plaintext key

SHA-256 is chosen over bcrypt for key hashing. Key lookup is on the hot path (every `/v1/*` request). bcrypt's deliberate computational cost (designed to slow down password brute-force) would add 50–300ms per request. With 192 bits of random entropy in the key, the security property is provided by the key's entropy, not the hash function's slowness. SHA-256 is deterministic and sub-millisecond.

**Non-retrievability:** The full plaintext key is returned once in the creation API response. The backend never stores it in recoverable form. After creation, the key cannot be retrieved — only revoked and replaced.

### Decision 5: Enforcement Middleware — KeyAuth (D-05, D-06, D-10)

The `/v1/*` route group accepts both the master key and per-app keys. A new `KeyAuth` middleware replaces `Auth()` on `/v1/*`.

**Master key path:** If the `Authorization: Bearer` value matches the master key, the request proceeds with no enforcement (rate limits, budgets, model restrictions are all bypassed). The master key is an admin/testing credential for the proxy operator.

**Per-app key path:** KeyAuth computes SHA-256 of the bearer token. If the hash matches a record in the key cache:
1. Load the full key record (limits, allowlist) from cache
2. Inject an `APIKey` struct into the request context
3. Downstream handlers read from context — no extra DB lookups per proxied request

**Enforcement response codes (D-10):**
- Model not in allowlist → `403` with body `{"error": "model_not_allowed", "type": "permission_error"}`
- Rate limit exceeded (RPM or RPD) → `429` with body `{"error": "rate_limit_exceeded", "type": "rate_limit_error"}`
- Hard budget exceeded → `429` with body `{"error": "budget_exceeded", "type": "budget_limit_error"}`
- Missing or invalid key → `401`

The model allowlist check is performed in the handler after reading the injected `APIKey` from context.

### Decision 6: In-Memory Enforcement Engine — keystore Package (D-07, D-08, D-09)

A new `internal/keystore/` package provides three components. All are in-process, stdlib-only — no new external dependencies.

#### Cache (cache.go)

TTL-based key cache (default TTL: 60 seconds). On cache miss, the key is loaded from SQLite and cached for the TTL duration. On revoke, the cache entry is immediately invalidated via `cache.Invalidate(keyID)` — the key does not remain valid until TTL expiry after revocation.

The cache is interface-backed to allow future replacement with a distributed store (e.g., Redis) without changing callers. In the current single-instance deployment, the in-process LRU/TTL cache is sufficient.

#### RateLimiter (counters.go)

`sync.Map` of `atomic.Int64` counters keyed by `(keyID, window)`, where window is either:
- Current UTC minute string for RPM enforcement
- Current UTC date string for RPD enforcement

Enforcement: before proxying, increment the counter and compare against the key's `max_rpm` / `max_rpd` limits. If exceeded, return 429 immediately.

Counters reset on process restart. This is an accepted tradeoff for a single-instance small team deployment — a restart clears the rate limit state for the current minute/day. The counter abstraction does not preclude future replacement with distributed rate limiting (e.g., Redis `INCR` with `EXPIRE`).

#### SpendAccumulator (spend.go)

Per-key running spend total maintained in memory. At startup, the accumulator is initialized from `SUM(total_cost)` in `usage_logs` grouped by `api_key_id`. This ensures the accumulator reflects all historical spend, not just spend since last restart.

`AcceptSpend(keyID, cost float64) bool` checks whether `current_total + cost` would exceed the key's `max_budget`. If so, it returns false and the request is rejected with 429. If the budget is not exceeded, the cost is added to the running total and the method returns true.

The accumulator is periodically flushed to the database (see Decision 7). `usage_logs` is the authoritative source of truth; the accumulator is a performance optimization that avoids per-request DB writes on the hot path.

### Decision 7: Spend Flush Loop (D-09)

A background goroutine in `cmd/proxy/main.go` flushes the in-memory spend accumulator to `usage_logs` every 30 seconds (configurable via `general_settings.spend_flush_interval`).

**Flush implementation:** For each key with pending spend delta since last flush, the goroutine inserts or updates a summary row in `usage_logs` recording the accumulated cost. The exact SQL is an `INSERT OR REPLACE` or `UPDATE` pattern that does not require a row per request.

**Shutdown:** During graceful shutdown, a final flush is performed before process exit to minimize spend data loss. The flush is called before `db.Close()` in the shutdown sequence.

**Eventual consistency:** Spend totals are eventually consistent with up to a 30-second lag. Hard budget enforcement uses the in-memory accumulator, so a burst of concurrent requests within a flush window could briefly exceed the budget by the cost of one batch. This is an accepted tradeoff — exact to-the-cent budget enforcement would require synchronous DB writes on the hot path, which conflicts with the latency requirement.

### Decision 8: Cost Attribution (D-11)

Cost is recorded after a successful response, including streaming (applied at stream completion, not stream start).

`logRequest()` in the chat handler is extended to accept `apiKeyID *int64`. A `nil` value indicates a master-key request (no key attribution). A non-nil value indicates a per-app key request.

After the usage log entry is created, `sa.Credit(keyID, cost)` updates the in-memory spend accumulator. The order is:
1. Proxy the request to the upstream provider
2. Receive response (or complete streaming)
3. Calculate cost from token counts
4. `logRequest(ctx, req, resp, apiKeyID)`
5. If `apiKeyID != nil`: `spendAccumulator.Credit(*apiKeyID, cost)`

### Decision 9: Frontend Keys View (D-14, D-15, D-16, D-17)

**Navigation:** Keys view organized as Team → App → Keys drill-down, consistent with the Applications view from Phase 1. The user selects a team, then an application, then sees that application's keys.

**Key creation form:** A single form with all fields: name (required), model allowlist (multiselect from available models, optional — empty means all models allowed), RPM (optional), RPD (optional), hard budget (optional), soft budget (optional).

**Key display after creation:** After a successful creation API call, the full plaintext key is displayed exactly once in a modal with:
- A prominent "this won't be shown again" warning
- A copy-to-clipboard button
- An explicit "I've copied the key" dismiss action

Dismissing the modal removes the key from frontend memory. The modal cannot be reopened for the same key.

**Keys list columns:** key prefix (`sk-app-xxxxxxxx...`), name, model allowlist summary ("All models" or "N models"), current spend vs hard budget (e.g., `$4.20 / $10.00`), active/revoked status, revoke action button.

---

## Consequences

- **Sub-microsecond enforcement overhead per request.** Every `/v1/*` request passes through KeyAuth middleware, which performs: one sync.Map lookup (cache), one atomic increment (rate counter), one float64 comparison (budget). This is well below 1µs overhead — negligible relative to network and LLM latency.
- **Spend is eventually consistent.** The 30-second flush window means hard budget enforcement uses an in-memory total, not a DB total. A burst of concurrent requests within the flush window could briefly exceed the budget. This is an accepted tradeoff against per-request DB writes.
- **Counter state lost on restart.** RPM and RPD enforcement counters reset on process restart. For a single-instance small team deployment, this is acceptable — a restart does not meaningfully advantage an abusive client.
- **Key revocation is immediate per-instance.** `cache.Invalidate()` removes the key from the in-memory cache immediately. In a future multi-instance deployment, other instances would not see the revocation until their TTL expires (default 60s). This is a documented limitation of the current single-instance design.
- **No new external dependencies.** The keystore package uses only Go stdlib (`sync`, `crypto/sha256`, `time`). The existing `modernc.org/sqlite` handles all DB operations. No Redis, Memcached, or other external service is introduced.
- **SHA-256 key hashing is irreversible.** Once a key is created and the creation response is dismissed, the plaintext is unrecoverable. Users must revoke and create a new key if they lose the original. This is intentional — a lost key cannot be silently compromised.

---

## Alternatives Considered

### bcrypt for key hashing
**Rejected.** bcrypt's computational cost (50–300ms) is a feature for password hashing but a defect for hot-path key lookup. The 192-bit random entropy in the key provides the security property; bcrypt's slowness adds latency without adding meaningful security. SHA-256 is the correct choice for high-entropy random tokens on the request path.

### JSON column for model allowlists
**Rejected.** A JSON column would require parsing on every enforcement check and cannot be efficiently queried. The normalized `key_allowed_models` table allows indexed lookups and is consistent with the relational schema pattern used throughout this project.

### Synchronous DB write per request for spend tracking
**Rejected.** Writing to `usage_logs` on every request adds a DB round-trip to the hot path. The in-memory accumulator with periodic flush achieves the same eventual consistency with sub-microsecond hot-path cost. The DB remains the source of truth via the startup initialization and regular flush.

### Per-request cache bypass (always hit DB)
**Rejected.** Loading the full key record from SQLite on every request would add a DB read (~1–5ms) to every `/v1/*` request. The 60-second TTL cache amortizes this cost with acceptable staleness — revocation is handled by explicit cache invalidation, not TTL expiry.

### Key expiration dates
**Deferred to v2.** Key expiration (auto-revoke at a future datetime) is a useful operational feature but is not required for Phase 2 correctness. It is tracked as KEY-V2-01 and deferred to avoid scope creep.

---

## Implementation Files

| File | Role |
|------|------|
| `internal/storage/storage.go` | `APIKey` struct; new Storage interface methods: `CreateAPIKey`, `GetAPIKeyByHash`, `ListAPIKeys`, `RevokeAPIKey`, `GetKeyAllowedModels` |
| `internal/storage/sqlite/migrations.go` | Migrations 11–13: drop+recreate `api_keys`, `key_allowed_models` |
| `internal/storage/sqlite/apikeys.go` | SQLite implementations of key CRUD methods |
| `internal/keystore/cache.go` | TTL-based key cache with explicit invalidation |
| `internal/keystore/counters.go` | Rate limit counters (sync.Map + atomic int64) |
| `internal/keystore/spend.go` | SpendAccumulator: in-memory totals, AcceptSpend(), Credit(), Flush() |
| `internal/api/middleware/keyauth.go` | KeyAuth middleware: master key bypass, per-app key enforcement |
| `internal/api/handler/keys.go` | Admin handlers: create key, list keys, revoke key |
| `cmd/proxy/main.go` | keystore initialization, spend flush loop goroutine, shutdown flush |

---

## References

- `.planning/phases/02-api-keys-enforcement/02-CONTEXT.md` — Locked decisions D-01 through D-17 (source of truth for this phase)
- `adr/003-auth-identity-design.md` — Phase 1 identity ADR; Phase 2 builds on the `applications` entity and RBAC model defined there
- `internal/storage/storage.go` — Storage interface to extend
- `internal/storage/sqlite/migrations.go` — Migration sequence; new migrations appended as 11+
- `internal/api/router.go` — Route groups; `/v1/*` group middleware extended with KeyAuth
- `internal/api/middleware/auth.go` — Existing master key middleware (replaced by KeyAuth on `/v1/*`)
