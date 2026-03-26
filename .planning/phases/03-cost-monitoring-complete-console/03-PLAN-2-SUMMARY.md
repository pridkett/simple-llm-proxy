---
phase: 03-cost-monitoring-complete-console
plan: 2
subsystem: backend-handler
tags: [go, handler, spend, admin-api, auth, tdd]
dependency_graph:
  requires:
    - 03-01-PLAN.md  # GetSpendSummary storage interface + SQLite implementation
  provides:
    - GET /admin/spend endpoint (admin-only, JSON spend summary with alerts)
    - AdminSpend handler function (exported, handler factory pattern)
    - spendResponse / spendAlert types
  affects:
    - internal/api/handler/admin_routes.go (new route registered)
tech_stack:
  added: []
  patterns:
    - handler factory (AdminSpend(store storage.Storage) http.HandlerFunc)
    - per-handler admin-only guard via middleware.UserFromContext (same as AdminUsers)
    - inclusive-to / exclusive-SQL date semantics with parseSpendDate + AddDate(0,0,1)
    - alert computation in handler (not SQL): hard budget precedence over soft
    - parseOptionalInt64 returns nil for "" or "0" to satisfy SQL double-bind pattern
key_files:
  created:
    - internal/api/handler/spend.go
  modified:
    - internal/api/handler/spend_test.go
    - internal/api/handler/admin_routes.go
decisions:
  - Admin-only guard is per-handler (middleware.UserFromContext), consistent with AdminUsers/AdminTeams pattern — not a route-group-level check
  - Auth tests use newRequestWithUser() to inject user context, consistent with users_test.go pattern
  - unauthenticated unit test verifies 403 (not 401) since RequireSession middleware is not running in unit tests; 401 is covered by middleware integration
  - parseSpendDate uses time.ParseInLocation with time.UTC to ensure consistent date boundaries
metrics:
  duration: ~10min
  completed: "2026-03-26"
  tasks: 2
  files: 3
---

# Phase 3 Plan 2: AdminSpend Handler Summary

**One-liner:** `GET /admin/spend` handler with inclusive date range, alert computation (soft/hard), and per-handler admin-only enforcement via `middleware.UserFromContext`.

## What Was Built

### Task 1: AdminSpend handler + tests (TDD)

Created `internal/api/handler/spend.go` implementing the `AdminSpend` handler factory:

- Parses `from`/`to` query params as `YYYY-MM-DD`; defaults to today-7d/today
- Converts user-facing inclusive `to` date to exclusive SQL upper bound (`+1 day` via `AddDate`)
- Returns user-facing inclusive `to` date in JSON response (`"to": "2026-03-26"` not `"2026-03-27"`)
- `parseOptionalInt64` returns nil for `""` or `"0"` — satisfies SQL `? IS NULL OR col = ?` double-bind
- Alert computation in handler: hard budget takes precedence over soft; each key appears once
- Admin-only guard via `middleware.UserFromContext` (nil user or `IsAdmin=false` → 403)

Replaced Wave 0 `t.Skip` stubs in `spend_test.go` with 9 real subtests covering:
- 200 with default date range, empty rows/alerts
- Soft and hard alert computation (separate subtests, hard-takes-precedence verified)
- 400 for malformed date param
- team_id=42 filter propagated to storage; team_id=0 passed as nil
- SQL to-bound is day+1 of user-facing to; response to field is user-facing inclusive date
- non-admin → 403; no-user-in-context → 403

### Task 2: Route registration

Added to `RegisterAdminRoutes` in `admin_routes.go`:

```go
r.Get("/admin/spend", AdminSpend(store))
```

Placed after key management routes with authorization comment explaining the two-layer auth model (RequireSession for 401, per-handler check for 403).

## Deviations from Plan

### Auto-fixed Issues

None — implementation followed plan exactly.

**Auth test adjustment (not a deviation):** Plan suggested auth tests "should exercise the middleware" if admin enforcement is middleware-level only. Upon reading the existing pattern (AdminUsers, AdminTeams all use per-handler checks), it was confirmed the check is in-handler. Tests use `newRequestWithUser()` exactly as other admin handler tests do. The `unauthenticated request returns 401` subtest was renamed to clarify it tests the handler-level 403 (no session = nil user = 403), with a comment explaining that the true 401 is covered by RequireSession middleware which is not running in unit tests.

## Known Stubs

None — the handler is fully implemented and wired to `store.GetSpendSummary`.

## Self-Check: PASSED

Files created/modified:
- `internal/api/handler/spend.go` — FOUND
- `internal/api/handler/spend_test.go` — FOUND (modified)
- `internal/api/handler/admin_routes.go` — FOUND (modified, contains `AdminSpend`)

Commits:
- `e32d4aa` feat(03-02): AdminSpend handler with date parsing, alert computation, and auth enforcement — FOUND
- `515d77f` feat(03-02): register GET /admin/spend route in RegisterAdminRoutes — FOUND

Verification:
- `go build ./...` exits 0
- `go test ./internal/api/handler/... -run TestAdminSpend` — all 9 subtests PASS
- `go test ./...` — all packages pass, no regressions
