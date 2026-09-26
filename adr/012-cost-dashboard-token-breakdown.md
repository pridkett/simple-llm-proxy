# ADR 012: Per-Model Token and Cost Breakdown in the Cost Dashboard

**Status:** Accepted
**Date:** 2026-09-26
**ADR Issue:** pridkett/simple-llm-proxy#91
**Implementation Issue:** pridkett/simple-llm-proxy#92

---

## Context

This is the last piece of the v1.2 "Request Telemetry & Observability" milestone. It was planned as GSD Phase 18 (requirements COST-01, COST-02, COST-03) and never started. The requirements are:

- **COST-01:** `ModelSpendRow` gains aggregated input and output token counts from `usage_logs`.
- **COST-02:** `CostView.vue` shows a per-model token and cost breakdown.
- **COST-03:** Cache token costs (`cache_read_tokens`, `cache_write_tokens`) count toward the displayed cost, and the UI labels costs as estimates.

Most of the plumbing already exists:

- `GET /admin/spend` (`internal/api/handler/spend.go`) already returns `model_rows`, built by `Storage.GetModelSpend` (`internal/storage/sqlite/spend.go`). Each row has only `model`, `total_spend` and `request_count`.
- `CostView.vue` already has a "By Model" toggle on its breakdown table, with Model, Total Spend and Requests columns.
- `usage_logs` has had `input_tokens`, `output_tokens`, `cache_read_tokens` and `cache_write_tokens` since migration 15, so no schema change is needed.
- Phase 13 (INSTR-04) fixed the cost formula. `computeCost` (`internal/api/handler/chat.go`) is shared by chat, Responses and embeddings, and it prices cache reads with `cache_read_input_token_cost` and cache writes with `cache_creation_input_token_cost`. The resulting `total_cost` is stored on each `usage_logs` row when the request completes.

That leaves two things: returning token totals per model, and presenting them with honest labeling in the UI.

## Decision

### D-01: Aggregate at query time from existing columns; no migration

`GetModelSpend` adds four `SUM(...)` expressions to its existing query:

```sql
COALESCE(SUM(ul.input_tokens), 0)       AS input_tokens,
COALESCE(SUM(ul.output_tokens), 0)      AS output_tokens,
COALESCE(SUM(ul.cache_read_tokens), 0)  AS cache_read_tokens,
COALESCE(SUM(ul.cache_write_tokens), 0) AS cache_write_tokens
```

The joins, the `_flush` exclusion, the active-key predicate, the date bounds and the team/app/key filters stay exactly as they are, so token totals always describe the same rows as `total_spend` and `request_count`. The query already scans those rows, so the extra aggregates add negligible cost, and no new index or rollup table is justified at this scale.

### D-02: `ModelSpendRow` carries all four token counts, not just input and output

COST-01 only names `InputTokens` and `OutputTokens`. The ADR adds the cache counts too:

```go
type ModelSpendRow struct {
    Model            string  `json:"model"`
    TotalSpend       float64 `json:"total_spend"`
    RequestCount     int64   `json:"request_count"`
    InputTokens      int64   `json:"input_tokens"`
    OutputTokens     int64   `json:"output_tokens"`
    CacheReadTokens  int64   `json:"cache_read_tokens"`
    CacheWriteTokens int64   `json:"cache_write_tokens"`
}
```

Without the cache columns, an Anthropic model that relies heavily on prompt caching shows a cost that can't be reconciled with its visible tokens. Anthropic reports `input_tokens` *excluding* cache reads and writes, so the tokens that explain most of the spend would be invisible. With all four counts, an operator can see why a model costs what it does.

Token semantics vary by provider, and the UI note (D-04) says so:

- **Anthropic:** input tokens exclude cached tokens, and the cache columns are populated.
- **Every other provider:** the cache columns are always zero (`model.Usage` documents this). For OpenAI, cached prompt tokens are included in input tokens and are priced at the full input rate. OpenAI's `prompt_tokens_details.cached_tokens` discount is not captured today (see Out of scope).

