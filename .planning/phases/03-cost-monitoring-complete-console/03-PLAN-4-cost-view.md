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
    - "Team → Application → Key cascading dropdowns narrow the chart and table data"
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
      provides: "Full cost dashboard: alerts panel + filter bar + ApexCharts bar + breakdown table"
      min_lines: 200
    - path: "frontend/src/views/KeysView.vue"
      provides: "Spend column updated from placeholder to real $X.XX / $Y.YY display"
      contains: "total_spend"
    - path: "frontend/tests/unit/views/CostView.test.js"
      provides: "Real tests replacing Wave 0 todos"
  key_links:
    - from: "frontend/src/views/CostView.vue"
      to: "/admin/spend"
      via: "api.spend(filters) in onMounted and watch handlers"
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
// All params optional. Returns null on 401 (handled by request() interceptor).
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
</interfaces>

<tasks>

<task type="auto">
  <name>Task 1: Install ApexCharts and register as global plugin in main.js</name>
  <files>frontend/src/main.js</files>
  <action>
1. Install the npm packages (per RESEARCH.md RESEARCH.md Standard Stack):
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
  </action>
  <verify>
    <automated>cd /Users/pwagstro/Documents/workspace/simple_llm_proxy/frontend && node -e "require('./node_modules/vue3-apexcharts/dist/vue3-apexcharts.cjs.js')" 2>&1; echo "exit: $?"</automated>
  </verify>
  <done>vue3-apexcharts package exists in node_modules. main.js contains `use(VueApexCharts)`. npm install exits 0.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Implement CostView.vue and update KeysView spend column + real tests</name>
  <files>
    frontend/src/views/CostView.vue
    frontend/src/views/KeysView.vue
    frontend/tests/unit/views/CostView.test.js
  </files>
  <behavior>
    - LoadingSpinner shown while API call is in flight (loading=true)
    - ErrorAlert shown when api.spend() throws (error message matches UI-SPEC copy)
    - Alerts panel renders when alerts.length > 0; heading "Budget Alerts"; each row shows key name, app name, spend, soft budget, hard budget, and a StatusBadge
    - Alerts panel hidden entirely when alerts.length === 0
    - Filter bar: 4 date buttons (Today/7d/30d/Custom), default 7d highlighted; Custom reveals two date inputs
    - Team dropdown populated from unique team names in response rows; "All teams" is default
    - App dropdown filtered to apps in selected team; "All applications" default; disabled when team = "All"
    - Key dropdown filtered to keys in selected app; "All keys" default; disabled when app = "All"
    - Reset Filters button appears only when any filter is non-default; resets all to defaults when clicked
    - Chart renders with spend rows aggregated per D-09 grouping logic
    - Table shows Name, Total Spend, Budget (or "—" if nil), % Budget (or "—" if no budget), StatusBadge (ok/warning/over)
    - Empty state inside table when rows array is empty
    - Re-fetch triggered reactively on any filter change (watch on dateRange, customFrom, customTo, selectedTeamName, selectedAppName, selectedKeyName)
    - Custom date inputs debounced 300ms before triggering re-fetch
    - KeysView spend column: for selected app's keys, call api.spend({ appId: selectedApp.id }) on app selection, store spend map keyed by key_id, display "$X.XX / $Y.YY" (or "$X.XX / ∞" for unlimited) instead of placeholder
  </behavior>
  <action>
**Create `frontend/src/views/CostView.vue`:**

Follow the layout contract from UI-SPEC.md exactly. Key implementation notes:

