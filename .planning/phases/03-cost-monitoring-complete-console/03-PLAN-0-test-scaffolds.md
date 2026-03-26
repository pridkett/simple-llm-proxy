---
phase: 03-cost-monitoring-complete-console
plan: 0
type: execute
wave: 0
depends_on: []
files_modified:
  - internal/storage/sqlite/spend_test.go
  - internal/api/handler/spend_test.go
  - frontend/tests/unit/views/CostView.test.js
  - frontend/tests/unit/components/NavBadge.test.js
autonomous: true
requirements:
  - COST-02
  - COST-04
  - COST-05
  - UI-05
  - UI-06

must_haves:
  truths:
    - "go test ./internal/storage/... passes before GetSpendSummary is implemented (stubs compile)"
    - "go test ./internal/api/handler/... passes before AdminSpend is implemented (stubs compile)"
    - "cd frontend && npm test passes before CostView.vue exists (stubs render)"
  artifacts:
    - path: "internal/storage/sqlite/spend_test.go"
      provides: "Failing test stubs for GetSpendSummary — compile-safe, test names defined"
    - path: "internal/api/handler/spend_test.go"
      provides: "Failing test stubs for AdminSpend handler and mockSpendStorage"
    - path: "frontend/tests/unit/views/CostView.test.js"
      provides: "Vitest stubs for CostView with apexchart stubbed out"
    - path: "frontend/tests/unit/components/NavBadge.test.js"
      provides: "Vitest stubs for NavBar badge behavior"
  key_links:
    - from: "internal/storage/sqlite/spend_test.go"
      to: "internal/storage/sqlite/spend.go"
      via: "same package — tests call GetSpendSummary directly"
      pattern: "GetSpendSummary"
    - from: "internal/api/handler/spend_test.go"
      to: "internal/api/handler/spend.go"
      via: "same package — handler tests call AdminSpend(store)"
      pattern: "AdminSpend"
---

<objective>
Create all Wave 0 test scaffolds required by the VALIDATION.md contract. These are stub tests that compile and run (skipping or asserting trivially) before the production code exists. Their purpose is to establish test file locations, import structures, and test function names so that later plans can fill in real assertions without hunting for the right test patterns.

Purpose: The VALIDATION.md specifies that no task may have `<automated>MISSING` after Wave 0 completes. This plan creates those files.
Output: Four test files, all compiling and passing before any production code in Plans 1–4 is written.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/03-cost-monitoring-complete-console/03-VALIDATION.md
@.planning/phases/03-cost-monitoring-complete-console/03-RESEARCH.md

<!-- Existing test patterns to follow exactly -->
@internal/api/handler/applications_test.go
@internal/api/handler/users_test.go
@internal/api/handler/models_test.go
@frontend/tests/unit/components/StatusBadge.test.js
@frontend/tests/unit/views/DashboardView.test.js
</context>

<interfaces>
<!-- mockStorage base struct (all methods already stubbed in models_test.go) -->
<!-- spend_test.go will extend mockStorage with spend-specific fields -->

From internal/storage/storage.go:
```go
// New types to add (Plan 1 will add these to the real interface)
type SpendFilters struct {
    TeamID *int64
    AppID  *int64
    KeyID  *int64
}

type SpendRow struct {
    KeyID      int64
    KeyName    string
    AppID      int64
    AppName    string
    TeamID     int64
    TeamName   string
    TotalSpend float64
    MaxBudget  *float64
    SoftBudget *float64
}

// Method signature:
GetSpendSummary(ctx context.Context, from, to time.Time, filters SpendFilters) ([]SpendRow, error)
```

From internal/api/handler (established pattern for handler tests):
```go
// Handler factory pattern:
func AdminSpend(store storage.Storage) http.HandlerFunc { ... }

// Response shape (from CONTEXT.md D-14):
// GET /admin/spend?from=YYYY-MM-DD&to=YYYY-MM-DD&team_id=&app_id=&key_id=
// Returns: { rows: []SpendRow, alerts: []AlertRow }
```

From frontend/tests/unit/views/DashboardView.test.js:
```javascript
// mount pattern for views with router:
function makeRouter() {
  return createRouter({ history: createWebHashHistory(), routes: [{ path: '/cost', component: CostView }] })
}
// Mock fetch globally:
global.fetch = vi.fn()
```
</interfaces>

