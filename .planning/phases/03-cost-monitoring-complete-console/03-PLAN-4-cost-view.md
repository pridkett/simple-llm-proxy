---
phase: 03-cost-monitoring-complete-console
plan: 4
type: execute
wave: 4
depends_on:
  - "03-PLAN-3"
  - "03-PLAN-2"
files_modified:
  - frontend/src/main.js
  - frontend/src/views/CostView.vue
  - frontend/src/views/KeysView.vue
  - frontend/tests/unit/views/CostView.test.js
autonomous: false
requirements:
  - COST-02
  - COST-04
  - COST-05
  - UI-05
  - UI-06

user_setup:
  - service: apexcharts
    why: "ApexCharts npm package must be installed before main.js can import it"
    env_vars: []
    dashboard_config:
      - task: "Run npm install in frontend directory"
        location: "Terminal: cd frontend && npm install apexcharts vue3-apexcharts"

must_haves:
  truths:
    - "Admin can navigate to /cost and see a Cost page with a bar chart and breakdown table"
    - "Date range filter bar shows Today/7d/30d/Custom buttons; default is 7d (highlighted)"
    - "Team → Application → Key cascading dropdowns each trigger a NEW server-side refetch with resolved IDs"
    - "Alerts panel appears at top when any key is at or above its soft budget threshold (amber for soft, red for hard)"
    - "Alerts panel is hidden entirely when no alerts exist"
    - "Breakdown table shows Name, Total Spend, Budget, % Budget, Status badge columns"
    - "Empty state shown when no spend data matches filters"
    - "Keys view spend column shows '$X.XX / $Y.YY' (or '$X.XX / ∞' for unlimited) — not the Phase 2 placeholder"
    - "cd frontend && npm test passes with CostView tests green"
  artifacts:
    - path: "frontend/src/main.js"
      provides: "VueApexCharts registered as global plugin"
      contains: "VueApexCharts"
    - path: "frontend/src/views/CostView.vue"
      provides: "Full cost dashboard: alerts panel + filter bar + ApexCharts bar + breakdown table; all filter changes trigger server refetch"
      min_lines: 200
    - path: "frontend/src/views/KeysView.vue"
      provides: "Spend column updated from placeholder to real $X.XX / $Y.YY display"
      contains: "total_spend"
    - path: "frontend/tests/unit/views/CostView.test.js"
      provides: "Real tests replacing Wave 0 todos; includes refetch-on-filter-change test"
  key_links:
    - from: "frontend/src/views/CostView.vue"
      to: "/admin/spend"
      via: "api.spend(filters) in onMounted and watch handlers — every filter change triggers new API call"
      pattern: "api.spend"
    - from: "frontend/src/views/CostView.vue"
      to: "apexchart component"
      via: "global registration via VueApexCharts plugin in main.js"
      pattern: "<apexchart"
    - from: "frontend/src/views/KeysView.vue"
      to: "/admin/spend"
      via: "api.spend({ appId: selectedApp.id }) at app selection time"
      pattern: "api.spend"
---

<objective>
Build the CostView.vue page (the primary deliverable of Phase 3), register ApexCharts as a global plugin, and wire up the Keys view spend column. This plan completes the entire Phase 3 user-visible feature set.

Purpose: The admin console is incomplete without a cost view. This plan closes all remaining Phase 3 requirements (COST-02, COST-04, COST-05, UI-05, UI-06).
Output: A fully functional Cost dashboard at `/cost` with filters, chart, table, and alert surfacing. Keys view displays real spend vs budget.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/03-cost-monitoring-complete-console/03-CONTEXT.md
@.planning/phases/03-cost-monitoring-complete-console/03-UI-SPEC.md
@.planning/phases/03-cost-monitoring-complete-console/03-RESEARCH.md
@.planning/phases/03-cost-monitoring-complete-console/03-PLAN-3-SUMMARY.md
@frontend/src/main.js
@frontend/src/views/KeysView.vue
@frontend/src/api/client.js
@frontend/tests/unit/views/CostView.test.js
@frontend/tests/unit/views/DashboardView.test.js
</context>

<interfaces>
<!-- Plan 3 produced these (read from client.js before coding): -->
From frontend/src/api/client.js (after Plan 3):
```javascript
// api.spend({ from, to, teamId, appId, keyId }) — returns { rows, alerts, from, to }
// All params optional. Only positive integer IDs are sent (zero/NaN/negative omitted).
// Returns null on 401 (handled by request() interceptor).
```

