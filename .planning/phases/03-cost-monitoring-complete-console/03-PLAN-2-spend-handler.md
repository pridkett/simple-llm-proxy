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
    - "Non-admin session is rejected (403); unauthenticated request is rejected (401 or redirect)"
    - "go test ./internal/api/handler/... passes with TestAdminSpend subtests green"
  artifacts:
    - path: "internal/api/handler/spend.go"
      provides: "AdminSpend handler, SpendResponse/AlertItem types with date semantics comments"
      exports: ["AdminSpend"]
      min_lines: 80
    - path: "internal/api/handler/spend_test.go"
      provides: "Real handler tests replacing Wave 0 skips, including auth rejection and date boundary tests"
    - path: "internal/api/handler/admin_routes.go"
      provides: "GET /admin/spend route registered under admin middleware group"
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
Output: `GET /admin/spend` responds with aggregated spend data including pre-computed alerts. All handler tests pass including auth rejection and date boundary cases.
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
// SpendFilters uses pointer types. nil = no filter. NEVER pass 0 for absent IDs.
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

Date semantics (IMPORTANT — end-to-end contract):
```
Frontend sends:
  from: user-facing inclusive start date (YYYY-MM-DD)
  to:   user-facing inclusive end date (YYYY-MM-DD)

Backend converts to exclusive upper bound for SQL:
  SQL query uses: request_time >= from_midnight AND request_time < day_after_to_midnight
  This means: a row at 23:59 on the `to` date IS included.
  A row at midnight on the day AFTER `to` is NOT included.

API response returns:
  "from": the user-facing from date (same as input)
  "to":   the user-facing inclusive to date (one day before the exclusive SQL bound)

Example: user sends from=2026-03-01&to=2026-03-07
  SQL runs: request_time >= 2026-03-01T00:00:00Z AND request_time < 2026-03-08T00:00:00Z
  Response: { "from": "2026-03-01", "to": "2026-03-07" }
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

Authorization:
```
/admin/spend is registered under the admin route group in admin_routes.go.
The admin route group already applies session middleware (RequireSession + LoadAndSave).
RequireSession rejects unauthenticated requests (401 or redirect, depending on middleware behavior).
The plan must verify: non-admin authenticated users are also rejected (403).
Check how the existing admin handlers enforce admin-only access (see users.go, teams.go).
If the existing pattern uses a requireAdmin check in the handler itself (not just session),
add the same check to AdminSpend. Document the guard mechanism used in the handler comment.
```
</interfaces>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: AdminSpend handler with date parsing, alert computation, and auth enforcement</name>
  <files>
    internal/api/handler/spend.go
    internal/api/handler/spend_test.go
  </files>
  <behavior>
    - Default date range (no params): from = today minus 7 days, to = today + 1 day (exclusive SQL bound)
    - Frontend sends inclusive to date; backend adds 1 day to make it exclusive for SQL
    - Response returns the user-facing inclusive to date (one day before exclusive bound)
    - A row at 23:59:59 on the user-facing to date IS included in results
    - Malformed from= or to= returns 400 with error message
    - Valid request calls store.GetSpendSummary with parsed dates and optional int64 filters
    - Alert list is computed in the handler (not in SQL): rows where TotalSpend >= SoftBudget get alert_type="soft"; rows where TotalSpend >= MaxBudget get alert_type="hard"
    - A row can only appear in alerts once (hard takes precedence over soft when both thresholds are exceeded)
    - Rows with nil SoftBudget and nil MaxBudget do not appear in alerts
    - Response JSON has "rows", "alerts", "from", "to" fields
    - team_id query param: empty string or "0" is treated as no filter (nil SpendFilters.TeamID); same for app_id and key_id
    - Non-admin authenticated session returns 403 (same guard as other admin-only handlers)
    - Unauthenticated request returns 401 (or redirect, per existing middleware behavior)
  </behavior>
  <action>
**Create `internal/api/handler/spend.go`:**

Before implementing, read `internal/api/handler/users.go` (or teams.go) to determine how existing admin handlers enforce admin-only access. Use the exact same guard pattern. If admin-only is enforced via a middleware in the admin route group rather than per-handler, document this in a comment explaining why no explicit check is needed in the handler itself.

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
// Authorization: admin-only. This endpoint exposes deployment-wide spend data.
// It is registered under the admin route group which applies RequireSession middleware.
// Admin-only access is enforced via [describe the actual mechanism — per-handler check
// or admin middleware in the route group — after reading the existing handler pattern].
//
// Query params (all optional):
//   from    YYYY-MM-DD  User-facing inclusive start date. Default: today-7d.
//   to      YYYY-MM-DD  User-facing inclusive end date. Default: today.
//                       IMPORTANT: The backend adds 1 day to `to` before passing to SQL,
//                       making it an exclusive upper bound. This means a row at 23:59:59
//                       on the user-facing `to` date IS included. The response returns
//                       the user-facing inclusive date, not the exclusive SQL bound.
//   team_id  integer    Optional filter. Empty or "0" = no filter (nil).
//   app_id   integer    Optional filter. Empty or "0" = no filter (nil).
//   key_id   integer    Optional filter. Empty or "0" = no filter (nil).
func AdminSpend(store storage.Storage) http.HandlerFunc {
    return func(w http.ResponseWriter, req *http.Request) {
        // Apply admin-only guard using the same pattern as existing admin handlers.
        // Read users.go or teams.go to determine the correct pattern before coding.

        q := req.URL.Query()

        // Parse date range.
        // Default from: 7 days ago. Default to (exclusive SQL bound): tomorrow.
        now := time.Now().UTC().Truncate(24 * time.Hour)
        defaultFrom := now.AddDate(0, 0, -7).Format("2006-01-02")
        defaultToExclusive := now.AddDate(0, 0, 1).Format("2006-01-02")

        fromTime, err := parseSpendDate(q.Get("from"), defaultFrom)
        if err != nil {
            model.WriteError(w, model.ErrBadRequest("invalid 'from' date: use YYYY-MM-DD format"))
            return
        }

        // User sends inclusive to date; we add 1 day to make it exclusive for SQL.
        // Example: user sends to=2026-03-26 → SQL uses request_time < 2026-03-27T00:00:00Z
        // This ensures rows at 23:59:59 on 2026-03-26 are included.
        toInclusive, err := parseSpendDate(q.Get("to"), now.Format("2006-01-02"))
        if err != nil {
            model.WriteError(w, model.ErrBadRequest("invalid 'to' date: use YYYY-MM-DD format"))
            return
        }
        toExclusive, _ := parseSpendDate("", defaultToExclusive) // fallback unused; toInclusive+1
        _ = toExclusive
        toSQL := toInclusive.AddDate(0, 0, 1) // exclusive upper bound for SQL

        // Parse optional int64 filters.
        // parseOptionalInt64 returns nil for empty string or "0" — ensures nil (not 0) reaches SQL.
        // The double-bind pattern (? IS NULL OR col = ?) in GetSpendSummary requires nil for "no filter".
        filters := storage.SpendFilters{
            TeamID: parseOptionalInt64(q.Get("team_id")),
            AppID:  parseOptionalInt64(q.Get("app_id")),
            KeyID:  parseOptionalInt64(q.Get("key_id")),
        }

        rows, err := store.GetSpendSummary(req.Context(), fromTime, toSQL, filters)
        if err != nil {
            model.WriteError(w, model.ErrInternal("failed to load spend data"))
            return
        }

        // Compute alerts: keys at or above soft threshold or hard budget.
        alerts := computeAlerts(rows)

        resp := spendResponse{
            Rows:   rows,
            Alerts: alerts,
            From:   fromTime.Format("2006-01-02"),
            To:     toInclusive.Format("2006-01-02"), // return user-facing inclusive date
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

// parseOptionalInt64 returns nil if s is empty or "0", otherwise a pointer to the parsed int64.
// Only positive integers are valid filter IDs. "0" and non-numeric values return nil.
// This ensures the SQL double-bind pattern (? IS NULL OR col = ?) receives nil for absent filters.
func parseOptionalInt64(s string) *int64 {
    if s == "" {
        return nil
    }
    v, err := strconv.ParseInt(s, 10, 64)
    if err != nil || v <= 0 {
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
        // Hard budget check first (takes precedence over soft)
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

The tests create a `mockSpendStorage` struct (embed `mockStorage`, override `GetSpendSummary`) and test the handler via `httptest.NewRecorder()`.

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
    spendRows   []storage.SpendRow
    spendErr    error
    lastFilters storage.SpendFilters
    lastFrom    time.Time
    lastTo      time.Time
}

func (m *mockSpendStorage) GetSpendSummary(_ context.Context, from, to time.Time, filters storage.SpendFilters) ([]storage.SpendRow, error) {
    m.lastFrom = from
    m.lastTo = to
    m.lastFilters = filters
    return m.spendRows, m.spendErr
}

func TestAdminSpend(t *testing.T) {
    t.Run("returns 200 with aggregated spend rows for default 7d range", func(t *testing.T) {
        store := &mockSpendStorage{spendRows: []storage.SpendRow{}}
        req := httptest.NewRequest(http.MethodGet, "/admin/spend", nil)
        w := httptest.NewRecorder()
        AdminSpend(store)(w, req)
        if w.Code != http.StatusOK {
            t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
        }
        var resp spendResponse
        if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
            t.Fatalf("decode response: %v", err)
        }
        if resp.Rows == nil {
            t.Error("expected non-nil rows slice")
        }
        if resp.Alerts == nil {
            t.Error("expected non-nil alerts slice")
        }
    })

    t.Run("returns pre-computed alerts for keys over soft budget", func(t *testing.T) {
        softBudget := 8.0
        maxBudget := 10.0
        store := &mockSpendStorage{spendRows: []storage.SpendRow{
            {KeyID: 1, KeyName: "k1", AppName: "app1", TeamName: "team1",
             TotalSpend: 9.0, SoftBudget: &softBudget, MaxBudget: &maxBudget},
        }}
        req := httptest.NewRequest(http.MethodGet, "/admin/spend", nil)
        w := httptest.NewRecorder()
        AdminSpend(store)(w, req)
        var resp spendResponse
        json.NewDecoder(w.Body).Decode(&resp)
        if len(resp.Alerts) != 1 {
            t.Fatalf("expected 1 alert, got %d", len(resp.Alerts))
        }
        if resp.Alerts[0].AlertType != "soft" {
            t.Errorf("expected alert_type=soft, got %q", resp.Alerts[0].AlertType)
        }
    })

    t.Run("hard budget takes precedence over soft when both exceeded", func(t *testing.T) {
        softBudget := 5.0
        maxBudget := 8.0
        store := &mockSpendStorage{spendRows: []storage.SpendRow{
            {KeyID: 1, KeyName: "k1", AppName: "app1", TeamName: "team1",
             TotalSpend: 9.0, SoftBudget: &softBudget, MaxBudget: &maxBudget},
        }}
        req := httptest.NewRequest(http.MethodGet, "/admin/spend", nil)
        w := httptest.NewRecorder()
        AdminSpend(store)(w, req)
        var resp spendResponse
        json.NewDecoder(w.Body).Decode(&resp)
        if len(resp.Alerts) != 1 {
            t.Fatalf("expected 1 alert, got %d", len(resp.Alerts))
        }
        if resp.Alerts[0].AlertType != "hard" {
            t.Errorf("expected alert_type=hard, got %q", resp.Alerts[0].AlertType)
        }
    })

    t.Run("returns 400 for malformed date params", func(t *testing.T) {
        store := &mockSpendStorage{}
        req := httptest.NewRequest(http.MethodGet, "/admin/spend?from=not-a-date", nil)
        w := httptest.NewRecorder()
        AdminSpend(store)(w, req)
        if w.Code != http.StatusBadRequest {
            t.Fatalf("expected 400, got %d", w.Code)
        }
    })

    t.Run("team_id=0 is treated as no filter (nil)", func(t *testing.T) {
        store := &mockSpendStorage{spendRows: []storage.SpendRow{}}
        req := httptest.NewRequest(http.MethodGet, "/admin/spend?team_id=0", nil)
        w := httptest.NewRecorder()
        AdminSpend(store)(w, req)
        if w.Code != http.StatusOK {
            t.Fatalf("expected 200, got %d", w.Code)
        }
        if store.lastFilters.TeamID != nil {
            t.Errorf("expected nil TeamID for team_id=0, got %v", store.lastFilters.TeamID)
        }
    })

    t.Run("to date is inclusive — row at 23:59 on to date is included", func(t *testing.T) {
        // When user sends to=2026-03-26, the SQL bound should be < 2026-03-27.
        // We verify that the `to` time passed to GetSpendSummary is the day AFTER the user date.
        store := &mockSpendStorage{spendRows: []storage.SpendRow{}}
        req := httptest.NewRequest(http.MethodGet, "/admin/spend?from=2026-03-20&to=2026-03-26", nil)
        w := httptest.NewRecorder()
        AdminSpend(store)(w, req)
        if w.Code != http.StatusOK {
            t.Fatalf("expected 200, got %d", w.Code)
        }
        // The SQL `to` bound must be 2026-03-27 (exclusive) so rows on 2026-03-26 are included
        expectedToSQL := time.Date(2026, 3, 27, 0, 0, 0, 0, time.UTC)
        if !store.lastTo.Equal(expectedToSQL) {
            t.Errorf("expected SQL to=%v (exclusive), got %v", expectedToSQL, store.lastTo)
        }
        // The response `to` field must reflect the user-facing inclusive date
        var resp spendResponse
        json.NewDecoder(w.Body).Decode(&resp)
        if resp.To != "2026-03-26" {
            t.Errorf("expected response to=2026-03-26 (inclusive), got %q", resp.To)
        }
    })

    // Auth rejection tests: verify that /admin/spend correctly enforces admin-only access.
    // NOTE: These tests exercise the authorization mechanism. If admin-only is enforced purely
    // by middleware in the route group (not in the handler itself), these tests should be
    // integration tests against the full router rather than unit tests of AdminSpend.
    // Implement using the same pattern as auth tests in users_test.go or teams_test.go.
    // If the existing pattern is middleware-level only, mark these as t.Skip with an explanation
    // and add a note that auth is covered by the route group middleware test.
    t.Run("non-admin session returns 403", func(t *testing.T) {
        t.Skip("implement after reading auth enforcement pattern in existing admin handlers")
    })
    t.Run("unauthenticated request returns 401", func(t *testing.T) {
        t.Skip("implement after reading auth enforcement pattern in existing admin handlers")
    })
}
```