<tasks>

<task type="auto">
  <name>Task 1: Go storage test scaffold for GetSpendSummary</name>
  <files>internal/storage/sqlite/spend_test.go</files>
  <action>
Create `internal/storage/sqlite/spend_test.go` in package `sqlite`. This file creates test stubs that compile before `GetSpendSummary` exists (Plan 1 will implement it).

The file must:
1. Declare package `sqlite` (same package as implementation — direct struct access)
2. Import: `"context"`, `"testing"`, `"time"`, `"github.com/pwagstro/simple_llm_proxy/internal/storage"`
3. Contain a helper `setupTestDB(t *testing.T) *Storage` that calls `New(":memory:")` and `s.Initialize(context.Background())` — follow the same in-memory DB pattern used in any existing sqlite tests
4. Define `TestGetSpendSummary` with subtests:
   - `"returns empty slice when no usage logs exist"` — creates a key, queries with a 30-day range, asserts `len(rows) == 0` (or no error at minimum)
   - `"excludes flush rows from aggregation"` — marked with `t.Skip("implement in Plan 1")` for now
   - `"filters by team_id"` — marked with `t.Skip("implement in Plan 1")` for now
   - `"filters by app_id"` — marked with `t.Skip("implement in Plan 1")` for now

The stub tests that ARE runnable (not skipped) may call `storage.GetSpendSummary` if the method exists on `storage.Storage` interface, OR may compile-only-check by calling `var _ storage.Storage = (*Storage)(nil)`. Since GetSpendSummary does not exist yet on the interface, the test file MUST NOT call it directly — instead, use build tags or define a local interface stub.

IMPORTANT: The simplest approach for Wave 0: do NOT call `GetSpendSummary` at all in this file yet. Instead create test skeleton functions with `t.Skip("TODO Plan 1")` that compile correctly. The import of `storage` package is still valid because `SpendRow` and `SpendFilters` types will be added in Plan 1 — so import the package but only reference types that already exist (or use anonymous imports).

SIMPLEST VALID APPROACH: Create the test file with properly structured Go test functions, all using `t.Skip("implement in Plan 1")`. Use only `context`, `testing`, `time` imports (no `storage` import until Plan 1 adds the types). This ensures `go test ./internal/storage/...` passes right now.

```go
package sqlite

import (
    "testing"
)

func TestGetSpendSummary(t *testing.T) {
    t.Run("returns empty slice when no usage logs exist", func(t *testing.T) {
        t.Skip("implement in Plan 1")
    })
    t.Run("excludes flush rows from aggregation", func(t *testing.T) {
        t.Skip("implement in Plan 1")
    })
    t.Run("filters by team_id", func(t *testing.T) {
        t.Skip("implement in Plan 1")
    })
    t.Run("filters by app_id", func(t *testing.T) {
        t.Skip("implement in Plan 1")
    })
}
```
  </action>
  <verify>
    <automated>cd /Users/pwagstro/Documents/workspace/simple_llm_proxy && go test ./internal/storage/... -v 2>&1 | head -30</automated>
  </verify>
  <done>go test ./internal/storage/... exits 0; TestGetSpendSummary subtests show as SKIP</done>
</task>

<task type="auto">
  <name>Task 2: Go handler test scaffold for AdminSpend</name>
  <files>internal/api/handler/spend_test.go</files>
  <action>
Create `internal/api/handler/spend_test.go` in package `handler`. This file creates test stubs that compile before `spend.go` exists (Plan 2 will implement it).

The file must:
1. Declare package `handler`
2. Import `"testing"` only for the stub phase
3. Define `TestAdminSpend` with subtests, all skipped:
   - `"returns 200 with aggregated spend rows for default 7d range"`
   - `"returns pre-computed alerts for keys over soft budget"`
   - `"returns 400 for malformed date params"`
   - `"filters by team_id query param"`

All subtests: `t.Skip("implement in Plan 2")`

The `mockStorage` struct in `models_test.go` already stubs all Storage methods. When Plan 2 adds `GetSpendSummary` to the interface, `mockStorage` will need a new stub method — that will be added in Plan 2's test work. For now, the file just needs to compile.

