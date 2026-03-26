<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
    <h1 class="text-2xl font-semibold text-gray-900 mb-6">Cost</h1>

    <LoadingSpinner v-if="loading" />
    <ErrorAlert v-else-if="error" title="Failed to load cost data" :message="error" />

    <template v-else>
      <!-- 1. Alerts Panel — hidden entirely when no alerts -->
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

      <!-- 3. Chart — toggleable: Total (bar) / Over Time (area) -->
      <div class="card p-6 mb-6">
        <div class="flex items-center justify-between mb-4">
          <h2 class="text-base font-semibold text-gray-900">Spend Overview</h2>
          <div class="flex gap-1">
            <button
              v-for="mode in ['total', 'over-time']"
              :key="mode"
              @click="chartMode = mode"
              :class="chartMode === mode
                ? 'bg-indigo-50 text-indigo-700 font-semibold'
                : 'btn-secondary'"
              class="rounded-md px-3 py-1.5 text-sm"
            >
              {{ mode === 'total' ? 'Total' : 'Over Time' }}
            </button>
          </div>
        </div>
        <apexchart
          v-if="chartMode === 'total'"
          type="bar"
          height="280"
          :options="chartOptions"
          :series="chartSeries"
        />
        <apexchart
          v-else
          type="area"
          height="280"
          :options="overTimeChartOptions"
          :series="overTimeChartSeries"
        />
      </div>

      <!-- 4. Breakdown Table — toggleable: By Key / By Model -->
      <div class="card">
        <div class="px-6 py-4 border-b border-gray-100 flex items-center justify-between">
          <h2 class="text-base font-semibold text-gray-900">Spend Breakdown</h2>
          <div class="flex gap-1">
            <button
              v-for="mode in ['by-key', 'by-model']"
              :key="mode"
              @click="breakdownMode = mode"
              :class="breakdownMode === mode
                ? 'bg-indigo-50 text-indigo-700 font-semibold'
                : 'btn-secondary'"
              class="rounded-md px-3 py-1.5 text-sm"
            >
              {{ mode === 'by-key' ? 'By Key' : 'By Model' }}
            </button>
          </div>
        </div>

        <!-- By Key table (default) -->
        <table v-if="breakdownMode === 'by-key'" class="w-full text-sm">
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

        <!-- By Model table -->
        <table v-else class="w-full text-sm">
          <thead>
            <tr class="border-b border-gray-100">
              <th class="px-6 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wide">Model</th>
              <th class="px-6 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wide">Total Spend</th>
              <th class="px-6 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wide">Requests</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!spendData?.model_rows?.length">
              <td colspan="3" class="px-6 py-12 text-center text-sm text-gray-500">
                <div class="font-medium text-gray-700 mb-1">No model data</div>
                <div v-if="hasNonDefaultFilter">No model data matches the selected filters.</div>
                <div v-else>No requests were recorded in this date range.</div>
              </td>
            </tr>
            <tr
              v-for="row in spendData?.model_rows ?? []"
              :key="row.model"
              class="border-b border-gray-50 hover:bg-gray-50 transition-colors"
            >
              <td class="px-6 py-3 text-sm font-medium text-gray-900">{{ row.model }}</td>
              <td class="px-6 py-3 text-sm text-gray-900">{{ formatSpend(row.total_spend) }}</td>
              <td class="px-6 py-3 text-sm text-gray-500">{{ row.request_count.toLocaleString() }}</td>
            </tr>
          </tbody>
        </table>
      </div>

    </template>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { api } from '../api/client.js'
import LoadingSpinner from '../components/LoadingSpinner.vue'
import ErrorAlert from '../components/ErrorAlert.vue'
import StatusBadge from '../components/StatusBadge.vue'

// State — current filtered response
const loading = ref(false)
const error = ref('')
const spendData = ref(null)  // { rows: [], model_rows: [], daily_rows: [], alerts: [], from: '', to: '' }

// Chart / breakdown view toggles
const chartMode = ref('total')       // 'total' | 'over-time'
const breakdownMode = ref('by-key')  // 'by-key' | 'by-model'

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

// Over-time chart (area) — driven by daily_rows from the server response
const overTimeChartSeries = computed(() => [{
  name: 'Spend',
  data: (spendData.value?.daily_rows ?? []).map(r => parseFloat(r.total_spend.toFixed(4))),
}])

const overTimeChartOptions = computed(() => ({
  chart: { type: 'area', toolbar: { show: false } },
  colors: ['#4F46E5'],
  stroke: { curve: 'smooth', width: 2 },
  fill: { type: 'gradient', gradient: { shadeIntensity: 1, opacityFrom: 0.35, opacityTo: 0.05 } },
  xaxis: {
    categories: (spendData.value?.daily_rows ?? []).map(r => r.day),
    labels: { style: { colors: '#6B7280' }, rotate: -30 },
  },
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
</script>
