---
phase: 02-api-keys-enforcement
plan: 06
subsystem: frontend
tags: [vue, keys, ui, api-client, routing]
dependency_graph:
  requires: [02-00, 02-01, 02-02, 02-03, 02-04, 02-05]
  provides: [keys-management-ui, keys-api-client-methods]
  affects: [frontend/src/views/KeysView.vue, frontend/src/api/client.js, frontend/src/router/index.js, frontend/src/components/NavBar.vue]
tech_stack:
  added: []
  patterns: [three-column-drilldown, inline-revoke-confirmation, post-creation-modal, input-normalization]
key_files:
  created:
    - frontend/src/views/KeysView.vue
  modified:
    - frontend/src/api/client.js
    - frontend/src/router/index.js
    - frontend/src/components/NavBar.vue
decisions:
  - "Budget column shows 'Budget: $Y.YY' for capped keys and 'Unlimited' for uncapped — avoids misleading '$0.00 / $Y.YY' until spend API available (Phase 3)"
  - "Allowed models normalized on submit: trim, deduplicate, filter empty tokens before API call"
  - "Soft budget validation: client-side check soft < hard with inline error message"
  - "Rate limit inputs use step=1 to enforce integer entry; budget inputs use step=0.01"
metrics:
  duration: ~28min
  completed_date: "2026-03-26"
  tasks_completed: 2
  tasks_total: 3
  files_changed: 4
---

# Phase 02 Plan 06: Keys Frontend View Summary

**One-liner:** Vue 3 three-column Keys view (Team > App > Keys) with create-key form, post-creation modal, inline revoke, and three api client methods; review feedback incorporated for input normalization, spend display, and frontend validation.

---

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | API client methods, router route, NavBar link | b59b988 | client.js, router/index.js, NavBar.vue |
| 2 | KeysView.vue — three-column layout, create form, modal, revoke inline | 0a0354b | KeysView.vue |

**Task 3** (checkpoint:human-verify) — paused for human verification. See checkpoint message below.

---

## What Was Built

### Task 1: API Client + Routing

- `client.js`: Added `apiKeys(appId)`, `createAPIKey(appId, body)`, `revokeAPIKey(keyId)` methods following the existing `request()` pattern with credentials: include.
- `router/index.js`: Added `/keys` route importing `KeysView` component.
- `NavBar.vue`: Added `{ to: '/keys', label: 'Keys' }` to `adminLinks` array, after Applications.

### Task 2: KeysView.vue

Three-column drill-down layout: Team list (w-64) | App list (w-64) | Keys panel (flex-1).

**Keys table columns:** Prefix (font-mono), Name, Models, Budget, Status badge, Actions.

**Budget display (review feedback incorporated):** Shows "Budget: $Y.YY" for capped keys and "Unlimited" for uncapped — avoids the misleading "$0.00 / $Y.YY" display that implies no spend has occurred. Phase 3 comment added inline.

**Create Key form fields:**
- Name (required, disables submit when empty)
- Allowed models (comma-separated text input)
- Rate limit (req/min) — integer, min=0
- Rate limit (req/day) — integer, min=0
- Hard budget ($) — decimal, step=0.01, min=0
- Soft budget ($) — decimal, step=0.01, min=0; inline error if >= hard budget

**Post-creation modal:** Fixed overlay (no Escape/overlay dismiss), yellow warning banner, font-mono select-all key display, Copy Key → Copied! (2s) / Done buttons.

**Inline revoke confirmation:** data-testid="revoke-key-{id}" trigger / data-testid="confirm-revoke-{id}" confirm, "Revoke key" (red) / "Keep key" (gray) pattern.

---

## Deviations from Plan

### Review Feedback Incorporated (not deviations — pre-planned improvements)

**1. [Review - Input Normalization] Allowed models normalized before API submission**
- Split on comma, trim each token, filter empty strings, deduplicate with Set
- Applied in `handleCreateKey()` before constructing request body

**2. [Review - Spend Display] Budget column shows "Budget: $Y.YY" not "$0.00 / $Y.YY"**
- Plan Task 2 action spec said to show "$0.00 / $Y.YY" for capped keys
- Review feedback correctly identified this as misleading
- Changed to show "Budget: $Y.YY" for capped, "Unlimited" for uncapped
- Added inline comment: "Phase 3: fetch spend totals from cost API and display '$X.XX / $Y.YY'"

**3. [Review - Frontend Validation] Added soft budget < hard budget client-side validation**
- `softBudgetError` computed property checks soft >= hard when both are set
- Inline error text below soft budget field: "Must be less than the hard budget limit."
- Form submit also checks `softBudgetError` and sets `formError` message

**4. [Review - Rate Limits] Rate limit inputs use `step="1"` for integer constraint**
- Enforces integer-only input at the HTML level
- Budget inputs use `step="0.01"` for decimal precision

### Out-of-Scope Pre-existing Test Failures

**Pre-existing failures in `client.test.js` (3 tests):** Tests expect `/v1/models` and `/v1/chat/completions` but actual routes are `/admin/models` and `/admin/chat/completions`. These failures existed before this plan and are not caused by Task 1 or Task 2 changes. Logged to deferred-items.

---

## Known Stubs

**Budget/Spend column:** Shows "Budget: $Y.YY" or "Unlimited" rather than actual spend because the keys list endpoint does not return per-key spend totals. This is intentional and documented with a Phase 3 comment:
```
<!-- Phase 3: fetch spend totals from cost API and display "$X.XX / $Y.YY" -->
```
File: `frontend/src/views/KeysView.vue`, Budget column `<td>` (around line 96).

**Models column:** Shows "All models" italic for all keys because the keys list API does not return allowed_models in the list response. This is intentional and documented with a Phase 3 comment:
```
<!-- Phase 3: show allowed model count once API includes it in list response -->
```
File: `frontend/src/views/KeysView.vue`, Models column `<td>` (around line 91).

These stubs do NOT prevent the plan's goal (key create/view/revoke via UI) from being achieved. They are placeholders for spend and model display that require additional backend API surface (out of scope for Phase 2).

---

## Self-Check: PASSED

- FOUND: frontend/src/views/KeysView.vue
- FOUND: frontend/src/api/client.js
- FOUND: frontend/src/router/index.js
- FOUND: frontend/src/components/NavBar.vue
- FOUND commit: b59b988 (Task 1)
- FOUND commit: 0a0354b (Task 2)