```go
package handler

import (
    "testing"
)

func TestAdminSpend(t *testing.T) {
    t.Run("returns 200 with aggregated spend rows for default 7d range", func(t *testing.T) {
        t.Skip("implement in Plan 2")
    })
    t.Run("returns pre-computed alerts for keys over soft budget", func(t *testing.T) {
        t.Skip("implement in Plan 2")
    })
    t.Run("returns 400 for malformed date params", func(t *testing.T) {
        t.Skip("implement in Plan 2")
    })
    t.Run("filters by team_id query param", func(t *testing.T) {
        t.Skip("implement in Plan 2")
    })
}
```
  </action>
  <verify>
    <automated>cd /Users/pwagstro/Documents/workspace/simple_llm_proxy && go test ./internal/api/handler/... -v 2>&1 | grep -E "SKIP|PASS|FAIL|TestAdminSpend"</automated>
  </verify>
  <done>go test ./internal/api/handler/... exits 0; TestAdminSpend subtests show as SKIP</done>
</task>

<task type="auto">
  <name>Task 3: Frontend test scaffolds for CostView and NavBar badge</name>
  <files>
    frontend/tests/unit/views/CostView.test.js
    frontend/tests/unit/components/NavBadge.test.js
  </files>
  <action>
Create two Vitest test stubs following the DashboardView.test.js and StatusBadge.test.js patterns.

**File 1: `frontend/tests/unit/views/CostView.test.js`**

This file tests `CostView.vue` which does not exist yet. It must NOT import CostView (which would fail). Instead, use a `describe.todo` pattern or a minimal passing test.

```javascript
import { describe, it } from 'vitest'

// CostView.vue will be implemented in Plan 4.
// These tests are stubs for Wave 0 — they pass trivially.
// Plan 4 will replace these with real assertions.
describe('CostView', () => {
  it.todo('renders LoadingSpinner while loading')
  it.todo('renders ErrorAlert on API failure')
  it.todo('renders Alerts Panel when alerts array is non-empty')
  it.todo('hides Alerts Panel when alerts array is empty')
  it.todo('renders breakdown table rows from spend data')
  it.todo('renders empty state when spend rows array is empty')
  it.todo('filter bar defaults to 7d date range selection')
  it.todo('re-fetches data when date range filter changes')
})
```

**File 2: `frontend/tests/unit/components/NavBadge.test.js`**

```javascript
import { describe, it } from 'vitest'

// NavBar badge behavior will be wired in Plan 3.
// These tests are stubs for Wave 0 — they pass trivially via .todo.
describe('NavBar Cost badge', () => {
  it.todo('renders numeric badge when alertCount > 0')
  it.todo('hides badge when alertCount is 0')
  it.todo('shows 9+ when alertCount > 9')
  it.todo('fetches alert count on NavBar mount')
})
```

Note: `it.todo` tests compile and pass in Vitest without any assertions. They appear as TODO in output, not as failures.
  </action>
  <verify>
    <automated>cd /Users/pwagstro/Documents/workspace/simple_llm_proxy/frontend && npm test -- --reporter=verbose 2>&1 | grep -E "todo|PASS|FAIL|CostView|NavBadge" | head -20</automated>
  </verify>
  <done>npm test exits 0; CostView and NavBadge test files appear in output as todo items (not failures)</done>
</task>

</tasks>

<verification>
After all three tasks:
- `go test ./...` passes with all new test functions showing as SKIP
- `cd frontend && npm test` passes with CostView and NavBadge test files showing todo items
- `go build ./...` succeeds (no compilation errors introduced)
</verification>

<success_criteria>
- go test ./internal/storage/... exits 0 with TestGetSpendSummary SKIP subtests visible
- go test ./internal/api/handler/... exits 0 with TestAdminSpend SKIP subtests visible
- cd frontend && npm test exits 0 with CostView.test.js and NavBadge.test.js showing todo items
- go build ./... exits 0
</success_criteria>

<output>
After completion, create `.planning/phases/03-cost-monitoring-complete-console/03-PLAN-0-SUMMARY.md`
</output>

## PLANNING COMPLETE
