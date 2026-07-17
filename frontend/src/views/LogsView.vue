<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold text-gray-900">Request Logs</h1>
      <button class="btn-secondary text-xs" :disabled="loading" @click="load">Refresh</button>
    </div>

    <!-- Filters: Row 1 — Date range presets (D-04) -->
    <div class="mb-4 flex flex-wrap gap-3 items-end">
      <div class="flex gap-1">
        <button
          v-for="range in ['today', '7d', '30d', 'custom']"
          :key="range"
          @click="datePreset = range"
          :class="datePreset === range ? 'bg-indigo-50 text-indigo-700 font-bold' : 'btn-secondary'"
          class="btn-secondary text-xs py-2"
        >
          {{ { today: 'Today', '7d': '7d', '30d': '30d', custom: 'Custom' }[range] }}
        </button>
      </div>
      <template v-if="datePreset === 'custom'">
        <div>
          <label class="block text-xs font-medium text-gray-500 mb-1">From</label>
          <input type="date" v-model="customFrom" @change="onCustomDateChange"
                 class="input text-sm py-2 max-w-[140px]" />
        </div>
        <div>
          <label class="block text-xs font-medium text-gray-500 mb-1">To</label>
          <input type="date" v-model="customTo" @change="onCustomDateChange"
                 class="input text-sm py-2 max-w-[140px]" />
        </div>
      </template>
    </div>

    <!-- Filters: Row 2 — Dimension dropdowns (D-07, D-08) -->
    <div class="mb-4 flex flex-wrap gap-3 items-end">
      <div>
        <label class="block text-xs font-medium text-gray-500 mb-1">Team</label>
        <select v-model="filterTeam" class="input text-sm py-2 min-w-[160px]">
          <option :value="null">All Teams</option>
          <option v-for="t in metaTeams" :key="t.id" :value="t.id">{{ t.name }}</option>
        </select>
      </div>
      <div>
        <label class="block text-xs font-medium text-gray-500 mb-1">Application</label>
        <select v-model="filterApp" class="input text-sm py-2 min-w-[160px]">
          <option :value="null">All Applications</option>
          <option v-for="a in metaApps" :key="a.id" :value="a.id">{{ a.name }}</option>
        </select>
      </div>
      <div>
        <label class="block text-xs font-medium text-gray-500 mb-1">Key</label>
        <select v-model="filterKey" class="input text-sm py-2 min-w-[160px]">
          <option :value="null">All Keys</option>
          <option v-for="k in metaKeys" :key="k.id" :value="k.id">{{ k.name }}</option>
        </select>
      </div>
      <div>
        <label class="block text-xs font-medium text-gray-500 mb-1">Provider</label>
        <select v-model="filterProvider" class="input text-sm py-2 min-w-[160px]">
          <option value="">All Providers</option>
          <option v-for="p in metaProviders" :key="p" :value="p">{{ p }}</option>
        </select>
      </div>
      <div>
        <label class="block text-xs font-medium text-gray-500 mb-1">Pool</label>
        <select v-model="filterPool" class="input text-sm py-2 min-w-[160px]">
          <option value="">All Pools</option>
          <option v-for="p in metaPools" :key="p" :value="p">{{ p }}</option>
        </select>
      </div>
      <div>
        <label class="block text-xs font-medium text-gray-500 mb-1">Model</label>
        <select v-model="filterModel" class="input text-sm py-2 min-w-[160px]">
          <option value="">All Models</option>
          <option v-for="m in metaModels" :key="m" :value="m">{{ m }}</option>
        </select>
      </div>
      <button
        v-if="hasActiveFilters"
        class="btn-secondary text-xs py-2"
        @click="clearFilters"
      >
        Clear Filters
      </button>
    </div>

    <LoadingSpinner v-if="loading" />
    <ErrorAlert v-else-if="error" title="Failed to load logs" :message="error" />

    <template v-else-if="logsData">
      <div class="card overflow-hidden">
        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-100 text-sm">
            <thead class="bg-gray-50">
              <tr>
                <th class="px-4 py-3 text-left text-xs font-bold text-gray-500 uppercase tracking-wider">Time</th>
                <th class="px-4 py-3 text-left text-xs font-bold text-gray-500 uppercase tracking-wider">Request ID</th>
                <th class="px-4 py-3 text-left text-xs font-bold text-gray-500 uppercase tracking-wider">Key</th>
                <th class="px-4 py-3 text-left text-xs font-bold text-gray-500 uppercase tracking-wider">Model</th>
                <th class="px-4 py-3 text-left text-xs font-bold text-gray-500 uppercase tracking-wider">Provider</th>
                <th class="px-4 py-3 text-left text-xs font-bold text-gray-500 uppercase tracking-wider">Endpoint</th>
                <th class="px-4 py-3 text-right text-xs font-bold text-gray-500 uppercase tracking-wider">Input Tokens</th>
                <th class="px-4 py-3 text-right text-xs font-bold text-gray-500 uppercase tracking-wider">Output Tokens</th>
                <th class="px-4 py-3 text-right text-xs font-bold text-gray-500 uppercase tracking-wider">Cost</th>
                <th class="px-4 py-3 text-right text-xs font-bold text-gray-500 uppercase tracking-wider">Latency</th>
                <th class="px-4 py-3 text-right text-xs font-bold text-gray-500 uppercase tracking-wider">TTFT</th>
                <th class="px-4 py-3 text-left text-xs font-bold text-gray-500 uppercase tracking-wider">Status</th>
              </tr>
            </thead>
            <tbody class="bg-white divide-y divide-gray-50">
              <tr v-for="log in logsData.logs" :key="log.request_id"
                  class="hover:bg-gray-50 transition-colors cursor-pointer"
                  @click="toggleRow(log.request_id)">
                <td class="px-4 py-3 text-gray-500 whitespace-nowrap text-xs">
                  {{ formatDate(log.request_time) }}
                </td>
                <td class="px-4 py-3 text-gray-500 font-mono text-xs whitespace-nowrap" :title="log.request_id">
                  {{ truncateID(log.request_id) }}
                </td>
                <td class="px-4 py-3 text-gray-600 text-xs whitespace-nowrap" :title="formatKeyFull(log)">
                  {{ formatKeyLabel(log) }}
                </td>
                <td class="px-4 py-3 font-medium">{{ log.model }}</td>
                <td class="px-4 py-3 text-gray-600 capitalize">{{ log.provider }}</td>
                <td class="px-4 py-3 text-gray-500 font-mono text-xs">{{ log.endpoint }}</td>
                <td class="px-4 py-3 text-right text-gray-600">{{ (log.prompt_tokens || 0).toLocaleString() }}</td>
                <td class="px-4 py-3 text-right text-gray-600">{{ (log.completion_tokens || 0).toLocaleString() }}</td>
                <td class="px-4 py-3 text-right text-gray-600 font-mono text-xs">{{ formatCost(log.total_cost) }}</td>
                <td class="px-4 py-3 text-right text-gray-600">{{ log.latency_ms }}ms</td>
                <td class="px-4 py-3 text-right text-gray-600">
                  {{ log.ttft_ms != null ? log.ttft_ms + 'ms' : '—' }}
                </td>
                <td class="px-4 py-3">
                  <span
                    class="px-2 py-0.5 rounded text-xs font-medium"
                    :class="log.status_code < 400 ? 'bg-green-50 text-green-700' : 'bg-red-50 text-red-700'"
                  >
                    {{ log.status_code }}
                  </span>
                </td>
              </tr>
              <tr v-if="!logsData.logs || !logsData.logs.length">
                <td colspan="12" class="px-4 py-12 text-center text-gray-500">
                  <span v-if="hasActiveFilters">
                    No logs match the current filters. Try clearing one or more filters.
                  </span>
                  <span v-else>
                    No request logs yet. Logs appear after sending requests through the proxy.
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- Pagination -->
        <div
          v-if="logsData.total > pageSize"
          class="px-4 py-3 border-t border-gray-100 flex items-center justify-between"
        >
          <p class="text-xs text-gray-500">
            Showing {{ offset + 1 }}–{{ Math.min(offset + pageSize, logsData.total) }}
            of {{ logsData.total }} logs
          </p>
          <div class="flex gap-2">
            <button
              class="btn-secondary text-xs"
              :disabled="offset === 0"
              @click="prevPage"
            >
              Previous
            </button>
            <button
              class="btn-secondary text-xs"
              :disabled="offset + pageSize >= logsData.total"
              @click="nextPage"
            >
              Next
            </button>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { api } from '../api/client.js'