The fields are additive JSON on an existing response, so existing clients are unaffected and no API versioning is needed.

### D-03: Display the cost recorded at request time; do not recompute or backfill

The dashboard keeps summing `usage_logs.total_cost`. It does not recompute cost from token counts and the current cost map, for three reasons:

1. **Consistency with enforcement.** Key budgets (`keystore.SpendAccumulator`), pool budgets (`PoolBudgetManager`) and budget alerts all use the cost recorded at request time. If the dashboard recomputed cost, it could disagree with the numbers that actually block requests.
2. **The cost map changes.** Cost-map overrides and upstream pricing updates would silently rewrite historical spend.
3. **The data is already there.** Every row has priced cache tokens since the Phase 13 fix.

Consequence: Anthropic rows logged before the Phase 13 fix under-count cache costs, and they stay that way. Roadmap success criterion 3 ("Anthropic per-model costs are higher than before the cache fix") holds for traffic after that fix, which is now most retained data because retention defaults to 30 days (Phase 15). Backfilling would have to guess historical prices, so it is not done.

### D-04: By Model table layout and estimate labeling

The By Model table becomes:

| Model | Requests | Input Tokens | Output Tokens | Cache Read | Cache Write | Est. Spend |
|---|---|---|---|---|---|---|

- Token counts use locale-grouped integers (`toLocaleString()`).
- A **totals row** in `<tfoot>` sums every column over the rows currently shown, so filtering by team, app or key immediately shows the aggregate.
- Rows stay ordered by spend descending, as the storage query already returns them.
- The layout reuses the existing table styling. No new component and no new chart.

**Estimate labeling (COST-03):**

- Every spend value in `CostView.vue` renders with a `~` prefix (for example `~$0.0123`). That covers the by-key table, the by-model table, the totals row and the alert amounts. `formatSpend` is the single formatting point, so this is a one-line change.
- Spend column headers read "Est. Spend".
- A short note under the breakdown table reads: *"Costs are estimates calculated from the proxy's model price map when each request completed, including prompt-cache read/write pricing where the provider reports it. They may differ from provider invoices."*
- **Budget limits are not estimates.** `formatBudget` keeps plain `$` because those values are exact configured caps.

### D-05: Tests

- **Storage:** a SQLite test inserts rows for two models with known token counts, including cache tokens and a `_flush` row. It asserts per-model sums, that the flush row is excluded, and that the team/app/key filters apply to token totals.
- **Frontend (`CostView.test.js`):** asserts the new columns render with grouped numbers, the totals row sums correctly, spend shows the `~` prefix, budgets do not, and the estimate note is present.

## Consequences

**Positive**
- Completes the v1.2 milestone with no schema migration and no new endpoint.
- Operators can see token volume next to cost per model, including prompt-cache activity that drives Anthropic spend.
- The dashboard stays consistent with budget enforcement, because both use recorded cost.
- Labeling costs as estimates sets the right expectation against provider invoices.

**Negative / accepted trade-offs**
- Anthropic rows logged before the Phase 13 fix keep their under-counted cost until retention removes them.
- Input-token totals are not strictly comparable across providers, because Anthropic excludes cached tokens and OpenAI includes them. The UI note calls this out rather than normalizing.
- Wider table on narrow screens. The implementation wraps the breakdown tables in an `overflow-x-auto` container so they scroll horizontally instead of breaking the layout.

**Out of scope**
- Capturing OpenAI's `prompt_tokens_details.cached_tokens` and pricing the discount. That touches the openaicompat provider and `model.Usage`, and should be its own issue.
- Pool-level cost breakdown (listed as v1.3+ in the v1.2 requirements).
- Effective cost-per-1K-token columns, per-model time-series charts, and CSV export.
- Recomputing or backfilling historical `total_cost`.
- Documenting `/admin/spend` in the OpenAPI spec. It isn't documented today, and that gap is separate from this change.
