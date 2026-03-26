---
phase: 03-cost-monitoring-complete-console
plan: 3
type: execute
wave: 3
depends_on:
  - "03-PLAN-2"
files_modified:
  - frontend/src/api/client.js
  - frontend/src/components/StatusBadge.vue
  - frontend/src/components/NavBar.vue
  - frontend/src/router/index.js
  - frontend/tests/unit/components/NavBadge.test.js
  - frontend/tests/unit/components/StatusBadge.test.js
autonomous: true
requirements:
  - COST-02
  - COST-04
  - UI-05
  - UI-06

must_haves:
  truths:
    - "api.spend(params) calls GET /admin/spend with correct query string"
    - "api.spend() omits zero/negative IDs from query string (not ?team_id=0 or ?team_id=NaN)"
    - "StatusBadge renders 'OK' in green, 'Warning' in amber, 'Over Budget' in red for new statuses"
    - "NavBar shows a 'Cost' link in adminLinks after 'Keys'"
    - "NavBar 'Cost' link shows a red numeric badge when alertCount > 0"
    - "Badge shows '9+' when alertCount >= 10"
    - "Badge is hidden (not rendered) when alertCount === 0"
    - "Badge refreshes when user navigates to a new route (not stale from initial mount)"
    - "/cost route is defined in the Vue router with admin guard"
    - "cd frontend && npm test passes with updated StatusBadge and NavBadge tests"
  artifacts:
    - path: "frontend/src/api/client.js"
      provides: "api.spend(params) method with positive-integer-only ID filtering"
      contains: "spend"
    - path: "frontend/src/components/StatusBadge.vue"
      provides: "Extended with ok/warning/over statuses"
    - path: "frontend/src/components/NavBar.vue"
      provides: "Cost adminLink with reactive badge count, refreshed on navigation"
    - path: "frontend/src/router/index.js"
      provides: "/cost route pointing to CostView"
  key_links:
    - from: "frontend/src/components/NavBar.vue"
      to: "/admin/spend"
      via: "api.spend() call on mount and on route navigation to get alertCount"
      pattern: "api.spend"
    - from: "frontend/src/router/index.js"
      to: "frontend/src/views/CostView.vue"
      via: "import CostView + route /cost"
      pattern: "CostView"
---

<objective>
Wire all frontend infrastructure needed by CostView before building the view itself: add `api.spend()` to the API client, extend StatusBadge with the three new budget statuses (ok/warning/over), add the "Cost" admin nav link with its reactive badge to NavBar (including route-navigation refresh), and register the `/cost` route.

Purpose: Plan 4 (CostView.vue) imports all of these — building wiring first makes Plan 4 a clean implementation task with no cross-file dependencies to resolve.
Output: All frontend infrastructure for Phase 3 exists and passes tests. Plan 4 can import and use these without modification.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/03-cost-monitoring-complete-console/03-CONTEXT.md
@.planning/phases/03-cost-monitoring-complete-console/03-UI-SPEC.md
@.planning/phases/03-cost-monitoring-complete-console/03-PLAN-2-SUMMARY.md
@frontend/src/api/client.js
@frontend/src/components/StatusBadge.vue
@frontend/src/components/NavBar.vue
@frontend/src/router/index.js
@frontend/tests/unit/components/StatusBadge.test.js
@frontend/tests/unit/components/NavBadge.test.js
</context>

<interfaces>
<!-- Backend API contract (from Plan 2) -->
GET /admin/spend Response shape:
```json
{
  "rows": [
    {
      "key_id": 1, "key_name": "my-key",
      "app_id": 2, "app_name": "my-app",
      "team_id": 3, "team_name": "my-team",
      "total_spend": 1.234,
      "max_budget": 10.0,
      "soft_budget": 8.0
    }
  ],
  "alerts": [
    {
      "key_id": 1, "key_name": "my-key",
      "app_name": "my-app", "team_name": "my-team",
      "total_spend": 9.5, "soft_budget": 8.0, "max_budget": 10.0,
      "alert_type": "soft"
    }
  ],
  "from": "2026-03-19",
  "to": "2026-03-26"
}
```

From frontend/src/api/client.js (established request pattern):
```javascript
// All admin calls use request(path, options) which attaches credentials:include cookie
async function request(path, options = {}) { ... }

// Param encoding pattern (from logs()):
const qs = new URLSearchParams()
if (params.limit) qs.set('limit', String(params.limit))
```