import LoadingSpinner from '../components/LoadingSpinner.vue'
import ErrorAlert from '../components/ErrorAlert.vue'

const logsData = ref(null)
const loading = ref(false)
const error = ref('')
const offset = ref(0)
const pageSize = 50

// Date range filter state (D-04, D-06)
const datePreset = ref(null) // null = no preset, full history (D-06)
const customFrom = ref('')
const customTo = ref('')

// Dimension filter refs (D-08):
// ID-bearing: team, app, key — null = "no filter"; value = integer ID
const filterTeam = ref(null)
const filterApp = ref(null)
const filterKey = ref(null)
// String-bearing: provider, pool, model — '' = "no filter"
const filterProvider = ref('')
const filterPool = ref('')
const filterModel = ref('')

// Meta API data for dropdown options (UI-02)
const metaTeams = ref([])
const metaApps = ref([])
const metaKeys = ref([])
const metaProviders = ref([])
const metaPools = ref([])
const metaModels = ref([])

// Accordion state (Wave 3 extends these — declared here so Wave 3 can add behavior)
const expandedRows = ref(new Set())
const detailCache = ref(new Map())
const detailLoading = ref(new Set())
const detailErrors = ref(new Map())

const hasActiveFilters = computed(() => {
  return datePreset.value !== null ||
    filterTeam.value !== null ||
    filterApp.value !== null ||
    filterKey.value !== null ||
    filterProvider.value !== '' ||
    filterPool.value !== '' ||
    filterModel.value !== ''
})

