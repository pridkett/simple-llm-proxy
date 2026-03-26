---
phase: 03-cost-monitoring-complete-console
plan: 2
type: execute
wave: 2
depends_on:
  - "03-PLAN-1"
files_modified:
  - internal/api/handler/spend.go
  - internal/api/handler/spend_test.go
  - internal/api/handler/admin_routes.go
autonomous: true
requirements:
  - COST-02
  - COST-04
  - COST-05

must_haves:
  truths:
    - "GET /admin/spend?from=YYYY-MM-DD&to=YYYY-MM-DD returns 200 with spend rows and pre-computed alerts"
    - "Default date range (no params) uses today minus 7 days through today"
    - "Optional team_id, app_id, key_id query params filter the result"
    - "Response includes an alerts array: keys where TotalSpend >= SoftBudget or TotalSpend >= MaxBudget"
    - "Malformed date params return 400"
    - "go test ./internal/api/handler/... passes with TestAdminSpend subtests green"
  artifacts:
    - path: "internal/api/handler/spend.go"
      provides: "AdminSpend handler, SpendResponse/AlertItem types"
      exports: ["AdminSpend"]
      min_lines: 80
    - path: "internal/api/handler/spend_test.go"
      provides: "Real handler tests replacing Wave 0 skips"
    - path: "internal/api/handler/admin_routes.go"
      provides: "GET /admin/spend route registered"
      contains: "AdminSpend"
  key_links:
    - from: "internal/api/handler/spend.go"
      to: "internal/storage/storage.go"
      via: "store.GetSpendSummary(ctx, from, to, filters)"
      pattern: "GetSpendSummary"
    - from: "internal/api/handler/admin_routes.go"
      to: "internal/api/handler/spend.go"
      via: "r.Get(\"/admin/spend\", AdminSpend(store))"
      pattern: "/admin/spend"
---

<objective>
Implement the `GET /admin/spend` HTTP handler and register it on the admin router. The handler parses date range and filter query params, calls `store.GetSpendSummary`, computes the alert list, and returns a JSON response that both the Cost view chart/table and the NavBar badge will consume.

Purpose: This is the single backend endpoint all frontend spend features consume. Getting it right here means Plans 3 and 4 can be written against a stable, tested contract.
Output: `GET /admin/spend` responds with aggregated spend data including pre-computed alerts. All handler tests pass.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/03-cost-monitoring-complete-console/03-CONTEXT.md
@.planning/phases/03-cost-monitoring-complete-console/03-RESEARCH.md
@.planning/phases/03-cost-monitoring-complete-console/03-PLAN-1-SUMMARY.md
@internal/api/handler/admin_routes.go
@internal/api/handler/admin.go
@internal/api/handler/keys.go
@internal/api/handler/spend_test.go
@internal/storage/storage.go
</context>

<interfaces>
<!-- Storage interface — GetSpendSummary now exists (added in Plan 1) -->
From internal/storage/storage.go (after Plan 1):
```go
type SpendFilters struct {
    TeamID *int64
    AppID  *int64
    KeyID  *int64
}

type SpendRow struct {
    KeyID      int64    `json:"key_id"`
    KeyName    string   `json:"key_name"`
    AppID      int64    `json:"app_id"`
    AppName    string   `json:"app_name"`
    TeamID     int64    `json:"team_id"`
    TeamName   string   `json:"team_name"`
    TotalSpend float64  `json:"total_spend"`
    MaxBudget  *float64 `json:"max_budget"`
    SoftBudget *float64 `json:"soft_budget"`
}

GetSpendSummary(ctx context.Context, from, to time.Time, filters SpendFilters) ([]SpendRow, error)
```

From internal/api/handler/admin.go (date parsing reference):
```go
// AdminLogs parses ?limit= and ?offset= — analogous pattern for ?from= and ?to=
limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
```

From internal/api/handler/keys.go (handler factory pattern):
```go
func AdminListKeys(store storage.Storage) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // ... parse params, call store, encode JSON
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(items)
    }
}
```