IMPORTANT on auth tests: Before implementing the auth rejection tests, read `internal/api/handler/users_test.go` and the middleware in `internal/api/middleware/` to understand how session and admin checks work. If admin-only enforcement is via a middleware that wraps the route group (not inside the handler), the unit test of `AdminSpend(store)` alone cannot reproduce a 403 — the test must exercise the middleware. Follow the exact pattern used by existing admin handler tests for auth scenarios. If no existing unit-level auth tests exist, note this in a comment and create an integration test in the router test file instead.
  </action>
  <verify>
    <automated>cd /Users/pwagstro/Documents/workspace/simple_llm_proxy && go test ./internal/api/handler/... -v -run TestAdminSpend 2>&1</automated>
  </verify>
  <done>All non-skipped TestAdminSpend subtests PASS. Alert computation tested for both soft and hard cases. 400 returned for bad date params. team_id=0 passes nil to storage. to date exclusivity verified. Auth tests either pass or are explicitly skipped with a comment explaining the middleware pattern.</done>
</task>

<task type="auto">
  <name>Task 2: Register GET /admin/spend route</name>
  <files>internal/api/handler/admin_routes.go</files>
  <action>
Open `internal/api/handler/admin_routes.go` and add the spend route to `RegisterAdminRoutes`. Place it after the key management routes block (after the Phase 2 comment):