function buildDateParams() {
  const now = new Date()
  if (datePreset.value === 'today') {
    const startOfDay = new Date(now.getFullYear(), now.getMonth(), now.getDate())
    return { date_from: startOfDay.toISOString(), date_to: now.toISOString() }
  }
  if (datePreset.value === '7d') {
    return { date_from: new Date(now.getTime() - 7 * 86400000).toISOString() }
  }
  if (datePreset.value === '30d') {
    return { date_from: new Date(now.getTime() - 30 * 86400000).toISOString() }
  }
  if (datePreset.value === 'custom' && customFrom.value && customTo.value) {
    return {
      date_from: new Date(customFrom.value + 'T00:00:00').toISOString(),
      date_to: new Date(customTo.value + 'T23:59:59').toISOString(),
    }
  }
  return {} // D-06: no preset = no date params = full history
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const dateParams = buildDateParams()
    logsData.value = await api.logs({
      limit: pageSize,
      offset: offset.value,
      model: filterModel.value || undefined,
      team_id: filterTeam.value,
      app_id: filterApp.value,
      key_id: filterKey.value,
      provider: filterProvider.value || undefined,
      pool_name: filterPool.value || undefined,
      ...dateParams,
    })
  } catch (e) {
    error.value = e?.message || 'Failed to load logs'
  } finally {
    loading.value = false
  }
}

async function loadMeta() {
  try {
    const meta = await api.logsMeta()
    if (meta) {
      metaTeams.value = meta.teams || []
      metaApps.value = meta.apps || []
      metaKeys.value = meta.keys || []
      metaProviders.value = meta.providers || []
      metaPools.value = meta.pools || []
      metaModels.value = meta.models || []
    }
  } catch (e) {
    // meta fetch failure is non-fatal — dropdowns stay empty
    console.error('Failed to load log metadata:', e)
  }
}

// D-09: any filter change auto-fetches with offset reset
watch(
  [datePreset, filterTeam, filterApp, filterKey, filterProvider, filterPool, filterModel],
  () => {
    offset.value = 0
    load()
  }
)

let customDateDebounceTimer = null
function onCustomDateChange() {
  clearTimeout(customDateDebounceTimer)
  customDateDebounceTimer = setTimeout(() => {
    offset.value = 0
    load()
  }, 300)
}
watch([customFrom, customTo], onCustomDateChange)

onMounted(async () => {
  await loadMeta()
  await load()
})

function clearFilters() {
  datePreset.value = null
  customFrom.value = ''
  customTo.value = ''
  filterTeam.value = null
  filterApp.value = null
  filterKey.value = null
  filterProvider.value = ''
  filterPool.value = ''
  filterModel.value = ''
  // watch triggers load() automatically
}

function toggleRow(requestId) {
  // Wave 3 (Plan 03) implements accordion expand/collapse
}

function prevPage() {
  offset.value = Math.max(0, offset.value - pageSize)
  load()
}

function nextPage() {
  offset.value = offset.value + pageSize
  load()
}

function formatDate(iso) {
  return new Date(iso).toLocaleString()
}

function truncateID(id) {
  if (!id) return ''
  return id.length > 8 ? id.slice(0, 8) + '...' : id
}

function formatCost(cost) {
  if (cost == null || cost === 0) return '-'
  if (cost < 0.01) return '$' + cost.toFixed(4)
  return '$' + cost.toFixed(2)
}

function formatKeyLabel(log) {
  if (!log.api_key_id) return 'Master Key'
  const parts = [log.team_name, log.app_name, log.key_name].filter(Boolean)
  if (parts.length === 0) return 'Key #' + log.api_key_id
  return parts.join(' / ')
}

function formatKeyFull(log) {
  if (!log.api_key_id) return 'Master Key (no API key)'
  return `Team: ${log.team_name || 'N/A'} | App: ${log.app_name || 'N/A'} | Key: ${log.key_name || 'N/A'}`
}
</script>