**Script setup:**
```javascript
import { ref, computed, watch, onMounted } from 'vue'
import { api } from '../api/client.js'
import LoadingSpinner from '../components/LoadingSpinner.vue'
import ErrorAlert from '../components/ErrorAlert.vue'
import StatusBadge from '../components/StatusBadge.vue'

// State
const loading = ref(false)
const error = ref('')
const spendData = ref(null)  // { rows: [], alerts: [], from: '', to: '' }

// Filter state
const dateRange = ref('7d')  // 'today' | '7d' | '30d' | 'custom'
const customFrom = ref('')
const customTo = ref('')
const selectedTeamName = ref('')   // '' = "All teams"
const selectedAppName = ref('')    // '' = "All applications"
const selectedKeyName = ref('')    // '' = "All keys"

// Computed: unique teams/apps/keys from response rows for dropdown population
const teams = computed(() => {
  if (!spendData.value) return []
  return [...new Set(spendData.value.rows.map(r => r.team_name))].sort()
})
const appsForTeam = computed(() => {
  if (!spendData.value) return []
  const rows = selectedTeamName.value
    ? spendData.value.rows.filter(r => r.team_name === selectedTeamName.value)
    : spendData.value.rows
  return [...new Set(rows.map(r => r.app_name))].sort()
})
const keysForApp = computed(() => {
  if (!spendData.value) return []
  const rows = selectedAppName.value
    ? spendData.value.rows.filter(r => r.app_name === selectedAppName.value)
    : spendData.value.rows
  return [...new Set(rows.map(r => r.key_name))].sort()
})

// Computed: visible rows after local filtering (client-side narrowing within fetched data)
// Note: server-side filters are used for the API call; client-side computed narrows display
const filteredRows = computed(() => {
  if (!spendData.value) return []
  return spendData.value.rows.filter(r => {
    if (selectedTeamName.value && r.team_name !== selectedTeamName.value) return false
    if (selectedAppName.value && r.app_name !== selectedAppName.value) return false
    if (selectedKeyName.value && r.key_name !== selectedKeyName.value) return false
    return true
  })
})

// D-09: Chart grouping logic
const chartLabels = computed(() => {
  if (selectedKeyName.value) return filteredRows.value.map(r => r.key_name)
  if (selectedAppName.value) return filteredRows.value.map(r => r.key_name)
  if (selectedTeamName.value) return filteredRows.value.map(r => r.app_name)
  // No filters: aggregate by team
  const teamSpend = {}
  filteredRows.value.forEach(r => {
    teamSpend[r.team_name] = (teamSpend[r.team_name] || 0) + r.total_spend
  })
  return Object.keys(teamSpend)
})
const chartValues = computed(() => {
  if (selectedKeyName.value || selectedAppName.value || selectedTeamName.value) {
    return filteredRows.value.map(r => parseFloat(r.total_spend.toFixed(4)))
  }
  const teamSpend = {}
  filteredRows.value.forEach(r => {
    teamSpend[r.team_name] = (teamSpend[r.team_name] || 0) + r.total_spend
  })
  return Object.values(teamSpend).map(v => parseFloat(v.toFixed(4)))
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

// Computed: date range as from/to strings for api.spend()
const dateFromTo = computed(() => {
  const now = new Date()
  const fmt = (d) => d.toISOString().split('T')[0]
  if (dateRange.value === 'today') {
    const from = fmt(now)
    const to = fmt(new Date(now.getTime() + 86400000))
    return { from, to }
  }
  if (dateRange.value === '7d') {
    const from = fmt(new Date(now.getTime() - 7 * 86400000))
    return { from }
  }
  if (dateRange.value === '30d') {
    const from = fmt(new Date(now.getTime() - 30 * 86400000))
    return { from }
  }
  if (dateRange.value === 'custom' && customFrom.value && customTo.value) {
    return { from: customFrom.value, to: customTo.value }
  }
  return {}
})

// Computed: is any filter non-default?
const hasNonDefaultFilter = computed(() => {
  return dateRange.value !== '7d' || selectedTeamName.value || selectedAppName.value || selectedKeyName.value
})

// Reset all filters
function resetFilters() {
  dateRange.value = '7d'
  customFrom.value = ''
  customTo.value = ''
  selectedTeamName.value = ''
  selectedAppName.value = ''
  selectedKeyName.value = ''
}

// Cascade: resetting team clears app and key; resetting app clears key
watch(selectedTeamName, () => { selectedAppName.value = ''; selectedKeyName.value = '' })
watch(selectedAppName, () => { selectedKeyName.value = '' })

// Fetch function
async function loadSpend() {
  loading.value = true
  error.value = ''
  try {
    const params = { ...dateFromTo.value }
    const data = await api.spend(params)
    spendData.value = data
  } catch (e) {
    error.value = e?.message || 'Failed to load cost data. Check that the proxy is reachable and your session is valid.'
  } finally {
    loading.value = false
  }
}

// Row status helper
function rowStatus(row) {
  if (row.max_budget != null && row.total_spend >= row.max_budget) return 'over'
  if (row.soft_budget != null && row.total_spend >= row.soft_budget) return 'warning'
  return 'ok'
}

// Format spend/budget for table
function formatSpend(v) {
  return `$${v.toFixed(4)}`
}
function formatBudget(row) {
  if (row.max_budget == null) return '—'
  return `$${row.max_budget.toFixed(2)}`
}
function formatPctBudget(row) {
  if (row.max_budget == null || row.max_budget === 0) return '—'
  return `${((row.total_spend / row.max_budget) * 100).toFixed(1)}%`
}

// Debounce for custom date inputs (300ms)
let customDateDebounceTimer = null
function onCustomDateChange() {
  clearTimeout(customDateDebounceTimer)
  customDateDebounceTimer = setTimeout(loadSpend, 300)
}

// Reactive re-fetch on filter changes (immediate for dropdowns, debounced for date range type changes)
watch([dateRange, selectedTeamName, selectedAppName, selectedKeyName], loadSpend)
// Custom date watcher is separate (uses debounce)
watch([customFrom, customTo], onCustomDateChange)

onMounted(loadSpend)
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
          <!-- Team dropdown -->
          <select v-model="selectedTeamName" class="input">
            <option value="">All teams</option>
            <option v-for="team in teams" :key="team" :value="team">{{ team }}</option>
          </select>
          <!-- App dropdown — disabled when no team selected -->
          <select
            v-model="selectedAppName"
            :disabled="!selectedTeamName"
            :class="!selectedTeamName ? 'opacity-50 cursor-not-allowed' : ''"
            class="input"
          >
            <option value="">All applications</option>
            <option v-for="app in appsForTeam" :key="app" :value="app">{{ app }}</option>
          </select>
          <!-- Key dropdown — disabled when no app selected -->
          <select
            v-model="selectedKeyName"
            :disabled="!selectedAppName"
            :class="!selectedAppName ? 'opacity-50 cursor-not-allowed' : ''"
            class="input"
          >
            <option value="">All keys</option>
            <option v-for="key in keysForApp" :key="key" :value="key">{{ key }}</option>
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

      <!-- 4. Breakdown Table -->
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
            <tr
              v-if="filteredRows.length === 0"
            >
              <td colspan="5" class="px-6 py-12 text-center text-sm text-gray-500">
                <div class="font-medium text-gray-700 mb-1">No spend data</div>
                <div v-if="hasNonDefaultFilter">No spend data matches the selected filters. Try clearing the team or application filter.</div>
                <div v-else>No requests were recorded in this date range. Try expanding the date range or checking that requests are being proxied.</div>
              </td>
            </tr>
            <tr
              v-for="row in filteredRows"
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

Replace all `it.todo` stubs with real tests. Mock `api` and test key behaviors:
- LoadingSpinner shown when loading=true (set via delayed mock)
- ErrorAlert shown when api.spend() throws
- Alerts panel renders when spendData.alerts.length > 0
- Alerts panel NOT rendered when spendData.alerts is empty
- Table shows rows from filteredRows
- Empty state renders when rows is empty
- Filter bar defaults to 7d highlighted
- Reset Filters button appears when filter is non-default

Use `vi.mock('../../../src/api/client.js', ...)` to stub api.spend(). Use `stubs: { apexchart: true }` in mount options to prevent ApexCharts DOM errors.

```javascript
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createWebHashHistory } from 'vue-router'
import CostView from '@/views/CostView.vue'

