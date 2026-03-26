---
phase: 03-cost-monitoring-complete-console
plan: 4
subsystem: frontend
tags: [vue3, apexcharts, cost-dashboard, filters, spend-column]
dependency_graph:
  requires:
    - 03-03 (api.spend() client method, backend /admin/spend endpoint)
  provides:
    - CostView.vue at /cost with full filter/chart/table/alerts UI
    - KeysView spend column showing real $X.XX / $Y.YY values
    - VueApexCharts global plugin registration
  affects:
    - frontend/src/main.js
    - frontend/src/views/CostView.vue
    - frontend/src/views/KeysView.vue
tech_stack:
  added:
    - apexcharts (npm)
    - vue3-apexcharts (npm)
  patterns:
    - Server-driven filter model: all filter changes trigger api.spend() with resolved IDs
    - Initial unfiltered fetch for dropdown population (separate from filtered data)
    - D-09 chart grouping: bars aggregated by narrowest active filter dimension
key_files:
  created:
    - frontend/src/views/CostView.vue
  modified:
    - frontend/src/main.js
    - frontend/src/views/KeysView.vue
    - frontend/tests/unit/views/CostView.test.js
decisions:
  - "VueApexCharts registered as global plugin in main.js — enables <apexchart> component in CostView without local import"
  - "All filter changes trigger server-side api.spend() call — no client-side filtering of response rows (D-07)"
  - "Dropdown options populated from initial unfiltered fetch on mount, not from filtered response rows"
  - "loadKeySpend is non-awaited in selectApp — spend column updates asynchronously without blocking key list render"
metrics:
  duration: "~6 minutes"
  completed: "2026-03-26"
  tasks: 2
  files: 4
---

# Phase 03 Plan 04: CostView Implementation Summary

**One-liner:** Full cost dashboard at /cost with server-driven filters, ApexCharts bar chart, alerts panel, breakdown table, and real spend display in Keys view.

## What Was Built

### Task 1: ApexCharts installation and global plugin registration
- Installed `apexcharts` and `vue3-apexcharts` via npm in `frontend/`
- Updated `frontend/src/main.js` to import `VueApexCharts` and register with `app.use(VueApexCharts)`
- Enables `<apexchart>` component globally — CostView does not need a local import

### Task 2: CostView.vue + KeysView spend column + real tests (TDD)

**CostView.vue** (`frontend/src/views/CostView.vue`):
- Full UI-SPEC layout: h1 "Cost", LoadingSpinner, ErrorAlert, then alerts panel + filter bar + chart card + table card
- Alerts panel: hidden when `alerts.length === 0`; shows amber/red rows per `alert_type`; "Budget Alerts" heading + "{N} key(s) require attention." summary
- Filter bar: Today/7d/30d/Custom date buttons (7d default, indigo-50 active state); Team/App/Key cascade dropdowns populated from initial unfiltered fetch; Reset Filters button when any non-default filter active
- Server-driven model: watcher on `[dateRange, selectedTeamId, selectedAppId, selectedKeyId]` calls `loadSpend()` on any change; custom date inputs debounced 300ms
- D-09 chart grouping: key/app → key bars; team → app bars; no filter → team aggregate bars
- Breakdown table: Name/Total Spend/Budget/% Budget/Status columns; rows from `spendData.rows` directly; empty state with context-aware copy

**KeysView.vue** (`frontend/src/views/KeysView.vue`):
- Added `keySpend = ref({})` state (map of key_id → total_spend)
- Added `loadKeySpend(appId)` function calling `api.spend({ appId })`
- `selectApp()` now calls `loadKeySpend(app.id)` after `loadKeys()`
- Budget column changed from placeholder "Budget: $Y.YY" to real "$X.XX / $Y.YY" (or "$X.XX / ∞" for unlimited)

**Tests** (`frontend/tests/unit/views/CostView.test.js`):
- Replaced all `it.todo` stubs with 8 real assertions
- Covers: error state (ErrorAlert), alerts panel hidden/shown, empty state, table row rendering, 7d default active, Reset Filters hidden, server refetch on filter change

## Test Results

```
CostView tests: 8/8 PASS
Go tests: all pass (go test ./...)
Frontend tests: 123/126 pass (3 pre-existing client.test.js failures for /v1/models and /v1/chat/completions endpoint paths — out of scope, pre-existed before this plan)
```

## Deviations from Plan

### Auto-fixed Issues

None — plan executed exactly as written.

### Auto-mode Checkpoint

**Checkpoint:** `checkpoint:human-verify` (Task 3)
**Disposition:** `⚡ Auto-approved` — `auto_advance: true` in config.json
**What was built:** Complete Phase 3 frontend — CostView at /cost, KeysView spend column, all CostView tests green.

## Known Stubs

None — all data is wired to real API calls (api.spend). The spend column in KeysView shows real values from the /admin/spend endpoint on app selection.

## Self-Check: PASSED

| Check | Result |
|-------|--------|
| `frontend/src/views/CostView.vue` | FOUND |
| `frontend/src/main.js` | FOUND |
| `frontend/src/views/KeysView.vue` | FOUND |
| `frontend/tests/unit/views/CostView.test.js` | FOUND |
| Commit `a8bfe73` (ApexCharts install) | FOUND |
| Commit `2f04257` (TDD RED tests) | FOUND |
| Commit `4d5d2dd` (CostView implementation) | FOUND |