From UI-SPEC.md (authoritative layout contract):
```
CostView layout (top to bottom):
  <h1> Cost </h1>
  <LoadingSpinner v-if="loading" />
  <ErrorAlert v-else-if="error" :message="error" />
  <template v-else>
    <AlertsPanel :alerts="alerts" class="mb-6" />   <!-- hidden when alerts.length === 0 -->
    <div class="card px-4 py-3 mb-6">               <!-- Filter bar -->
      Date range: [Today] [7d] [30d] [Custom]
      Team dropdown, App dropdown (cascades), Key dropdown (cascades)
      [Reset Filters] button (appears only when non-default filter active)
    </div>
    <div class="card p-6 mb-6">                     <!-- Chart card -->
      <h2>Spend Overview</h2>
      <apexchart type="bar" height="280" :options="chartOptions" :series="chartSeries" />
    </div>
    <div class="card">                              <!-- Table card -->
      <div class="px-6 py-4 border-b">Spend Breakdown</div>
      <table> Name | Total Spend | Budget | % Budget | Status </table>
    </div>
  </template>
```

From UI-SPEC.md (chart configuration):
```javascript
const chartOptions = computed(() => ({
  chart: { type: 'bar', toolbar: { show: false } },
  colors: ['#4F46E5'],  // indigo-600 (not indigo-500)
  plotOptions: { bar: { borderRadius: 4, horizontal: false } },
  xaxis: { categories: chartLabels.value, labels: { style: { colors: '#6B7280' } } },
  yaxis: { labels: { formatter: (v) => `$${v.toFixed(2)}`, style: { colors: '#6B7280' } } },
  grid: { borderColor: '#E5E7EB' },
  tooltip: { y: { formatter: (v) => `$${v.toFixed(4)}` } },
  dataLabels: { enabled: false },
  noData: { text: 'No spend data for this period.' },
}))
```

From UI-SPEC.md (filter interaction states):
```
Date range active: bg-indigo-50 text-indigo-700 font-semibold rounded-md px-3 py-1.5 text-sm
Date range inactive: btn-secondary text-sm px-3 py-1.5
Custom date: input class="input max-w-[140px]" type="date"
Team/App/Key: select class="input"
Disabled dropdown: class="input opacity-50 cursor-not-allowed"
```

From UI-SPEC.md (table + alerts):
```
Table columns: Name | Total Spend | Budget | % Budget | Status
Table row: hover:bg-gray-50 transition-colors
Alerts row (soft): bg-amber-50 border border-amber-200 rounded-md p-3 text-sm
Alerts row (hard): bg-red-50 border border-red-200 rounded-md p-3 text-sm
Alerts summary: "{N} key(s) require attention." text-sm font-semibold text-gray-700
```

From UI-SPEC.md (copywriting — EXACT strings required):
```
Page heading: "Cost"
Chart section heading: "Spend Overview"
Table section heading: "Spend Breakdown"
Alerts panel heading: "Budget Alerts"
Alerts summary: "{N} key(s) require attention."
Empty state heading: "No spend data"
Empty state body (date range): "No requests were recorded in this date range. Try expanding the date range or checking that requests are being proxied."
Empty state body (filtered): "No spend data matches the selected filters. Try clearing the team or application filter."
Error state: "Failed to load cost data. Check that the proxy is reachable and your session is valid."
Date range buttons: "Today", "7d", "30d", "Custom"
Custom date labels: "From", "To"
Reset button: "Reset Filters"
Keys view budget set: "$X.XX / $Y.YY"
Keys view unlimited: "$X.XX / ∞"
```

D-09 chart grouping logic (from CONTEXT.md):
```
Key selected → bars: one per key matching filter (usually one bar)
Only application selected → bars: one per key in that application
Only team selected → bars: one per application in that team
No filters → bars: one per team (aggregate by team name from rows)
```

IMPORTANT — Filter model (per D-07, validated by review):
```
ALL filter changes (date range, team, application, key dropdowns) trigger a new
server-side API call. There is NO client-side filtering of already-fetched data.

Rationale: The server uses GROUP BY to aggregate rows. When the filter dimension
changes (e.g., from "no filter" to "specific team"), the correct grouping and
aggregation can only come from the server — client-side narrowing of a broader
dataset would produce incorrect aggregates.

Implementation:
  - The watch on [dateRange, selectedTeamId, selectedAppId, selectedKeyId] calls loadSpend()
  - Each dropdown stores the SELECTED ID (not just name) to pass to api.spend()
  - The response rows are used DIRECTLY as the table rows (no client-side filter computed)
  - The dropdowns (team/app/key) are populated from a SEPARATE teams/apps/keys API call
    OR from the response rows of an unfiltered fetch — see note below

NOTE on dropdown population: Since the response rows are now scoped to the active filter,
a filtered response cannot be used to populate the "Team" dropdown (e.g., if team_id=1
is selected, only team 1's rows come back — you can't build a team dropdown from that).
Solution: Load dropdown options from the RESPONSE OF AN UNFILTERED INITIAL CALL, or from
the existing /admin/teams and /admin/applications endpoints.

Simplest approach: On mount, call api.spend() with no filters to populate dropdown options.
Then, when filters are applied, call api.spend() again with the selected IDs — the response
rows update the table and chart, but the dropdown options remain from the initial unfiltered call.

Cascade behavior:
  - When selectedTeamId changes, reset selectedAppId and selectedKeyId to null, then re-fetch
  - When selectedAppId changes, reset selectedKeyId to null, then re-fetch
  - App dropdown options: filter initial-response teams/apps by selectedTeamId
  - Key dropdown options: filter initial-response apps/keys by selectedAppId
  - This gives cascade without requiring a separate API call per dropdown change
```
</interfaces>