From frontend/src/components/NavBar.vue (existing adminLinks pattern):
```javascript
const adminLinks = [
  { to: '/users', label: 'Users' },
  { to: '/teams', label: 'Teams' },
  { to: '/applications', label: 'Applications' },
  { to: '/keys', label: 'Keys' },
  // ADD: { to: '/cost', label: 'Cost' }
]
```

From frontend/src/components/StatusBadge.vue (existing switch pattern to extend):
```javascript
const label = computed(() => {
  switch (props.status) {
    case 'healthy': return 'Healthy'
    case 'cooldown': return 'Cooldown'
    default: return 'Unknown'
  }
})
```

UI-SPEC NavBar badge markup (per UI-SPEC.md):
```html
<span
  v-if="link.to === '/cost' && alertCount > 0"
  class="absolute -top-1 -right-1 flex h-4 w-4 items-center justify-center rounded-full bg-red-500 text-white text-[10px] font-semibold"
>
  {{ alertCount > 9 ? '9+' : alertCount }}
</span>
```
</interfaces>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Add api.spend() to client.js</name>
  <files>frontend/src/api/client.js</files>
  <behavior>
    - api.spend() with no params calls GET /admin/spend (no query string)
    - api.spend({ from: '2026-01-01', to: '2026-01-31' }) calls GET /admin/spend?from=2026-01-01&to=2026-01-31
    - api.spend({ teamId: 1 }) calls GET /admin/spend?team_id=1
    - api.spend({ appId: 2, keyId: 3 }) calls GET /admin/spend?app_id=2&key_id=3
    - api.spend({ teamId: 0 }) does NOT include team_id in query string (zero is not a valid ID)
    - api.spend({ teamId: NaN }) does NOT include team_id in query string
    - api.spend({ teamId: -1 }) does NOT include team_id in query string (negative IDs invalid)
    - Only positive integer IDs (> 0) are included in the query string
    - Returns the parsed JSON response object (rows + alerts + from + to)
  </behavior>
  <action>
Open `frontend/src/api/client.js` and add the `spend` method to the `api` export object. Place it after the `updateAPIKey` method (the last existing method), before the closing `}` of the `api` object.

Use explicit positive-integer checks (not falsy checks) to prevent zero, NaN, or negative values from reaching the query string:

```javascript
/**
 * GET /admin/spend — returns aggregated spend data with pre-computed alerts.
 * @param {{ from?: string, to?: string, teamId?: number, appId?: number, keyId?: number }} params
 *   from/to: YYYY-MM-DD strings (user-facing inclusive dates).
 *   teamId/appId/keyId: positive integer IDs for filtering.
 *   Only positive integers (> 0) are sent as query params. Zero, NaN, and negative
 *   values are treated as "no filter" and omitted from the query string.
 */
spend(params = {}) {
  const qs = new URLSearchParams()
  if (params.from) qs.set('from', params.from)
  if (params.to) qs.set('to', params.to)
  if (params.teamId && params.teamId > 0) qs.set('team_id', String(params.teamId))
  if (params.appId && params.appId > 0) qs.set('app_id', String(params.appId))
  if (params.keyId && params.keyId > 0) qs.set('key_id', String(params.keyId))
  const query = qs.toString() ? `?${qs}` : ''
  return request(`/admin/spend${query}`)
},
```

Note: The existing `client.test.js` tests cover the `request` helper. No changes to client.test.js are needed — `spend()` follows the exact same pattern as `logs()`.
  </action>
  <verify>
    <automated>cd /Users/pwagstro/Documents/workspace/simple_llm_proxy/frontend && npm test -- --reporter=verbose 2>&1 | grep -E "PASS|FAIL|client" | head -10</automated>
  </verify>
  <done>npm test exits 0. No existing client.test.js tests broken. api.spend is exported from client.js with positive-integer ID guard.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Extend StatusBadge with ok/warning/over statuses + update tests</name>
  <files>
    frontend/src/components/StatusBadge.vue
    frontend/tests/unit/components/StatusBadge.test.js
  </files>
  <behavior>
    - status='ok' renders text "OK" with bg-green-50 text-green-700 bg-green-500 dot (per UI-SPEC)
    - status='warning' renders text "Warning" with bg-amber-50 text-amber-700 bg-amber-500 dot
    - status='over' renders text "Over Budget" with bg-red-50 text-red-700 bg-red-500 dot
    - Existing statuses (healthy, cooldown) still render correctly — no regressions
    - Unknown status still falls through to "Unknown" gray
  </behavior>
  <action>
