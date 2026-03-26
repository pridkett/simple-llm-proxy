---
phase: 03-cost-monitoring-complete-console
plan: 1
type: execute
wave: 1
depends_on:
  - "03-PLAN-0"
files_modified:
  - internal/storage/storage.go
  - internal/storage/sqlite/spend.go
  - internal/storage/sqlite/migrations.go
  - internal/storage/sqlite/spend_test.go
autonomous: true
requirements:
  - COST-02
  - COST-05

must_haves:
  truths:
    - "GetSpendSummary returns rows with key name, app name, team name, total spend, soft/hard budget per key"
    - "Flush rows (model='_flush') are excluded from spend totals"
    - "Date range filter (from/to) correctly bounds the aggregation window"
    - "Optional team_id, app_id, key_id filters narrow results without breaking when nil"
    - "go test ./internal/storage/... passes with all GetSpendSummary tests green (not skipped)"
  artifacts:
    - path: "internal/storage/storage.go"
      provides: "SpendRow struct, SpendFilters struct, GetSpendSummary method on Storage interface"
      contains: "GetSpendSummary"
    - path: "internal/storage/sqlite/spend.go"
      provides: "SQLite implementation of GetSpendSummary with JOIN + flush exclusion"
      min_lines: 50
    - path: "internal/storage/sqlite/spend_test.go"
      provides: "Real tests for GetSpendSummary replacing Wave 0 skips"
  key_links:
    - from: "internal/storage/storage.go"
      to: "internal/storage/sqlite/spend.go"
      via: "Storage interface implemented by sqlite.Storage struct"
      pattern: "GetSpendSummary"
    - from: "internal/storage/sqlite/spend.go"
      to: "usage_logs table"
      via: "SQL LEFT JOIN with WHERE model != '_flush'"
      pattern: "model != '_flush'"
---

<objective>
Add the spend aggregation storage layer: new types (`SpendRow`, `SpendFilters`) in the Storage interface, a SQLite implementation that JOINs api_keys → applications → teams with date-range filtering, and real tests replacing the Wave 0 skips.

Purpose: Every higher layer (handler, frontend) depends on this contract. Defining it here means Plan 2's handler can be written against a stable, tested interface.
Output: `GetSpendSummary` callable from anywhere that has a `storage.Storage`. All Go tests for this layer pass.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/03-cost-monitoring-complete-console/03-CONTEXT.md
@.planning/phases/03-cost-monitoring-complete-console/03-RESEARCH.md
@internal/storage/storage.go
@internal/storage/sqlite/apikeys.go
@internal/storage/sqlite/migrations.go
@internal/storage/sqlite/spend_test.go
</context>

<interfaces>
<!-- Existing storage interface — all new methods add to the END of the Storage interface -->
<!-- Full struct is in internal/storage/storage.go, read before modifying -->

From internal/storage/sqlite/apikeys.go (GetKeySpendTotals — the reference pattern):
```go
// Pattern for spend queries — new GetSpendSummary follows same scan pattern
func (s *Storage) GetKeySpendTotals(ctx context.Context) (map[int64]float64, error) {
    rows, err := s.db.QueryContext(ctx, `
        SELECT api_key_id, SUM(total_cost)
        FROM usage_logs
        WHERE api_key_id IS NOT NULL
        GROUP BY api_key_id
    `)
    // ...scan loop...
}
```

From internal/storage/sqlite/migrations.go (existing schema — no changes needed):
```sql
-- api_keys table (Migration 11):
-- id, application_id, name, key_prefix, key_hash, max_rpm, max_rpd, max_budget, soft_budget, is_active, created_at

-- applications table (Migration 9):
-- id, team_id, name, created_at

-- teams table (Migration 7):
-- id, name, created_at

-- usage_logs table (Migration 2):
-- id, request_id, api_key_id, model, provider, endpoint, prompt_tokens, completion_tokens, total_cost, status_code, latency_ms, request_time

-- Index already exists (Migration 3):
-- idx_usage_logs_request_time ON usage_logs(request_time)
-- idx_usage_logs_api_key_id ON usage_logs(api_key_id)
```

