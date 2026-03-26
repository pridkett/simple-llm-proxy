---
phase: 03-cost-monitoring-complete-console
plan: 01
subsystem: storage
tags: [go, sqlite, tdd, spend-aggregation, storage-interface]

# Dependency graph
requires:
  - "03-00-PLAN (Wave 0 test scaffolds)"
provides:
  - "SpendRow struct with per-key spend data and JOIN-resolved names"
  - "SpendFilters struct with pointer *int64 fields for nil-means-no-filter contract"
  - "GetSpendSummary method on Storage interface and SQLite implementation"
  - "13 real TestGetSpendSummary subtests replacing Wave 0 t.Skip stubs"
affects:
  - 03-02-PLAN  # spend handler uses GetSpendSummary via Storage interface

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "SQL double-bind pattern: (? IS NULL OR col = ?) with Go nil pointer for absent filters"
    - "LEFT JOIN + COALESCE(SUM(ul.total_cost), 0) for zero-spend rows in aggregation"
    - "Explicit GROUP BY all non-aggregated columns for cross-engine SQL correctness"

key-files:
  created:
    - internal/storage/sqlite/spend.go
  modified:
    - internal/storage/storage.go
    - internal/api/handler/models_test.go
    - internal/api/handler/auth_test.go

key-decisions:
  - "SpendFilters uses *int64 pointer types — nil means no filter, not zero (zero is a valid-but-impossible ID at this layer)"
  - "Only active keys (is_active=TRUE) appear in GetSpendSummary — deactivated keys are intentionally excluded from spend views"
  - "LEFT JOIN usage_logs with flush exclusion (model!='_flush') + COALESCE gives zero-spend rows without a separate query"

requirements-completed: [COST-02, COST-05]

# Metrics
duration: 8min
completed: 2026-03-26
---

# Phase 03 Plan 01: Spend Aggregation Storage Layer Summary

**SQLite implementation of GetSpendSummary with SpendRow/SpendFilters types, 13 real tests replacing Wave 0 skips, and date-range + optional-filter support via the SQL double-bind pattern**

## Performance

- **Duration:** ~8 min
- **Completed:** 2026-03-26
- **Tasks:** 2 completed
- **Files modified:** 4

## Accomplishments

- Added `SpendFilters` and `SpendRow` types to `internal/storage/storage.go`
- Added `GetSpendSummary` to the `Storage` interface
- Created `internal/storage/sqlite/spend.go` with LEFT JOIN implementation:
  - Flush rows (`model='_flush'`) excluded from aggregation
  - COALESCE ensures zero-spend keys still appear
  - Explicit GROUP BY all non-aggregated columns
  - SQL double-bind pattern `(? IS NULL OR col = ?)` for optional filters
  - Only active keys (`is_active = TRUE`) returned
- Replaced all 9 `t.Skip("implement in Plan 1")` stubs with 13 real PASS assertions
- Fixed compilation for `mockAuthStore` in `auth_test.go` (Rule 3 deviation)

## Task Commits

1. **Task 1: Storage interface types + SQLite implementation** - `b692ff3` (feat)
2. **Task 2: Real tests replacing Wave 0 skips** - `b459f86` (test)

## Files Created/Modified

- `internal/storage/sqlite/spend.go` — GetSpendSummary implementation (66 lines)
- `internal/storage/storage.go` — SpendRow, SpendFilters types, GetSpendSummary interface method
- `internal/api/handler/models_test.go` — GetSpendSummary stub for mockStorage
- `internal/api/handler/auth_test.go` — GetSpendSummary stub for mockAuthStore

## Decisions Made

- `SpendFilters` uses `*int64` pointer types for all filter dimensions. The SQL double-bind pattern requires Go to pass `nil` (not `0`) for absent filters; using pointer types enforces this contract at the type level. The comment in storage.go documents why zero is invalid.
- Only active keys appear in `GetSpendSummary`. Deactivated keys have `is_active=FALSE` and are excluded via `WHERE k.is_active = TRUE`. This is documented as an intentional simplification in spend.go; historical reporting for deactivated keys is deferred.
- `LEFT JOIN usage_logs ... ON ... model != '_flush' AND request_time >= ? AND request_time < ?` placed inside the JOIN condition (not WHERE) ensures that zero-spend keys still appear in results — if placed in WHERE, rows without matching usage_logs would be filtered out entirely.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Fixed missing GetSpendSummary stub in auth_test.go**
- **Found during:** Task 2 verification (`go test ./internal/api/handler/...`)
- **Issue:** `mockAuthStore` in `auth_test.go` also implements `storage.Storage` but had no stub for the new `GetSpendSummary` method — caused build failure
- **Fix:** Added `GetSpendSummary` stub to `mockAuthStore` matching the same signature as the one added to `mockStorage` in Task 1
- **Files modified:** `internal/api/handler/auth_test.go`
- **Commit:** b459f86 (included with Task 2 commit)

## Known Stubs

None — all behavior paths are implemented and tested.

## Self-Check: PASSED

- FOUND: internal/storage/sqlite/spend.go
- FOUND: internal/storage/storage.go (SpendRow, SpendFilters, GetSpendSummary)
- FOUND: internal/api/handler/models_test.go (GetSpendSummary stub)
- FOUND: internal/api/handler/auth_test.go (GetSpendSummary stub)
- FOUND commit b692ff3: feat(03-01): add SpendRow, SpendFilters types and GetSpendSummary to Storage interface
- FOUND commit b459f86: test(03-01): implement real GetSpendSummary tests replacing Wave 0 skips

---
*Phase: 03-cost-monitoring-complete-console*
*Completed: 2026-03-26*