**Update `frontend/src/components/StatusBadge.vue`:**

Add the three new cases to all three computed properties (`label`, `classes`, `dotClass`). The `props.status` comment should also be updated to list the new valid values.

```javascript
// In label computed:
case 'ok': return 'OK'
case 'warning': return 'Warning'
case 'over': return 'Over Budget'

// In classes computed:
case 'ok': return 'bg-green-50 text-green-700'
case 'warning': return 'bg-amber-50 text-amber-700'
case 'over': return 'bg-red-50 text-red-700'

// In dotClass computed:
case 'ok': return 'bg-green-500'
case 'warning': return 'bg-amber-500'
case 'over': return 'bg-red-500'
```

Also update the props comment: `// 'healthy' | 'cooldown' | 'unknown' | 'ok' | 'warning' | 'over'`

**Update `frontend/tests/unit/components/StatusBadge.test.js`:**

Add test cases for the three new statuses following the existing pattern:

```javascript
it('renders "OK" for status ok', () => {
  const wrapper = mount(StatusBadge, { props: { status: 'ok' } })
  expect(wrapper.text()).toBe('OK')
})

it('applies green classes for ok status', () => {
  const wrapper = mount(StatusBadge, { props: { status: 'ok' } })
  const span = wrapper.find('span')
  expect(span.classes()).toContain('bg-green-50')
  expect(span.classes()).toContain('text-green-700')
})

it('renders "Warning" for status warning', () => {
  const wrapper = mount(StatusBadge, { props: { status: 'warning' } })
  expect(wrapper.text()).toBe('Warning')
})

it('applies amber classes for warning status', () => {
  const wrapper = mount(StatusBadge, { props: { status: 'warning' } })
  const span = wrapper.find('span')
  expect(span.classes()).toContain('bg-amber-50')
  expect(span.classes()).toContain('text-amber-700')
})

it('renders "Over Budget" for status over', () => {
  const wrapper = mount(StatusBadge, { props: { status: 'over' } })
  expect(wrapper.text()).toBe('Over Budget')
})

it('applies red classes for over status', () => {
  const wrapper = mount(StatusBadge, { props: { status: 'over' } })
  const span = wrapper.find('span')
  expect(span.classes()).toContain('bg-red-50')
  expect(span.classes()).toContain('text-red-700')
})
```
  </action>
  <verify>
    <automated>cd /Users/pwagstro/Documents/workspace/simple_llm_proxy/frontend && npm test -- --reporter=verbose 2>&1 | grep -E "StatusBadge|PASS|FAIL" | head -20</automated>
  </verify>
  <done>All StatusBadge tests pass including the 6 new cases. Existing healthy/cooldown/unknown cases still pass.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: Add Cost link + reactive badge to NavBar + badge refresh on navigation + register /cost route</name>
  <files>
    frontend/src/components/NavBar.vue
    frontend/src/router/index.js
    frontend/tests/unit/components/NavBadge.test.js
  </files>
  <behavior>
    - adminLinks array contains { to: '/cost', label: 'Cost' } after { to: '/keys', label: 'Keys' }
    - NavBar calls api.spend() on mount (for the default 7d window) to get the alert count
    - alertCount ref = response.alerts.length (0 if call fails — badge hidden gracefully)
    - Badge element is v-if="alertCount > 0" — not rendered when 0
    - Badge text is alertCount when 1-9, '9+' when >= 10
    - Badge classes: absolute -top-1 -right-1 flex h-4 w-4 items-center justify-center rounded-full bg-red-500 text-white text-[10px] font-semibold
    - Cost link uses relative positioning container so badge positions correctly
    - Badge refreshes when user navigates to a new route — uses Vue Router afterEach or watch on route.path
    - The Cost nav link renders with the same active/inactive classes as other admin links
    - /cost route in router/index.js points to CostView component (dynamic import)
    - /cost route uses default requiresAuth behavior (session guard applies automatically)
  </behavior>
  <action>
**Update `frontend/src/components/NavBar.vue`:**

