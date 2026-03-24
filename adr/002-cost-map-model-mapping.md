# ADR 002: Cost Map Model Mapping Strategy

**Status:** Accepted
**Date:** 2026-03-24
**Issues:** pridkett/simple-llm-proxy#2, pridkett/simple-llm-proxy#3
**ADR Issue:** pridkett/simple-llm-proxy#12

---

## Context

Issue #1 integrated the LiteLLM cost/context map into the proxy (`costmap.Manager`). Issues #2 and #3 require surfacing that cost data in the user-facing models API (`GET /v1/models/{model}`) and in the admin frontend, along with the ability to override how a proxy model name resolves to a cost map entry.

### The Mapping Problem

The proxy uses a user-facing model name (e.g. `gpt-4`) that maps to one or more deployments, each with an `actual_model` field (e.g. `openai/gpt-4`). The LiteLLM cost map is keyed by `actual_model`-style strings. The two namespaces do not always match.

### Requirements

1. Expose cost info in `GET /v1/models/{model}` — always include a `costs` block (empty when no mapping).
2. Allow operators to override which cost map key is used for a proxy model.
3. Allow operators to define fully custom cost values for novel or unmapped models.
4. Persist overrides across server restarts.
5. Keep the `costmap` package free of `storage` imports (avoid import cycles; keep the package testable in isolation).

---

## Decision

### 1. Auto-Detection via `actual_model`

When no override exists, the system attempts to resolve cost data automatically by:

1. Taking each deployment's `actual_model` string (e.g. `gpt-4-turbo`)
2. Trying the `provider/actual_model` form (e.g. `openai/gpt-4-turbo`)
3. Returning the first match found in the LiteLLM cost map

This handles the common case where `actual_model` in the config matches LiteLLM's naming convention directly.

### 2. Two Override Modes

**Mode A — Cost Map Key Override:** The operator specifies which LiteLLM key to look up for a given proxy model name. For example, mapping `my-gpt4` → `openai/gpt-4`. This is useful when the proxy model name is an alias or when multiple deployments would resolve ambiguously.

**Mode B — Custom Spec:** The operator provides a fully custom `ModelSpec` (cost values, token limits, capabilities). This bypasses the LiteLLM cost map entirely and is intended for novel or private models not tracked in LiteLLM.

Only one mode can be active for a given model at a time; the most recent PATCH wins.

### 3. Persistence via SQLite

A new `cost_overrides` table stores per-model overrides:

```sql
CREATE TABLE IF NOT EXISTS cost_overrides (
    model_name   TEXT PRIMARY KEY,
    cost_map_key TEXT,      -- nullable: override key for LiteLLM lookup
    custom_spec  TEXT,      -- nullable: JSON-encoded custom ModelSpec
    updated_at   DATETIME NOT NULL DEFAULT (datetime('now'))
);
```

Exactly one of `cost_map_key` or `custom_spec` is non-null for any given row.

### 4. In-Memory Cache in `costmap.Manager`; No `storage` Import

`costmap.Manager` holds two in-memory maps:

- `overrideKeys map[string]string` — proxy model name → LiteLLM key
- `customSpecs map[string]ModelSpec` — proxy model name → custom spec

At server startup, `cmd/proxy/main.go` reads all rows from `cost_overrides` and seeds the Manager's in-memory maps. PATCH request handlers update both the SQLite row and the Manager's in-memory state atomically (from the handler's perspective — SQLite update then Manager update, with rollback on storage error).

This design preserves the clean separation: `costmap` has no knowledge of `storage`, and `storage` has no knowledge of `costmap`.

### 5. `GetEffectiveSpec` Resolution Method

A new method on `Manager` resolves cost data for a proxy model using a single read lock:

```go
type EffectiveSpecResult struct {
    Spec   ModelSpec
    Found  bool
    Source string // "custom" | "override" | "auto" | ""
    Key    string // the cost map key that matched
}

func (m *Manager) GetEffectiveSpec(proxyModel string, candidateActualModels []string) EffectiveSpecResult
```

Precedence:
1. Custom spec → `Source="custom"`
2. Override key lookup in cost map → `Source="override"`
3. Auto-detection through candidate actual model strings → `Source="auto"`
4. Not found → `Found=false`, `Source=""`

The `Source` field is exposed in API responses so clients can understand how costs were determined.

### 6. API Design

New endpoints:

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/v1/models/{model}` | Model detail with `costs` block |
| `PATCH` | `/v1/models/{model}/cost_map_key` | Set cost map key override |
| `PATCH` | `/v1/models/{model}/costs` | Set custom cost spec |

The `GET /v1/models` list endpoint is unchanged — it returns the minimal OpenAI-compatible model list without cost data, keeping the list response lightweight.

---

## Alternatives Considered

### Alternative A: Store overrides in `costmap.Manager` only (no SQLite)

**Rejected.** Overrides would be lost on every server restart, making the PATCH endpoints misleading (they look persistent but are not). Operators who set a cost map mapping would lose it on restart.

### Alternative B: `costmap.Manager` imports `storage`

**Rejected.** Would create an import cycle (`storage` → ... → `costmap` would be possible in the future) and would make `costmap` harder to unit test. The coordination responsibility belongs in the handler/startup layer.

### Alternative C: Merge both override modes into a single PATCH body

**Considered.** A single `PATCH /v1/models/{model}` with optional `cost_map_key` and `costs` fields would be more RESTful. However, two fields with mutual exclusivity is confusing in a single endpoint. Two focused endpoints with clear intent are easier to document and test.

### Alternative D: Single `GET /v1/models` with cost data

**Rejected.** The LiteLLM cost map may not be loaded at response time (it's loaded asynchronously). Returning costs in the list would require N lookups and would silently return zeros if the cost map is still loading. The detail endpoint pattern (list is lightweight, detail has everything) is consistent with the OpenAI API design.

---

## Consequences

- `costmap.Manager` gains 4 new public methods and 2 new unexported fields.
- `storage.Storage` interface gains 4 new methods; any mock implementations in tests must be updated.
- `cmd/proxy/main.go` gains a startup seeding block after `store.Initialize()`.
- PATCH endpoints are idempotent: repeating the same PATCH is safe.
- The `cost_overrides` table migration is append-only and non-destructive.

---

## References

- [LiteLLM model prices JSON](https://raw.githubusercontent.com/BerriAI/litellm/refs/heads/main/model_prices_and_context_window.json)
- `internal/costmap/costmap.go` — Manager implementation
- `internal/storage/storage.go` — Storage interface
- Issue #1: LiteLLM cost map integration
- Issue #2: Backend cost map API
- Issue #3: Frontend cost map UI