From internal/api/handler/models_test.go (mockStorage — must add GetSpendSummary stub):
```go
// mockStorage has all existing interface stubs in models_test.go.
// When GetSpendSummary is added to the Storage interface, mockStorage
// will fail to compile until a stub is added to models_test.go.
// ADD to models_test.go (in the API Key CRUD stubs section):
func (m *mockStorage) GetSpendSummary(_ context.Context, _, _ time.Time, _ storage.SpendFilters) ([]storage.SpendRow, error) {
    return nil, nil
}
```
</interfaces>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Add SpendRow, SpendFilters types and GetSpendSummary to Storage interface</name>
  <files>
    internal/storage/storage.go
    internal/api/handler/models_test.go
  </files>
  <behavior>
    - SpendRow has fields: KeyID int64, KeyName string, AppID int64, AppName string, TeamID int64, TeamName string, TotalSpend float64, MaxBudget *float64, SoftBudget *float64
    - SpendFilters has fields: TeamID *int64, AppID *int64, KeyID *int64
    - GetSpendSummary(ctx, from, to, filters) is a method on the Storage interface
    - mockStorage in models_test.go compiles after adding the stub method
    - go build ./... exits 0 (compilation check before implementation)
  </behavior>
  <action>
1. Open `internal/storage/storage.go` and add at the end of the Storage interface (after `FlushKeySpend`):

```go
// GetSpendSummary returns aggregated spend per key for the given date range and optional filters.
// Flush rows (model='_flush') are excluded. Only active keys are returned.
// Used by the /admin/spend dashboard endpoint.
GetSpendSummary(ctx context.Context, from, to time.Time, filters SpendFilters) ([]SpendRow, error)
```

2. Also in `internal/storage/storage.go`, add two new types after the `RequestLog` struct:

```go
// SpendFilters optionally narrows a GetSpendSummary query to a specific team, application, or key.
// nil fields are ignored (no filter applied for that dimension).
type SpendFilters struct {
    TeamID *int64
    AppID  *int64
    KeyID  *int64
}

// SpendRow is one row from GetSpendSummary: per-key spend with JOIN-resolved names.
type SpendRow struct {
    KeyID      int64    `json:"key_id"`
    KeyName    string   `json:"key_name"`
    AppID      int64    `json:"app_id"`
    AppName    string   `json:"app_name"`
    TeamID     int64    `json:"team_id"`
    TeamName   string   `json:"team_name"`
    TotalSpend float64  `json:"total_spend"`
    MaxBudget  *float64 `json:"max_budget"`   // nil = unlimited (hard cap)
    SoftBudget *float64 `json:"soft_budget"`  // nil = no soft alert threshold
}
```

3. Open `internal/api/handler/models_test.go` and add a stub for the new interface method in the "API Key CRUD stubs" section (after `FlushKeySpend`):

```go
func (m *mockStorage) GetSpendSummary(_ context.Context, _, _ time.Time, _ storage.SpendFilters) ([]storage.SpendRow, error) {
    return nil, nil
}
```

This requires adding `"time"` to the import if not already present in models_test.go (check: `time` is already imported for `configForTest`).
  </action>
  <verify>
    <automated>cd /Users/pwagstro/Documents/workspace/simple_llm_proxy && go build ./... 2>&1</automated>
  </verify>
  <done>go build ./... exits 0 — interface updated, stub added, compilation clean</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Implement GetSpendSummary in SQLite + real tests replacing Wave 0 skips</name>
  <files>
    internal/storage/sqlite/spend.go
    internal/storage/sqlite/spend_test.go
  </files>
  <behavior>
    - Empty table returns empty slice (no error)
    - Real request rows (model != '_flush') are summed into TotalSpend
    - Flush rows (model='_flush') are NOT included in TotalSpend — double-count test
    - Date range filter: rows before `from` are excluded; rows on/after `to` are excluded
    - TeamID filter: only rows for keys belonging to that team are returned
    - AppID filter: only rows for keys in that application are returned
    - KeyID filter: only that one key's row is returned
    - All nil filters: all active keys returned (even with zero spend)
    - Returned rows have correct KeyName, AppName, TeamName from JOINs
    - MaxBudget and SoftBudget are populated from api_keys row (nil when unset)
  </behavior>
  <action>
**Create `internal/storage/sqlite/spend.go`:**