1. Add `onMounted`, `watch` to imports from 'vue': `import { onMounted, ref, watch } from 'vue'`
2. Add `useRoute` to imports from 'vue-router': `import { useRouter, useRoute } from 'vue-router'` (or check existing imports)
3. Add `api` import (already imported from client.js — verify it's there)
4. Add `alertCount` ref: `const alertCount = ref(0)`
5. Create a reusable `fetchAlertCount` function:

```javascript
async function fetchAlertCount() {
  try {
    const data = await api.spend()
    if (data && data.alerts) {
      alertCount.value = data.alerts.length
    }
  } catch {
    // Badge silently stays at 0 on error — non-critical
  }
}
```

6. Call `fetchAlertCount()` on mount:

```javascript
onMounted(fetchAlertCount)
```

7. Refresh badge on every route navigation. Use Vue Router's `afterEach` hook or watch the current route path. The `afterEach` approach is preferred as it works independently of component lifecycle:

```javascript
// Refresh alert badge whenever the user navigates to a new page.
// This prevents the badge from going stale when spend changes or the user
// returns to the console after time has passed.
const router = useRouter()
router.afterEach(() => {
  fetchAlertCount()
})
```

Alternatively, use watch on route.path if useRouter is not already imported:
```javascript
const route = useRoute()
watch(() => route.path, fetchAlertCount)
```

Use whichever pattern is consistent with how NavBar already uses the router.

8. Add `{ to: '/cost', label: 'Cost' }` to `adminLinks` after the Keys entry.

9. The Cost link in the template needs special treatment for the badge. Replace the generic `v-for="link in adminLinks"` router-link block with a version that handles the Cost link badge. The simplest approach: render adminLinks with the existing v-for, but add a conditional badge span for the Cost link only:

Per UI-SPEC.md NavBar badge layout:
```html
<template v-if="currentUser?.is_admin">
  <template v-for="link in adminLinks" :key="link.to">
    <router-link
      :to="link.to"
      class="relative px-3 py-2 rounded-md text-sm font-medium transition-colors"
      :class="$route.path === link.to
        ? 'bg-indigo-50 text-indigo-700'
        : 'text-gray-600 hover:text-gray-900 hover:bg-gray-50'"
    >
      {{ link.label }}
      <span
        v-if="link.to === '/cost' && alertCount > 0"
        class="absolute -top-1 -right-1 flex h-4 w-4 items-center justify-center rounded-full bg-red-500 text-white text-[10px] font-semibold"
      >
        {{ alertCount > 9 ? '9+' : alertCount }}
      </span>
    </router-link>
  </template>
</template>
```

Note: The existing `<router-link v-for="link in adminLinks">` uses `class="px-3 py-2 ..."` (no `relative`). Adding `relative` is required for the absolutely-positioned badge to work. Update the class binding to include `relative` as a static class.

**Update `frontend/src/router/index.js`:**

1. Add route to the `routes` array (after the keys route) using a dynamic import to avoid build errors before CostView.vue is created:
```javascript
{ path: '/cost', name: 'cost', component: () => import('../views/CostView.vue') },
```

Dynamic import defers the module resolution — the router compiles even if the file doesn't exist yet.

**Update `frontend/tests/unit/components/NavBadge.test.js`:**

Replace the `it.todo` stubs with real tests. This file tests NavBar's Cost badge behavior — there is no standalone NavBadge component; the describe block is already named `NavBar Cost badge` to make this clear.

Mount NavBar with a mocked `api.spend()`:

```javascript
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createWebHashHistory } from 'vue-router'
import NavBar from '@/components/NavBar.vue'

function makeRouter() {
  return createRouter({
    history: createWebHashHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/cost', component: { template: '<div />' } },
      { path: '/dashboard', component: { template: '<div />' } },
    ],
  })
}

// Tests NavBar's Cost link badge behavior.
// There is no standalone NavBadge component — this tests the badge as part of NavBar.
describe('NavBar Cost badge', () => {
  let fetchMock

  beforeEach(() => {
    fetchMock = vi.fn()
    global.fetch = fetchMock
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders numeric badge when alertCount > 0', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: true, status: 200,
      json: () => Promise.resolve({ rows: [], alerts: [{ key_id: 1 }], from: '', to: '' }),
    })
    // Also mock /admin/me for session state
    fetchMock.mockResolvedValueOnce({ ok: true, status: 200, json: () => Promise.resolve(null) })

    const router = makeRouter()
    const wrapper = mount(NavBar, { global: { plugins: [router] } })
    await flushPromises()
    const badge = wrapper.find('.bg-red-500.rounded-full')
    expect(badge.exists()).toBe(true)
    expect(badge.text()).toBe('1')
  })

  it('hides badge when alertCount is 0', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: true, status: 200,
      json: () => Promise.resolve({ rows: [], alerts: [], from: '', to: '' }),
    })
    const router = makeRouter()
    const wrapper = mount(NavBar, { global: { plugins: [router] } })
    await flushPromises()
    const badge = wrapper.find('.bg-red-500.rounded-full')
    expect(badge.exists()).toBe(false)
  })

  it('shows 9+ when alertCount >= 10', async () => {
    const manyAlerts = Array.from({ length: 10 }, (_, i) => ({ key_id: i + 1 }))
    fetchMock.mockResolvedValueOnce({
      ok: true, status: 200,
      json: () => Promise.resolve({ rows: [], alerts: manyAlerts, from: '', to: '' }),
    })
    const router = makeRouter()
    const wrapper = mount(NavBar, { global: { plugins: [router] } })
    await flushPromises()
    const badge = wrapper.find('.bg-red-500.rounded-full')
    expect(badge.exists()).toBe(true)
    expect(badge.text()).toBe('9+')
  })

  it('refreshes alert count on route navigation', async () => {
    // First load: 0 alerts (badge hidden)
    fetchMock.mockResolvedValueOnce({
      ok: true, status: 200,
      json: () => Promise.resolve({ rows: [], alerts: [], from: '', to: '' }),
    })
    const router = makeRouter()
    const wrapper = mount(NavBar, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.find('.bg-red-500.rounded-full').exists()).toBe(false)

    // Navigation triggers re-fetch: now 1 alert
    fetchMock.mockResolvedValueOnce({
      ok: true, status: 200,
      json: () => Promise.resolve({ rows: [], alerts: [{ key_id: 1 }], from: '', to: '' }),
    })
    await router.push('/dashboard')
    await flushPromises()
    expect(wrapper.find('.bg-red-500.rounded-full').exists()).toBe(true)
  })
})
```

Note: NavBar tests are complex because the component uses `useSession` (which calls `/admin/me`). If the existing NavBar.test.js shows how to handle this, follow that pattern. The key insight: fetch will be called multiple times (once for /admin/me via useSession on mount, once for /admin/spend via the badge logic). Order mocks accordingly, or mock `useSession` directly via `vi.mock('../composables/useSession.js', ...)`.

If NavBar tests are too brittle due to useSession coupling, it's acceptable to test at a more limited scope (just verify the badge renders in isolation) and mark the full NavBar integration test as `it.skip` with a comment. The CRITICAL assertion is that `StatusBadge.test.js` passes (tests the component that handles ok/warning/over statuses).
  </action>
  <verify>
    <automated>cd /Users/pwagstro/Documents/workspace/simple_llm_proxy/frontend && npm test -- --reporter=verbose 2>&1 | grep -E "PASS|FAIL|NavBadge|NavBar|StatusBadge" | head -30</automated>
  </verify>
  <done>StatusBadge tests all pass. NavBadge tests pass (or are explicitly skipped with comment if useSession coupling makes them too brittle). /cost route exists in router/index.js. NavBar adminLinks contains Cost entry. fetchAlertCount is called on mount AND on route navigation.</done>
</task>

</tasks>

<verification>
```bash
cd /Users/pwagstro/Documents/workspace/simple_llm_proxy/frontend
npm test -- --reporter=verbose 2>&1 | grep -E "PASS|FAIL|todo"
```
All StatusBadge tests (including 6 new ones) pass. NavBadge tests pass or are explicitly skipped. No existing tests broken.
</verification>

<success_criteria>
- api.spend(params) method exists in client.js and builds correct query strings
- api.spend() uses positive-integer guard (> 0) not just truthy check for ID params
- StatusBadge renders OK (green), Warning (amber), Over Budget (red) for new statuses — 6 new tests pass
- NavBar adminLinks contains { to: '/cost', label: 'Cost' } after Keys
- NavBar fetches alertCount on mount and renders badge when count > 0
- NavBar re-fetches alertCount on route navigation (afterEach or watch on route.path)
- Badge shows '9+' when alertCount >= 10, hidden when 0
- /cost route registered in router/index.js pointing to CostView (dynamic import)
- cd frontend && npm test exits 0
</success_criteria>

<output>
After completion, create `.planning/phases/03-cost-monitoring-complete-console/03-PLAN-3-SUMMARY.md`
</output>

## PLANNING COMPLETE