Frontend API contract (what client.js will call — per CONTEXT.md D-14):
```
GET /admin/spend?from=YYYY-MM-DD&to=YYYY-MM-DD&team_id=N&app_id=N&key_id=N
Response 200:
{
  "rows": [
    {
      "key_id": 1, "key_name": "my-key",
      "app_id": 2, "app_name": "my-app",
      "team_id": 3, "team_name": "my-team",
      "total_spend": 1.234,
      "max_budget": 10.0,   // null if unlimited
      "soft_budget": 8.0    // null if no threshold
    }
  ],
  "alerts": [
    {
      "key_id": 1, "key_name": "my-key",
      "app_name": "my-app",
      "total_spend": 9.5,
      "soft_budget": 8.0,
      "max_budget": 10.0,
      "alert_type": "soft"  // "soft" | "hard"
    }
  ],
  "from": "2026-03-19",
  "to": "2026-03-26"
}
```
</interfaces>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: AdminSpend handler with date parsing and alert computation</name>
  <files>
    internal/api/handler/spend.go
    internal/api/handler/spend_test.go
  </files>
  <behavior>
    - Default date range (no params): from = today minus 7 days, to = tomorrow (inclusive range)
    - Malformed from= or to= returns 400 with error message
    - Valid request calls store.GetSpendSummary with parsed dates and optional int64 filters
    - Alert list is computed in the handler (not in SQL): rows where TotalSpend >= SoftBudget get alert_type="soft"; rows where TotalSpend >= MaxBudget get alert_type="hard"
    - A row can only appear in alerts once (hard takes precedence over soft when both thresholds are exceeded)
    - Rows with nil SoftBudget and nil MaxBudget do not appear in alerts
    - Response JSON has "rows", "alerts", "from", "to" fields
    - team_id=0 (missing/empty) treated as no filter (nil SpendFilters.TeamID)
  </behavior>
  <action>
**Create `internal/api/handler/spend.go`:**

```go
package handler

import (
    "encoding/json"
    "net/http"
    "strconv"
    "time"

    "github.com/pwagstro/simple_llm_proxy/internal/model"
    "github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// spendResponse is the JSON envelope for GET /admin/spend.
type spendResponse struct {
    Rows   []storage.SpendRow `json:"rows"`
    Alerts []spendAlert       `json:"alerts"`
    From   string             `json:"from"`
    To     string             `json:"to"`
}

// spendAlert describes a key that has exceeded or is approaching its budget.
type spendAlert struct {
    KeyID      int64    `json:"key_id"`
    KeyName    string   `json:"key_name"`
    AppName    string   `json:"app_name"`
    TeamName   string   `json:"team_name"`
    TotalSpend float64  `json:"total_spend"`
    SoftBudget *float64 `json:"soft_budget"`
    MaxBudget  *float64 `json:"max_budget"`
    AlertType  string   `json:"alert_type"` // "soft" | "hard"
}

// AdminSpend handles GET /admin/spend
// Query params: from (YYYY-MM-DD), to (YYYY-MM-DD), team_id, app_id, key_id.
// All params are optional. Default range is last 7 days.
func AdminSpend(store storage.Storage) http.HandlerFunc {
    return func(w http.ResponseWriter, req *http.Request) {
        q := req.URL.Query()

        // Parse date range — default: last 7 days
        now := time.Now().UTC()
        defaultFrom := now.AddDate(0, 0, -7).Format("2006-01-02")
        defaultTo := now.AddDate(0, 0, 1).Format("2006-01-02") // exclusive upper bound

        from, err := parseSpendDate(q.Get("from"), defaultFrom)
        if err != nil {
            model.WriteError(w, model.ErrBadRequest("invalid 'from' date: use YYYY-MM-DD format"))
            return
        }
        to, err := parseSpendDate(q.Get("to"), defaultTo)
        if err != nil {
            model.WriteError(w, model.ErrBadRequest("invalid 'to' date: use YYYY-MM-DD format"))
            return
        }

        // Parse optional int64 filters — zero/missing = nil (no filter)
        filters := storage.SpendFilters{
            TeamID: parseOptionalInt64(q.Get("team_id")),
            AppID:  parseOptionalInt64(q.Get("app_id")),
            KeyID:  parseOptionalInt64(q.Get("key_id")),
        }

        rows, err := store.GetSpendSummary(req.Context(), from, to, filters)
        if err != nil {
            model.WriteError(w, model.ErrInternal("failed to load spend data"))
            return
        }

        // Compute alerts: keys at or above soft threshold or hard budget
        alerts := computeAlerts(rows)

        resp := spendResponse{
            Rows:   rows,
            Alerts: alerts,
            From:   from.Format("2006-01-02"),
            To:     to.AddDate(0, 0, -1).Format("2006-01-02"), // return the inclusive end date
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(resp)
    }
}

// parseSpendDate parses a YYYY-MM-DD string. If s is empty, parses defaultVal.
func parseSpendDate(s, defaultVal string) (time.Time, error) {
    if s == "" {
        s = defaultVal
    }
    return time.Parse("2006-01-02", s)
}

// parseOptionalInt64 returns nil if s is empty or zero, otherwise a pointer to the parsed int64.
func parseOptionalInt64(s string) *int64 {
    if s == "" {
        return nil
    }
    v, err := strconv.ParseInt(s, 10, 64)
    if err != nil || v == 0 {
        return nil
    }
    return &v
}

// computeAlerts returns alerts for rows where spend has reached soft or hard budget.
// Hard budget exceeded takes precedence — a row appears only once in the alert list.
func computeAlerts(rows []storage.SpendRow) []spendAlert {
    alerts := make([]spendAlert, 0)
    for _, r := range rows {
        var alertType string
        // Hard budget check first (takes precedence)
        if r.MaxBudget != nil && r.TotalSpend >= *r.MaxBudget {
            alertType = "hard"
        } else if r.SoftBudget != nil && r.TotalSpend >= *r.SoftBudget {
            alertType = "soft"
        }
        if alertType == "" {
            continue
        }
        alerts = append(alerts, spendAlert{
            KeyID:      r.KeyID,
            KeyName:    r.KeyName,
            AppName:    r.AppName,
            TeamName:   r.TeamName,
            TotalSpend: r.TotalSpend,
            SoftBudget: r.SoftBudget,
            MaxBudget:  r.MaxBudget,
            AlertType:  alertType,
        })
    }
    return alerts
}
```