```go
package sqlite

import (
    "context"
    "fmt"
    "time"

    "github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// GetSpendSummary returns aggregated spend per active key for the given date range and filters.
// Flush rows (model='_flush') are excluded to prevent double-counting.
// Keys with zero spend in the date range are included (LEFT JOIN returns 0 via COALESCE).
func (s *Storage) GetSpendSummary(ctx context.Context, from, to time.Time, filters storage.SpendFilters) ([]storage.SpendRow, error) {
    // Build the query with optional filter predicates.
    // SQLite does not support named parameters for IS NULL checks, so we use
    // the double-bind pattern: (? IS NULL OR col = ?) — bind the value twice.
    const baseQuery = `
        SELECT
            k.id          AS key_id,
            k.name        AS key_name,
            k.max_budget,
            k.soft_budget,
            a.id          AS app_id,
            a.name        AS app_name,
            t.id          AS team_id,
            t.name        AS team_name,
            COALESCE(SUM(ul.total_cost), 0) AS total_spend
        FROM api_keys k
        JOIN applications a ON a.id = k.application_id
        JOIN teams t        ON t.id = a.team_id
        LEFT JOIN usage_logs ul
            ON ul.api_key_id = k.id
            AND ul.model != '_flush'
            AND ul.request_time >= ?
            AND ul.request_time < ?
        WHERE k.is_active = TRUE
          AND (? IS NULL OR t.id = ?)
          AND (? IS NULL OR a.id = ?)
          AND (? IS NULL OR k.id = ?)
        GROUP BY k.id
        ORDER BY total_spend DESC
    `
    // Args: from, to, teamID, teamID, appID, appID, keyID, keyID
    args := []any{
        from, to,
        filters.TeamID, filters.TeamID,
        filters.AppID, filters.AppID,
        filters.KeyID, filters.KeyID,
    }

    rows, err := s.db.QueryContext(ctx, baseQuery, args...)
    if err != nil {
        return nil, fmt.Errorf("get spend summary: %w", err)
    }
    defer rows.Close()

    result := make([]storage.SpendRow, 0)
    for rows.Next() {
        var r storage.SpendRow
        if err := rows.Scan(
            &r.KeyID, &r.KeyName, &r.MaxBudget, &r.SoftBudget,
            &r.AppID, &r.AppName,
            &r.TeamID, &r.TeamName,
            &r.TotalSpend,
        ); err != nil {
            return nil, fmt.Errorf("get spend summary scan: %w", err)
        }
        result = append(result, r)
    }
    return result, rows.Err()
}
```

**Update `internal/storage/sqlite/spend_test.go`** — replace all t.Skip stubs with real assertions. The tests use an in-memory SQLite DB, insert controlled data, and assert the returned SpendRow values.

Key test setup helper:
```go
package sqlite

import (
    "context"
    "testing"
    "time"

    "github.com/pwagstro/simple_llm_proxy/internal/storage"
)

func setupSpendTestDB(t *testing.T) *Storage {
    t.Helper()
    s, err := New(":memory:")
    if err != nil {
        t.Fatalf("New: %v", err)
    }
    if err := s.Initialize(context.Background()); err != nil {
        t.Fatalf("Initialize: %v", err)
    }
    return s
}
```

Test structure — write tests for all behavior items listed in the `<behavior>` block above:
- Insert a team, app, key using the store's Create methods (or direct SQL via `s.db.ExecContext`)
- Insert usage_log rows with specific `request_time`, `total_cost`, `model` values
- Call `s.GetSpendSummary(ctx, from, to, filters)` and assert the returned rows

For the flush row exclusion test: insert one real request row (`model='gpt-4'`, cost=0.01) and one flush row (`model='_flush'`, cost=99.99) for the same key in the same date range. Assert TotalSpend == 0.01 (not 100.00).

For the date range test: insert three rows at T-10d, T-1d, T+1d. Query with `from=T-7d, to=T`. Assert only the T-1d row is included.

For filter tests: insert two teams with one key each. Filter by team_id of team 1. Assert only team 1's key appears.

Note: Direct SQL inserts into `teams`, `applications`, `api_keys`, `usage_logs` are acceptable in tests where the storage methods would require too many setup calls. Use `s.db.ExecContext` for test data seeding.
  </action>
  <verify>
    <automated>cd /Users/pwagstro/Documents/workspace/simple_llm_proxy && go test ./internal/storage/... -v -run TestGetSpendSummary 2>&1</automated>
  </verify>
  <done>All TestGetSpendSummary subtests show as PASS (not SKIP). go test ./internal/storage/... exits 0.</done>
</task>

</tasks>

<verification>
```bash
cd /Users/pwagstro/Documents/workspace/simple_llm_proxy
go build ./...
go test ./internal/storage/... -v -run TestGetSpendSummary
go test ./internal/api/handler/... -v  # mockStorage must still compile with new stub
```
All commands exit 0. No regressions in existing tests.
</verification>

<success_criteria>
- go build ./... exits 0
- go test ./internal/storage/... exits 0 with TestGetSpendSummary subtests PASS (not SKIP)
- go test ./internal/api/handler/... exits 0 (mockStorage compiles with new GetSpendSummary stub)
- GetSpendSummary correctly excludes flush rows (verified by dedicated test)
- Date range filter correctly bounds results (verified by dedicated test)
</success_criteria>

<output>
After completion, create `.planning/phases/03-cost-monitoring-complete-console/03-PLAN-1-SUMMARY.md`
</output>

## PLANNING COMPLETE
