---
phase: 03-cost-monitoring-complete-console
plan: 3
subsystem: frontend
tags: [frontend, api-client, navigation, routing, cost-monitoring]
dependency_graph:
  requires: [03-PLAN-2]
  provides: [api.spend, StatusBadge-ok-warning-over, NavBar-cost-badge, /cost-route, CostView-stub]
  affects: [frontend/src/api/client.js, frontend/src/components/StatusBadge.vue, frontend/src/components/NavBar.vue, frontend/src/router/index.js]
tech_stack:
  added: []
  patterns: [vi.mock-hoisting-safe-factory, router.afterEach-for-badge-refresh, positive-integer-guard-for-ids]
key_files:
  created:
    - frontend/src/views/CostView.vue
  modified:
    - frontend/src/api/client.js
    - frontend/src/components/StatusBadge.vue
    - frontend/src/components/NavBar.vue
    - frontend/src/router/index.js
    - frontend/tests/unit/components/StatusBadge.test.js
    - frontend/tests/unit/components/NavBadge.test.js
    - frontend/tests/unit/components/NavBar.test.js
decisions:
  - "CostView.vue stub created so dynamic import resolves in test environment (Vite import-analysis runs at test time)"
  - "NavBadge tests use vi.mock factory pattern (no external variable references) to avoid hoisting errors"
  - "router.afterEach() chosen for badge refresh — works independently of component lifecycle, fires on every navigation"
metrics:
  duration: "~15min"
  completed: "2026-03-26"
  tasks_completed: 3
  files_changed: 7
---

# Phase 3 Plan 3: Frontend Infrastructure Wiring Summary

**One-liner:** api.spend() method with ID guards, StatusBadge extended for ok/warning/over budgets, NavBar Cost link with reactive alert badge refreshed on navigation, /cost route with CostView stub.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Add api.spend() to client.js | 148600f | frontend/src/api/client.js |
| 2 | Extend StatusBadge with ok/warning/over + tests | b14ef27 | frontend/src/components/StatusBadge.vue, frontend/tests/unit/components/StatusBadge.test.js |
| 3 | Cost nav link + badge + /cost route + CostView stub | 7ba3d24 | frontend/src/components/NavBar.vue, frontend/src/router/index.js, frontend/src/views/CostView.vue, frontend/tests/unit/components/NavBadge.test.js, frontend/tests/unit/components/NavBar.test.js |

## What Was Built

### api.spend() (client.js)
Added the `spend(params)` method to the api export object following the same pattern as `logs()`. Only positive integers (> 0) are added to the query string for teamId/appId/keyId — zero, NaN, and negative values are treated as "no filter" and omitted. Date range params (from/to) are passed as-is.

### StatusBadge Extension (StatusBadge.vue)
Extended all three computed properties (label, classes, dotClass) with three new budget status cases:
- `ok`: "OK" / bg-green-50 text-green-700 / bg-green-500 dot
- `warning`: "Warning" / bg-amber-50 text-amber-700 / bg-amber-500 dot
- `over`: "Over Budget" / bg-red-50 text-red-700 / bg-red-500 dot

Existing healthy/cooldown/unknown cases unchanged.

### NavBar Cost Badge (NavBar.vue)
- Added `alertCount` ref initialized to 0
- Added `fetchAlertCount()` function calling `api.spend()` and reading `data.alerts.length`
- Called `fetchAlertCount()` on mount via `onMounted`
- Refreshed on every route navigation via `router.afterEach()`
- Added `{ to: '/cost', label: 'Cost' }` to adminLinks after Keys
- Template updated: `v-for` on adminLinks now uses nested `<template>` to allow badge span inside router-link; added `relative` class to router-link for absolute badge positioning
- Badge: `v-if="link.to === '/cost' && alertCount > 0"`, shows count (1-9) or "9+" (>= 10)

### /cost Route (router/index.js)
Added `{ path: '/cost', name: 'cost', component: () => import('../views/CostView.vue') }` with dynamic import to defer resolution. Uses default requiresAuth behavior (session guard applies automatically via beforeEach).

### CostView.vue Stub (views/CostView.vue)
Minimal stub created so Vite's import-analysis can resolve the dynamic import at test time. Full implementation is Plan 4's responsibility.

## Test Results

- **Before plan:** 1 failed | 14 passed | 2 skipped (17 files) | 3 failed | 105 passed | 16 todo (124 tests)
- **After plan:** 1 failed | 15 passed | 1 skipped (17 files) | 3 failed | 115 passed | 11 todo (129 tests)
- **Net gain:** +10 passing tests, +1 file passing, 5 todo stubs converted to real tests
- The 3 pre-existing failures in client.test.js (wrong endpoint URL expectations for /v1/models and /v1/chat/completions) are unchanged and out of scope for this plan

### New tests added
- StatusBadge.test.js: 6 new tests (ok/warning/over label + classes)
- NavBadge.test.js: 4 new tests (badge count, hidden at 0, 9+, refresh on navigation)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Created CostView.vue stub to unblock test compilation**
- **Found during:** Task 3 — dynamic import in router/index.js caused Vite import-analysis error in test environment
- **Issue:** Even dynamic imports (`() => import('../views/CostView.vue')`) are analyzed at build/test time by Vite; file must exist for tests to compile
- **Fix:** Created minimal stub `frontend/src/views/CostView.vue` with placeholder template and comment noting Plan 4 implements it
- **Files modified:** frontend/src/views/CostView.vue (created)
- **Commit:** 7ba3d24

**2. [Rule 2 - Missing test mock] Added spend() to NavBar.test.js api mock**
- **Found during:** Task 3 — after adding fetchAlertCount() to NavBar, the existing NavBar.test.js mock for api.client only had `logout`
- **Issue:** NavBar now calls `api.spend()` on mount; without the mock, test environment may throw or produce stderr warnings
- **Fix:** Added `spend: vi.fn().mockResolvedValue({ rows: [], alerts: [], from: '', to: '' })` to the mock
- **Files modified:** frontend/tests/unit/components/NavBar.test.js
- **Commit:** 7ba3d24

## Known Stubs

- `frontend/src/views/CostView.vue` — stub with no data wiring. This is intentional: full CostView implementation is Plan 4's responsibility. The stub exists solely to allow the /cost route to compile and the test suite to pass.

## Self-Check: PASSED

Files exist:
- frontend/src/api/client.js — FOUND
- frontend/src/components/StatusBadge.vue — FOUND
- frontend/src/components/NavBar.vue — FOUND
- frontend/src/router/index.js — FOUND
- frontend/src/views/CostView.vue — FOUND
- frontend/tests/unit/components/StatusBadge.test.js — FOUND
- frontend/tests/unit/components/NavBadge.test.js — FOUND

Commits exist:
- 148600f — FOUND
- b14ef27 — FOUND
- 7ba3d24 — FOUND