```go
// Cost/spend routes (Phase 3)
// Authorization: /admin/spend exposes deployment-wide spend data.
// Access is restricted to admin users via the session middleware applied to this route group
// (RequireSession + admin check). See internal/api/middleware/ for the guard implementation.
r.Get("/admin/spend", AdminSpend(store))
```

The function signature of `RegisterAdminRoutes` does NOT need to change — `store storage.Storage` is already a parameter, and `AdminSpend` only needs the store.

Before adding the route, verify by reading admin_routes.go: confirm that the `/admin/*` route group applies both session authentication AND admin-only access control. Document the middleware chain in the comment above. If only session auth (not admin-level check) is applied at the group level, add an explicit admin-only check inside `AdminSpend` using the same pattern as other admin handlers.
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
All commands exit 0. TestAdminSpend subtests PASS (or explicitly skipped with auth-pattern explanation). Full test suite green.
</verification>

<success_criteria>
- go build ./... exits 0
- go test ./internal/api/handler/... exits 0 with TestAdminSpend PASS
- Alert computation correctly produces "soft" vs "hard" types (tested)
- 400 returned for malformed date params (tested)
- team_id=0 passes nil to storage, not a 0 value (tested)
- to date exclusivity: SQL bound is day+1 of user-facing to date (tested)
- Response to field reflects user-facing inclusive date (tested)
- Auth rejection: non-admin returns 403, unauthenticated returns 401 — either tested or explicitly noted with auth middleware pattern explanation
- go test ./... exits 0 — no regressions
- GET /admin/spend route registered in admin_routes.go with authorization comment
</success_criteria>

<output>
After completion, create `.planning/phases/03-cost-monitoring-complete-console/03-PLAN-2-SUMMARY.md`
</output>

## PLANNING COMPLETE