vi.mock('@/api/client.js', () => ({
  api: {
    spend: vi.fn(),
    teams: vi.fn().mockResolvedValue([]),
    applications: vi.fn().mockResolvedValue([]),
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
    api.spend.mockRejectedValueOnce(new Error('Network error'))
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()
    expect(wrapper.findComponent({ name: 'ErrorAlert' }).exists()).toBe(true)
  })

  it('hides Alerts Panel when alerts array is empty', async () => {
    api.spend.mockResolvedValueOnce(emptySpendResponse)
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()
    expect(wrapper.text()).not.toContain('Budget Alerts')
  })

  it('renders Alerts Panel when alerts array is non-empty', async () => {
    api.spend.mockResolvedValueOnce(spendWithAlerts)
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('Budget Alerts')
  })

  it('renders empty state when spend rows array is empty', async () => {
    api.spend.mockResolvedValueOnce(emptySpendResponse)
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('No spend data')
  })

  it('renders breakdown table rows from spend data', async () => {
    api.spend.mockResolvedValueOnce(spendWithAlerts)
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('test-key')
  })

  it('filter bar defaults to 7d as the active date range', async () => {
    api.spend.mockResolvedValueOnce(emptySpendResponse)
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()
    // The 7d button should have the active class bg-indigo-50
    const buttons = wrapper.findAll('button')
    const btn7d = buttons.find(b => b.text() === '7d')
    expect(btn7d).toBeTruthy()
    expect(btn7d.classes()).toContain('bg-indigo-50')
  })

  it('Reset Filters button not shown when filters are at defaults', async () => {
    api.spend.mockResolvedValueOnce(emptySpendResponse)
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()
    expect(wrapper.text()).not.toContain('Reset Filters')
  })
})
```
  </action>
  <verify>
    <automated>cd /Users/pwagstro/Documents/workspace/simple_llm_proxy/frontend && npm test -- --reporter=verbose 2>&1 | grep -E "CostView|PASS|FAIL" | head -30</automated>
  </verify>
  <done>All CostView.test.js tests PASS (not todo). KeysView.vue spend column updated. CostView.vue exists at frontend/src/views/CostView.vue. npm test exits 0.</done>
</task>

<task type="checkpoint:human-verify" gate="blocking">
  <what-built>
Complete Phase 3 frontend implementation:
- CostView.vue at /cost with bar chart, filter bar, breakdown table, alerts panel
- Keys view spend column shows real $X.XX / $Y.YY values
- NavBar has "Cost" link with red badge when keys are over budget
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
7. **Keys view spend column:**
   - Navigate to /keys and select a team + application
   - Verify spend column shows "$X.XX / $Y.YY" format (not "Budget: $Y.YY")
   - For unlimited budget keys: verify "$X.XX / ∞"
8. **Filter behavior:**
   - In Cost view: select a team from the dropdown — table and chart should update
   - Verify App dropdown becomes enabled when a team is selected
   - Click Reset Filters — all dropdowns should reset to "All"
  </how-to-verify>
  <resume-signal>Type "approved" if the Cost view is functional, or describe any issues found</resume-signal>
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
- CostView.vue implements all 8 behavior items (alerts, filters, chart, table, empty state)
- KeysView.vue spend column shows "$X.XX / $Y.YY" (or "$X.XX / ∞")
- All CostView.test.js tests pass (not todo)
- go test ./... exits 0 — full backend test suite green
- cd frontend && npm test exits 0 — full frontend test suite green
- Human verification checkpoint passes (cost view is functional in browser)
</success_criteria>

<output>
After completion, create `.planning/phases/03-cost-monitoring-complete-console/03-PLAN-4-SUMMARY.md`
</output>

## PLANNING COMPLETE

## ALL PLANS COMPLETE