**Update `internal/api/handler/spend_test.go`** — replace all `t.Skip` stubs with real tests using `mockSpendStorage`:

The tests should create a `mockSpendStorage` struct (embed `mockStorage`, override `GetSpendSummary`) and test the handler via `httptest.NewRecorder()`. Test the following behaviors from the `<behavior>` block:
- 200 with correct rows and empty alerts when no key exceeds budget
- alerts computed correctly for soft threshold hit
- alerts computed correctly for hard budget exceeded
- hard takes precedence over soft when both exceeded
- 400 returned for `?from=not-a-date`
- team_id=0 or empty is treated as nil filter (passes nil to GetSpendSummary)

```go
package handler

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// mockSpendStorage extends mockStorage with controllable GetSpendSummary behavior.
type mockSpendStorage struct {
    mockStorage
    spendRows []storage.SpendRow
    spendErr  error
    lastFilters storage.SpendFilters
}

func (m *mockSpendStorage) GetSpendSummary(_ context.Context, _, _ time.Time, filters storage.SpendFilters) ([]storage.SpendRow, error) {
    m.lastFilters = filters
    return m.spendRows, m.spendErr
}

func TestAdminSpend(t *testing.T) {
    // ... real test implementations per the behavior block
}
```
  </action>
  <verify>
    <automated>cd /Users/pwagstro/Documents/workspace/simple_llm_proxy && go test ./internal/api/handler/... -v -run TestAdminSpend 2>&1</automated>
  </verify>
  <done>All TestAdminSpend subtests PASS (not SKIP). Alert computation tested for both soft and hard cases. 400 returned for bad date params.</done>
</task>

<task type="auto">
  <name>Task 2: Register GET /admin/spend route</name>
  <files>internal/api/handler/admin_routes.go</files>
  <action>
Open `internal/api/handler/admin_routes.go` and add the spend route to `RegisterAdminRoutes`. Place it after the key management routes block (after the Phase 2 comment):

```go
// Cost/spend routes (Phase 3)
r.Get("/admin/spend", AdminSpend(store))
```

The function signature of `RegisterAdminRoutes` does NOT need to change — `store storage.Storage` is already a parameter, and `AdminSpend` only needs the store.

Verify the chi router group already has session middleware applied (it does — the `/admin/*` group in router.go uses `sm.LoadAndSave` and `middleware.RequireSession`). No additional auth setup needed.
  </action>
  <verify>
    <automated>cd /Users/pwagstro/Documents/workspace/simple_llm_proxy && go build ./... 2>&1 && go test ./... 2>&1 | tail -20</automated>
  </verify>
  <done>go build ./... exits 0. go test ./... exits 0 with no regressions. Route is registered and AdminSpend is callable.</done>
</task>

</tasks>

<verification>
```bash
cd /Users/pwagstro/Documents/workspace/simple_llm_proxy
go build ./...
go test ./internal/api/handler/... -v -run TestAdminSpend
go test ./... 2>&1 | grep -E "FAIL|ok"
```
All commands exit 0. TestAdminSpend subtests PASS. Full test suite green.
</verification>

<success_criteria>
- go build ./... exits 0
- go test ./internal/api/handler/... exits 0 with TestAdminSpend PASS
- Alert computation correctly produces "soft" vs "hard" types (tested)
- 400 returned for malformed date params (tested)
- go test ./... exits 0 — no regressions
- GET /admin/spend route registered in admin_routes.go
</success_criteria>

<output>
After completion, create `.planning/phases/03-cost-monitoring-complete-console/03-PLAN-2-SUMMARY.md`
</output>

## PLANNING COMPLETE