<tasks>

<task type="auto">
  <name>Task 1: Install ApexCharts and register as global plugin in main.js</name>
  <files>frontend/src/main.js</files>
  <action>
1. Install the npm packages (per RESEARCH.md Standard Stack):
```bash
cd /Users/pwagstro/Documents/workspace/simple_llm_proxy/frontend && npm install apexcharts vue3-apexcharts
```

2. Update `frontend/src/main.js` to register VueApexCharts as a global plugin:

```javascript
import { createApp } from 'vue'
import App from './App.vue'
import router from './router/index.js'
import VueApexCharts from 'vue3-apexcharts'
import './style.css'

createApp(App).use(router).use(VueApexCharts).mount('#app')
```

The `app.use(VueApexCharts)` call registers the `<apexchart>` component globally — it will be available in CostView.vue without a local import.

IMPORTANT: After editing main.js, the Vite dev server requires a full restart (not HMR) for the plugin registration to take effect. This is a known ApexCharts limitation noted in RESEARCH.md anti-patterns.

NOTE on testing: ApexCharts relies on browser DOM APIs that are not available in jsdom (Vitest's test environment). All test files that mount CostView must stub the `<apexchart>` component to prevent test failures. Use `stubs: { apexchart: true }` in mount options — this is already specified in the CostView test task below.
  </action>
  <verify>
    <automated>cd /Users/pwagstro/Documents/workspace/simple_llm_proxy/frontend && node -e "require('./node_modules/vue3-apexcharts/dist/vue3-apexcharts.cjs.js')" 2>&1; echo "exit: $?"</automated>
  </verify>
  <done>vue3-apexcharts package exists in node_modules. main.js contains `use(VueApexCharts)`. npm install exits 0.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Implement CostView.vue (server-driven filters) + update KeysView spend column + real tests</name>
  <files>
    frontend/src/views/CostView.vue
    frontend/src/views/KeysView.vue
    frontend/tests/unit/views/CostView.test.js
  </files>
  <behavior>
    - LoadingSpinner shown while API call is in flight (loading=true)
    - ErrorAlert shown when api.spend() throws (error message matches UI-SPEC copy)
    - Alerts panel renders when spendData.alerts.length > 0; heading "Budget Alerts"; each row shows key name, app name, spend, soft budget, hard budget, and a StatusBadge
    - Alerts panel hidden entirely when alerts.length === 0
    - Filter bar: 4 date buttons (Today/7d/30d/Custom), default 7d highlighted; Custom reveals two date inputs
    - Team dropdown options populated from the initial unfiltered API response (all team names)
    - App dropdown options filtered to apps from the selected team's rows in the initial response; disabled when no team selected
    - Key dropdown options filtered to keys from the selected app's rows in the initial response; disabled when no app selected
    - When team dropdown changes: reset app and key selections, re-fetch with new teamId
    - When app dropdown changes: reset key selection, re-fetch with new appId (and current teamId)
    - When key dropdown changes: re-fetch with keyId (and current teamId and appId)
    - Each fetch passes resolved IDs (not names) to api.spend({teamId, appId, keyId})
    - Table rows come DIRECTLY from spendData.rows — NO client-side filtering of response rows
    - Chart series and labels derived from spendData.rows (the filtered server response)
    - D-09 grouping: if keyId filter active → show key name bars; if appId active → show key name bars; if teamId active → show app name bars; no filter → aggregate by team
    - Reset Filters button appears only when any filter is non-default; resets all to defaults and re-fetches
    - Custom date inputs debounced 300ms before triggering re-fetch
    - KeysView spend column: call api.spend({ appId: selectedApp.id }) on app selection, display "$X.XX / $Y.YY" (or "$X.XX / ∞" for unlimited)
    - Re-fetch triggers a new API call — confirmed by test that checks api.spend call count increases
  </behavior>
  <action>
**Create `frontend/src/views/CostView.vue`:**

Follow the layout contract from UI-SPEC.md exactly. Key implementation notes:

CRITICAL DESIGN DECISION (per D-07 and review): All filter changes (date range AND team/app/key dropdowns) trigger a new server-side API call. The table rows come directly from the API response. The dropdowns are populated from an initial unfiltered call that is separate from filtered calls.

**Script setup:**
```javascript
import { ref, computed, watch, onMounted } from 'vue'
import { api } from '../api/client.js'
import LoadingSpinner from '../components/LoadingSpinner.vue'
import ErrorAlert from '../components/ErrorAlert.vue'
import StatusBadge from '../components/StatusBadge.vue'

// State — current filtered response
const loading = ref(false)
const error = ref('')
const spendData = ref(null)  // { rows: [], alerts: [], from: '', to: '' }

// State — initial unfiltered response for populating dropdown options
// This is fetched once on mount with no filters and not updated by filter changes.
// It provides the full universe of teams/apps/keys for the cascade dropdowns.
const allRowsData = ref(null)  // cached unfiltered response rows

// Filter state — store IDs (not names) so we can pass them directly to api.spend()
const dateRange = ref('7d')   // 'today' | '7d' | '30d' | 'custom'
const customFrom = ref('')
const customTo = ref('')
const selectedTeamId = ref(null)    // null = "All teams"
const selectedAppId = ref(null)     // null = "All applications"
const selectedKeyId = ref(null)     // null = "All keys"

// Computed: date range as from/to strings for api.spend()
const dateFromTo = computed(() => {
  const now = new Date()
  const fmt = (d) => d.toISOString().split('T')[0]
  if (dateRange.value === 'today') {
    return { from: fmt(now), to: fmt(now) }
  }
  if (dateRange.value === '7d') {
    return { from: fmt(new Date(now.getTime() - 7 * 86400000)) }
  }
  if (dateRange.value === '30d') {
    return { from: fmt(new Date(now.getTime() - 30 * 86400000)) }
  }
  if (dateRange.value === 'custom' && customFrom.value && customTo.value) {
    return { from: customFrom.value, to: customTo.value }
  }
  return {}
})

// Computed: dropdown options derived from the initial unfiltered response
// Team dropdown: all unique teams from allRowsData
const teams = computed(() => {
  if (!allRowsData.value) return []
  const seen = new Map()
  for (const r of allRowsData.value.rows) {
    if (!seen.has(r.team_id)) seen.set(r.team_id, r.team_name)
  }
  return [...seen.entries()].map(([id, name]) => ({ id, name })).sort((a, b) => a.name.localeCompare(b.name))
})

// App dropdown: apps belonging to the selected team (from allRowsData)
const appsForTeam = computed(() => {
  if (!allRowsData.value) return []
  const rows = selectedTeamId.value
    ? allRowsData.value.rows.filter(r => r.team_id === selectedTeamId.value)
    : allRowsData.value.rows
  const seen = new Map()
  for (const r of rows) {
    if (!seen.has(r.app_id)) seen.set(r.app_id, r.app_name)
  }
  return [...seen.entries()].map(([id, name]) => ({ id, name })).sort((a, b) => a.name.localeCompare(b.name))
})

// Key dropdown: keys belonging to the selected app (from allRowsData)
const keysForApp = computed(() => {
  if (!allRowsData.value) return []
  const rows = selectedAppId.value
    ? allRowsData.value.rows.filter(r => r.app_id === selectedAppId.value)
    : allRowsData.value.rows
  const seen = new Map()
  for (const r of rows) {
    if (!seen.has(r.key_id)) seen.set(r.key_id, r.key_name)
  }
  return [...seen.entries()].map(([id, name]) => ({ id, name })).sort((a, b) => a.name.localeCompare(b.name))
})

// Computed: is any filter non-default?
const hasNonDefaultFilter = computed(() => {
  return dateRange.value !== '7d' || selectedTeamId.value != null || selectedAppId.value != null || selectedKeyId.value != null
})

// Reset all filters and re-fetch
function resetFilters() {
  dateRange.value = '7d'
  customFrom.value = ''
  customTo.value = ''
  selectedTeamId.value = null
  selectedAppId.value = null
  selectedKeyId.value = null
  // Watch will trigger loadSpend automatically
}

// D-09: Chart grouping logic — operates on spendData.rows (the current server response)
const chartLabels = computed(() => {
  if (!spendData.value) return []
  const rows = spendData.value.rows
  if (selectedKeyId.value || selectedAppId.value) return rows.map(r => r.key_name)
  if (selectedTeamId.value) return rows.map(r => r.app_name)
  // No filters: aggregate by team
  const teamSpend = new Map()
  for (const r of rows) {
    teamSpend.set(r.team_name, (teamSpend.get(r.team_name) || 0) + r.total_spend)
  }
  return [...teamSpend.keys()]
})
const chartValues = computed(() => {
  if (!spendData.value) return []
  const rows = spendData.value.rows
  if (selectedKeyId.value || selectedAppId.value) {
    return rows.map(r => parseFloat(r.total_spend.toFixed(4)))
  }
  if (selectedTeamId.value) {
    return rows.map(r => parseFloat(r.total_spend.toFixed(4)))
  }
  // No filters: aggregate by team
  const teamSpend = new Map()
  for (const r of rows) {
    teamSpend.set(r.team_name, (teamSpend.get(r.team_name) || 0) + r.total_spend)
  }
  return [...teamSpend.values()].map(v => parseFloat(v.toFixed(4)))
})
const chartSeries = computed(() => [{ name: 'Spend', data: chartValues.value }])
const chartOptions = computed(() => ({
  chart: { type: 'bar', toolbar: { show: false } },
  colors: ['#4F46E5'],
  plotOptions: { bar: { borderRadius: 4, horizontal: false } },
  xaxis: { categories: chartLabels.value, labels: { style: { colors: '#6B7280' } } },
  yaxis: { labels: { formatter: (v) => `$${v.toFixed(2)}`, style: { colors: '#6B7280' } } },
  grid: { borderColor: '#E5E7EB' },
  tooltip: { y: { formatter: (v) => `$${v.toFixed(4)}` } },
  dataLabels: { enabled: false },
  noData: { text: 'No spend data for this period.' },
}))

// Fetch function — called on mount and on any filter change
// Passes resolved IDs directly to api.spend() for server-side aggregation
async function loadSpend() {
  loading.value = true
  error.value = ''
  try {
    const params = {
      ...dateFromTo.value,
      teamId: selectedTeamId.value,   // null is omitted by api.spend() positive-integer check
      appId: selectedAppId.value,
      keyId: selectedKeyId.value,
    }
    const data = await api.spend(params)
    spendData.value = data
  } catch (e) {
    error.value = e?.message || 'Failed to load cost data. Check that the proxy is reachable and your session is valid.'
  } finally {
    loading.value = false
  }
}

// Initial unfiltered fetch — used to populate dropdown options
// This is separate from loadSpend() so filter changes don't affect dropdown options
async function loadAllRows() {
  try {
    const data = await api.spend({ ...dateFromTo.value })
    allRowsData.value = data
  } catch {
    // Non-critical — dropdowns will be empty but the view still works
  }
}

// Cascade: resetting team clears app and key; resetting app clears key
watch(selectedTeamId, () => {
  selectedAppId.value = null
  selectedKeyId.value = null
})
watch(selectedAppId, () => {
  selectedKeyId.value = null
})

// Re-fetch on filter change — always server-side, per D-07
watch([dateRange, selectedTeamId, selectedAppId, selectedKeyId], loadSpend)

// Custom date debounce
let customDateDebounceTimer = null
function onCustomDateChange() {
  clearTimeout(customDateDebounceTimer)
  customDateDebounceTimer = setTimeout(loadSpend, 300)
}
watch([customFrom, customTo], onCustomDateChange)

// Row helper functions
function rowStatus(row) {
  if (row.max_budget != null && row.total_spend >= row.max_budget) return 'over'
  if (row.soft_budget != null && row.total_spend >= row.soft_budget) return 'warning'
  return 'ok'
}
function formatSpend(v) { return `$${v.toFixed(4)}` }
function formatBudget(row) {
  if (row.max_budget == null) return '—'
  return `$${row.max_budget.toFixed(2)}`
}
function formatPctBudget(row) {
  if (row.max_budget == null || row.max_budget === 0) return '—'
  return `${((row.total_spend / row.max_budget) * 100).toFixed(1)}%`
}

onMounted(async () => {
  await loadAllRows()   // populate dropdowns from unfiltered response
  await loadSpend()     // initial filtered load (default 7d, no filters)
})
```

**Template structure** — follow the UI-SPEC.md layout contract exactly. Key structural elements:

```html
<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
    <h1 class="text-2xl font-semibold text-gray-900 mb-6">Cost</h1>

    <LoadingSpinner v-if="loading" />
    <ErrorAlert v-else-if="error" title="Failed to load cost data" :message="error" />

    <template v-else>
      <!-- 1. Alerts Panel -->
      <div v-if="spendData?.alerts?.length > 0" class="mb-6">
        <div class="flex items-center justify-between mb-3">
          <h2 class="text-base font-semibold text-gray-700">Budget Alerts</h2>
          <span class="text-sm text-gray-500">{{ spendData.alerts.length }} key(s) require attention.</span>
        </div>
        <div class="space-y-2">
          <div
            v-for="alert in spendData.alerts"
            :key="alert.key_id"
            :class="alert.alert_type === 'hard'
              ? 'bg-red-50 border border-red-200'
              : 'bg-amber-50 border border-amber-200'"
            class="rounded-md p-3 text-sm flex items-center justify-between"
          >
            <div :class="alert.alert_type === 'hard' ? 'text-red-800' : 'text-amber-800'">
              <span class="font-medium">{{ alert.key_name }}</span>
              <span class="ml-1 text-xs">({{ alert.app_name }})</span>
              <span class="ml-2">{{ formatSpend(alert.total_spend) }} spent</span>
              <span v-if="alert.soft_budget" class="ml-1">/ ${{ alert.soft_budget.toFixed(2) }} soft</span>
              <span v-if="alert.max_budget" class="ml-1">/ ${{ alert.max_budget.toFixed(2) }} hard</span>
            </div>
            <StatusBadge :status="alert.alert_type === 'hard' ? 'over' : 'warning'" />
          </div>
        </div>
      </div>

      <!-- 2. Filter Bar -->
      <div class="card px-4 py-3 mb-6">
        <div class="flex flex-wrap gap-3 items-center">
          <!-- Date range buttons -->
          <div class="flex gap-1">
            <button
              v-for="range in ['today', '7d', '30d', 'custom']"
              :key="range"
              @click="dateRange = range"
              :class="dateRange === range
                ? 'bg-indigo-50 text-indigo-700 font-semibold'
                : 'btn-secondary'"
              class="rounded-md px-3 py-1.5 text-sm"
            >
              {{ range === 'today' ? 'Today' : range === '7d' ? '7d' : range === '30d' ? '30d' : 'Custom' }}
            </button>
          </div>
          <!-- Custom date inputs -->
          <template v-if="dateRange === 'custom'">
            <label class="text-xs text-gray-500">From</label>
            <input type="date" v-model="customFrom" @change="onCustomDateChange" class="input max-w-[140px]" />
            <label class="text-xs text-gray-500">To</label>
            <input type="date" v-model="customTo" @change="onCustomDateChange" class="input max-w-[140px]" />
          </template>
          <!-- Team dropdown — always enabled; options from allRowsData -->
          <select v-model="selectedTeamId" class="input">
            <option :value="null">All teams</option>
            <option v-for="team in teams" :key="team.id" :value="team.id">{{ team.name }}</option>
          </select>
          <!-- App dropdown — disabled when no team selected -->
          <select
            v-model="selectedAppId"
            :disabled="!selectedTeamId"
            :class="!selectedTeamId ? 'opacity-50 cursor-not-allowed' : ''"
            class="input"
          >
            <option :value="null">All applications</option>
            <option v-for="app in appsForTeam" :key="app.id" :value="app.id">{{ app.name }}</option>
          </select>
          <!-- Key dropdown — disabled when no app selected -->
          <select
            v-model="selectedKeyId"
            :disabled="!selectedAppId"
            :class="!selectedAppId ? 'opacity-50 cursor-not-allowed' : ''"
            class="input"
          >
            <option :value="null">All keys</option>
            <option v-for="k in keysForApp" :key="k.id" :value="k.id">{{ k.name }}</option>
          </select>
          <!-- Reset Filters button — only when non-default state -->
          <button
            v-if="hasNonDefaultFilter"
            @click="resetFilters"
            class="btn-secondary text-xs px-3 py-1.5"
          >
            Reset Filters
          </button>
        </div>
      </div>

      <!-- 3. Bar Chart -->
      <div class="card p-6 mb-6">
        <h2 class="text-base font-semibold text-gray-900 mb-4">Spend Overview</h2>
        <apexchart
          type="bar"
          height="280"
          :options="chartOptions"
          :series="chartSeries"
        />
      </div>

      <!-- 4. Breakdown Table — rows come directly from spendData.rows (server response) -->
      <div class="card">
        <div class="px-6 py-4 border-b border-gray-100">
          <h2 class="text-base font-semibold text-gray-900">Spend Breakdown</h2>
        </div>
        <table class="w-full text-sm">
          <thead>
            <tr class="border-b border-gray-100">
              <th class="px-6 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wide">Name</th>
              <th class="px-6 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wide">Total Spend</th>
              <th class="px-6 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wide">Budget</th>
              <th class="px-6 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wide">% Budget</th>
              <th class="px-6 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wide">Status</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!spendData?.rows?.length">
              <td colspan="5" class="px-6 py-12 text-center text-sm text-gray-500">
                <div class="font-medium text-gray-700 mb-1">No spend data</div>
                <div v-if="hasNonDefaultFilter">No spend data matches the selected filters. Try clearing the team or application filter.</div>
                <div v-else>No requests were recorded in this date range. Try expanding the date range or checking that requests are being proxied.</div>
              </td>
            </tr>
            <tr
              v-for="row in spendData?.rows ?? []"
              :key="row.key_id"
              class="border-b border-gray-50 hover:bg-gray-50 transition-colors"
            >
              <td class="px-6 py-3 text-sm font-medium text-gray-900">
                {{ row.key_name }}
                <span class="ml-1 text-xs text-gray-400">{{ row.app_name }}</span>
              </td>
              <td class="px-6 py-3 text-sm text-gray-900">{{ formatSpend(row.total_spend) }}</td>
              <td class="px-6 py-3 text-sm text-gray-500">{{ formatBudget(row) }}</td>
              <td class="px-6 py-3 text-sm text-gray-500">{{ formatPctBudget(row) }}</td>
              <td class="px-6 py-3"><StatusBadge :status="rowStatus(row)" /></td>
            </tr>
          </tbody>
        </table>
      </div>

    </template>
  </div>
</template>
```

**Update `frontend/src/views/KeysView.vue` — spend column (per D-10):**

Find the spend column cell around line 104 (the Phase 2 placeholder comment is there):
```html
<!-- BEFORE (Phase 2 placeholder): -->
<td class="px-4 py-3 text-sm text-gray-700">
  <span v-if="key.max_budget != null">Budget: ${{ key.max_budget.toFixed(2) }}</span>
  <span v-else class="text-gray-400">Unlimited</span>
</td>

<!-- AFTER (Phase 3 real spend): -->
<td class="px-4 py-3 text-sm text-gray-700">
  <span v-if="key.max_budget != null">
    ${{ (keySpend[key.id] ?? 0).toFixed(4) }} / ${{ key.max_budget.toFixed(2) }}
  </span>
  <span v-else>
    ${{ (keySpend[key.id] ?? 0).toFixed(4) }} / ∞
  </span>
</td>
```

In the `<script setup>` of KeysView.vue, add:
1. `const keySpend = ref({})` — map of key_id → total_spend
2. A `loadSpend(appId)` function that calls `await api.spend({ appId })` and builds `keySpend.value` from the response rows
3. Call `loadSpend(selectedApp.value.id)` after `loadKeys()` in the `selectApp` function

```javascript
const keySpend = ref({})

async function loadSpend(appId) {
  try {
    const data = await api.spend({ appId })
    if (data && data.rows) {
      const map = {}
      data.rows.forEach(r => { map[r.key_id] = r.total_spend })
      keySpend.value = map
    }
  } catch {
    // Non-critical — spend column shows $0.00 gracefully on error
  }
}

// In selectApp function, after loading keys, also load spend:
// loadSpend(app.id)
```

**Update `frontend/tests/unit/views/CostView.test.js`:**

Replace all `it.todo` stubs with real tests. Mock `api` and test key behaviors including the server-driven refetch on filter change:

```javascript
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createWebHashHistory } from 'vue-router'
import CostView from '@/views/CostView.vue'

vi.mock('@/api/client.js', () => ({
  api: {
    spend: vi.fn(),
  },
}))

import { api } from '@/api/client.js'

function makeRouter() {
  return createRouter({
    history: createWebHashHistory(),
    routes: [{ path: '/cost', component: CostView }],
  })
}

const emptySpendResponse = { rows: [], alerts: [], from: '2026-03-19', to: '2026-03-26' }
const spendWithAlerts = {
  rows: [
    { key_id: 1, key_name: 'test-key', app_id: 1, app_name: 'test-app', team_id: 1, team_name: 'test-team',
      total_spend: 9.5, max_budget: 10.0, soft_budget: 8.0 }
  ],
  alerts: [
    { key_id: 1, key_name: 'test-key', app_name: 'test-app', team_name: 'test-team',
      total_spend: 9.5, soft_budget: 8.0, max_budget: 10.0, alert_type: 'soft' }
  ],
  from: '2026-03-19', to: '2026-03-26',
}

describe('CostView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders ErrorAlert on API failure', async () => {
    api.spend.mockRejectedValue(new Error('Network error'))
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()
    expect(wrapper.findComponent({ name: 'ErrorAlert' }).exists()).toBe(true)
  })

  it('hides Alerts Panel when alerts array is empty', async () => {
    api.spend.mockResolvedValue(emptySpendResponse)
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()
    expect(wrapper.text()).not.toContain('Budget Alerts')
  })

  it('renders Alerts Panel when alerts array is non-empty', async () => {
    api.spend.mockResolvedValue(spendWithAlerts)
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('Budget Alerts')
  })

  it('renders empty state when spend rows array is empty', async () => {
    api.spend.mockResolvedValue(emptySpendResponse)
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('No spend data')
  })

  it('renders breakdown table rows from spend data', async () => {
    api.spend.mockResolvedValue(spendWithAlerts)
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('test-key')
  })

  it('filter bar defaults to 7d as the active date range', async () => {
    api.spend.mockResolvedValue(emptySpendResponse)
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()
    const buttons = wrapper.findAll('button')
    const btn7d = buttons.find(b => b.text() === '7d')
    expect(btn7d).toBeTruthy()
    expect(btn7d.classes()).toContain('bg-indigo-50')
  })

  it('Reset Filters button not shown when filters are at defaults', async () => {
    api.spend.mockResolvedValue(emptySpendResponse)
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()
    expect(wrapper.text()).not.toContain('Reset Filters')
  })

  it('re-fetches from server when date range filter changes (no client-side filtering)', async () => {
    // Mount with initial 7d response
    api.spend.mockResolvedValue(emptySpendResponse)
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()

    const initialCallCount = api.spend.mock.calls.length
    expect(initialCallCount).toBeGreaterThan(0)

    // Click Today button — should trigger a new API call
    api.spend.mockResolvedValue(emptySpendResponse)
    const buttons = wrapper.findAll('button')
    const btnToday = buttons.find(b => b.text() === 'Today')
    await btnToday.trigger('click')
    await flushPromises()

    // api.spend should have been called again (server-driven refetch, not client-side filter)
    expect(api.spend.mock.calls.length).toBeGreaterThan(initialCallCount)
  })
})
```
  </action>
  <verify>
    <automated>cd /Users/pwagstro/Documents/workspace/simple_llm_proxy/frontend && npm test -- --reporter=verbose 2>&1 | grep -E "CostView|PASS|FAIL" | head -30</automated>
  </verify>
  <done>All CostView.test.js tests PASS (not todo). KeysView.vue spend column updated. CostView.vue exists at frontend/src/views/CostView.vue. CostView uses server-driven refetch for all filter changes. npm test exits 0.</done>
</task>

<task type="checkpoint:human-verify" gate="blocking">
  <what-built>
Complete Phase 3 frontend implementation:
- CostView.vue at /cost with bar chart, filter bar, breakdown table, alerts panel
- All filter changes (date range and team/app/key dropdowns) trigger server-side refetch with resolved IDs
- Keys view spend column shows real $X.XX / $Y.YY values
- NavBar has "Cost" link with red badge when keys are over budget (badge refreshes on navigation)
- All automated tests pass (go test ./... and cd frontend && npm test)
  </what-built>
  <how-to-verify>
1. Start the backend: `op run --env-file op.env --no-masking -- ./bin/proxy -config config.yaml`
2. Start the frontend dev server: `cd frontend && npm run dev`
3. Navigate to http://localhost:5173 and log in as admin
4. **Cost page:**
   - Click "Cost" in the nav bar — should navigate to /cost
   - Verify the page heading "Cost" renders
   - Verify the filter bar shows [Today] [7d] [30d] [Custom] buttons with 7d highlighted in indigo
   - Verify the "Spend Overview" bar chart section renders (may be empty if no spend data)
   - Verify the "Spend Breakdown" table renders with correct column headers
   - If any keys have spend data: verify rows appear with name, spend amount, budget, status badge
   - If no spend data: verify "No spend data" empty state message appears
5. **Alerts panel:**
   - If any key has spend >= soft_budget: verify "Budget Alerts" section appears above the filter bar
   - If no key has spend >= soft_budget: verify the panel is not shown at all
6. **Nav badge:**
   - If any key is over threshold: verify a red badge number appears on the "Cost" nav link
   - Navigate to another page and back — verify the badge reflects current state
7. **Keys view spend column:**
   - Navigate to /keys and select a team + application
   - Verify spend column shows "$X.XX / $Y.YY" format (not "Budget: $Y.YY")
   - For unlimited budget keys: verify "$X.XX / ∞"
8. **Filter behavior (server-driven):**
   - In Cost view: select a team from the dropdown
   - Open browser devtools Network tab — verify a new GET /admin/spend request fires with team_id=N
   - Select an application — verify another request fires with app_id=N
   - Click Reset Filters — verify another request fires with no team_id/app_id
   - Verify the table shows data appropriate to the selected filter (not stale from a prior broader fetch)
  </how-to-verify>
  <resume-signal>Type "approved" if the Cost view is functional and filter changes trigger server requests, or describe any issues found</resume-signal>
</task>

</tasks>

<verification>
```bash
# Full test suite
cd /Users/pwagstro/Documents/workspace/simple_llm_proxy
go test ./...
cd frontend && npm test

# Build check
go build ./...
```
All commands exit 0 before human verification checkpoint.
</verification>

<success_criteria>
- frontend/src/main.js contains `use(VueApexCharts)`
- frontend/src/views/CostView.vue exists and renders at /cost
- CostView.vue filter model is server-driven: all filter changes (date AND team/app/key dropdowns) trigger api.spend() call with resolved IDs — no client-side filtering of response rows
- Dropdown options (team/app/key) are populated from an initial unfiltered response, not from filtered response rows
- CostView.vue implements all behavior items (alerts, filters, chart, table, empty state)
- KeysView.vue spend column shows "$X.XX / $Y.YY" (or "$X.XX / ∞")
- CostView.test.js refetch test confirms api.spend is called again when date filter changes
- All CostView.test.js tests pass (not todo)
- go test ./... exits 0 — full backend test suite green
- cd frontend && npm test exits 0 — full frontend test suite green
- Human verification checkpoint passes (cost view is functional, filter changes trigger network requests)
</success_criteria>

<output>
After completion, create `.planning/phases/03-cost-monitoring-complete-console/03-PLAN-4-SUMMARY.md`
</output>

## PLANNING COMPLETE

## ALL PLANS COMPLETE
