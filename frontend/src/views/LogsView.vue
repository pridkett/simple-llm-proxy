<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold text-gray-900 dark:text-gray-100">Request Logs</h1>
      <button class="btn-secondary text-xs" :disabled="loading" @click="load">Refresh</button>
    </div>

    <!-- Filters: Row 1 — Date range presets (D-04) -->
    <div class="mb-4 flex flex-wrap gap-3 items-end">
      <div class="flex gap-1">
        <button
          v-for="range in [null, 'today', '7d', '30d', 'custom']"
          :key="range || 'all'"
          @click="datePreset = range"
          :class="datePreset === range ? 'bg-indigo-50 dark:bg-indigo-950 text-indigo-700 dark:text-indigo-300 font-bold' : 'btn-secondary'"
          class="btn-secondary text-xs py-2"
        >
          {{ { null: 'All time', today: 'Today', '7d': '7d', '30d': '30d', custom: 'Custom' }[range] }}
        </button>
      </div>
      <template v-if="datePreset === 'custom'">
        <div>
          <label class="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">From</label>
          <input type="date" v-model="customFrom" @change="onCustomDateChange"
                 class="input text-sm py-2 max-w-[140px]" />
        </div>
        <div>
          <label class="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">To</label>
          <input type="date" v-model="customTo" @change="onCustomDateChange"
                 class="input text-sm py-2 max-w-[140px]" />
        </div>
      </template>
    </div>

    <!-- Filters: Row 2 — Dimension dropdowns (D-07, D-08) -->
    <div class="mb-4 flex flex-wrap gap-3 items-end">
      <div>
        <label class="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">Team</label>
        <select v-model="filterTeam" class="input text-sm py-2 min-w-[160px]">
          <option :value="null">All Teams</option>
          <option v-for="t in metaTeams" :key="t.id" :value="t.id">{{ t.name }}</option>
        </select>
      </div>
      <div>
        <label class="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">Application</label>
        <select v-model="filterApp" class="input text-sm py-2 min-w-[160px]">
          <option :value="null">All Applications</option>
          <option v-for="a in metaApps" :key="a.id" :value="a.id">{{ a.name }}</option>
        </select>
      </div>
      <div>
        <label class="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">Key</label>
        <select v-model="filterKey" class="input text-sm py-2 min-w-[160px]">
          <option :value="null">All Keys</option>
          <option v-for="k in metaKeys" :key="k.id" :value="k.id">{{ k.name }}</option>
        </select>
      </div>
      <div>
        <label class="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">Provider</label>
        <select v-model="filterProvider" class="input text-sm py-2 min-w-[160px]">
          <option value="">All Providers</option>
          <option v-for="p in metaProviders" :key="p" :value="p">{{ p }}</option>
        </select>
      </div>
      <div>
        <label class="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">Pool</label>
        <select v-model="filterPool" class="input text-sm py-2 min-w-[160px]">
          <option value="">All Pools</option>
          <option v-for="p in metaPools" :key="p" :value="p">{{ p }}</option>
        </select>
      </div>
      <div>
        <label class="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">Model</label>
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

    <form class="mb-4 flex flex-wrap gap-3 items-end" @submit.prevent="applySession">
      <div class="w-full sm:w-80">
        <label for="session-filter" class="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1">Session ID</label>
        <input id="session-filter" v-model="sessionSearch" class="input text-sm py-2 w-full"
               placeholder="Exact session ID" maxlength="256" />
      </div>
      <button class="btn-secondary text-xs py-2" type="submit">Filter session</button>
      <button v-if="filterSession" class="btn-secondary text-xs py-2" type="button" @click="removeSession">Clear session</button>
    </form>
    <div v-if="filterSession" class="mb-4 rounded-lg border border-indigo-100 dark:border-indigo-900 bg-indigo-50 dark:bg-indigo-950 px-4 py-3 text-sm text-indigo-900 dark:text-indigo-200" role="status">
      <p class="font-medium">Session <span class="font-mono break-all">{{ filterSession }}</span></p>
      <p class="text-xs mt-1">{{ loading ? 'Loading requests…' : `${logsData?.total || 0} requests matching current filters` }} · {{ datePreset === null ? 'All time' : 'Selected date range' }}</p>
    </div>

    <LoadingSpinner v-if="loading" />
    <ErrorAlert v-else-if="error" title="Failed to load logs" :message="error" />

    <template v-else-if="logsData">
      <div class="card overflow-hidden">
        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-100 dark:divide-gray-700 text-sm">
            <thead class="bg-gray-50 dark:bg-gray-900">
              <tr>
                <th class="px-4 py-3 text-left text-xs font-bold text-gray-500 dark:text-gray-400 uppercase tracking-wider">Time</th>
                <th class="px-4 py-3 text-left text-xs font-bold text-gray-500 dark:text-gray-400 uppercase tracking-wider">Request ID</th>
                <th class="px-4 py-3 text-left text-xs font-bold text-gray-500 dark:text-gray-400 uppercase tracking-wider">Key</th>
                <th class="px-4 py-3 text-left text-xs font-bold text-gray-500 dark:text-gray-400 uppercase tracking-wider">Model</th>
                <th class="px-4 py-3 text-left text-xs font-bold text-gray-500 dark:text-gray-400 uppercase tracking-wider">Provider</th>
                <th class="px-4 py-3 text-left text-xs font-bold text-gray-500 dark:text-gray-400 uppercase tracking-wider">Endpoint</th>
                <th class="px-4 py-3 text-right text-xs font-bold text-gray-500 dark:text-gray-400 uppercase tracking-wider">Input Tokens</th>
                <th class="px-4 py-3 text-right text-xs font-bold text-gray-500 dark:text-gray-400 uppercase tracking-wider">Output Tokens</th>
                <th class="px-4 py-3 text-right text-xs font-bold text-gray-500 dark:text-gray-400 uppercase tracking-wider">Cost</th>
                <th class="px-4 py-3 text-right text-xs font-bold text-gray-500 dark:text-gray-400 uppercase tracking-wider">Latency</th>
                <th class="px-4 py-3 text-right text-xs font-bold text-gray-500 dark:text-gray-400 uppercase tracking-wider">TTFT</th>
                <th class="px-4 py-3 text-left text-xs font-bold text-gray-500 dark:text-gray-400 uppercase tracking-wider">Status</th>
              </tr>
            </thead>
            <tbody class="bg-white dark:bg-gray-800 divide-y divide-gray-50 dark:divide-gray-700">
              <template v-for="log in logsData.logs" :key="log.request_id">
                <tr @click="toggleRow(log.request_id)"
                    class="hover:bg-gray-50 dark:hover:bg-gray-900 transition-colors cursor-pointer">
                  <td class="px-4 py-3 text-gray-500 dark:text-gray-400 whitespace-nowrap text-xs">
                    {{ formatDate(log.request_time) }}
                  </td>
                  <td class="px-4 py-3 text-gray-500 dark:text-gray-400 font-mono text-xs whitespace-nowrap" :title="log.request_id">
                    {{ truncateID(log.request_id) }}
                  </td>
                  <td class="px-4 py-3 text-gray-600 dark:text-gray-300 text-xs whitespace-nowrap" :title="formatKeyFull(log)">
                    {{ formatKeyLabel(log) }}
                  </td>
                  <td class="px-4 py-3 font-medium">{{ log.model }}</td>
                  <td class="px-4 py-3 text-gray-600 dark:text-gray-300 capitalize">{{ log.provider }}</td>
                  <td class="px-4 py-3 text-gray-500 dark:text-gray-400 font-mono text-xs">{{ log.endpoint }}</td>
                  <td class="px-4 py-3 text-right text-gray-600 dark:text-gray-300">{{ log.usage_known === false ? '—' : (log.prompt_tokens || 0).toLocaleString() }}</td>
                  <td class="px-4 py-3 text-right text-gray-600 dark:text-gray-300">{{ log.usage_known === false ? '—' : (log.completion_tokens || 0).toLocaleString() }}</td>
                  <td class="px-4 py-3 text-right text-gray-600 dark:text-gray-300 font-mono text-xs">{{ log.usage_known === false ? 'Unknown' : formatCost(log.total_cost) }}</td>
                  <td class="px-4 py-3 text-right text-gray-600 dark:text-gray-300">{{ log.latency_ms }}ms</td>
                  <td class="px-4 py-3 text-right text-gray-600 dark:text-gray-300">
                    {{ log.ttft_ms != null ? log.ttft_ms + 'ms' : '—' }}
                  </td>
                  <td class="px-4 py-3">
                    <span
                      class="px-2 py-0.5 rounded text-xs font-medium"
                      :class="log.status_code < 400 ? 'bg-green-50 dark:bg-green-950 text-green-700 dark:text-green-300' : 'bg-red-50 dark:bg-red-950 text-red-700 dark:text-red-300'"
                    >
                      {{ log.status_code }}
                    </span>
                  </td>
                </tr>
                <!-- Accordion detail sub-row (UI-05) -->
                <tr v-if="expandedRows.has(log.request_id)">
                  <td colspan="12" class="bg-gray-50 dark:bg-gray-900 px-4 py-4">
                    <LoadingSpinner v-if="detailLoading.has(log.request_id)" />
                    <ErrorAlert
                      v-else-if="detailErrors.has(log.request_id)"
                      title="Failed to load log detail"
                      :message="detailErrors.get(log.request_id)"
                    />
                    <div
                      v-else-if="detailCache.has(log.request_id)"
                      class="grid grid-cols-2 gap-6"
                    >
                      <!-- LEFT COLUMN: Request body + Attribution -->
                      <div>
                        <p class="text-xs font-bold text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-2">Request</p>
                        <p class="text-xs text-gray-500 dark:text-gray-400 mb-1">Request Body</p>
                        <pre
                          v-if="detailCache.get(log.request_id).req_body_snippet"
                          class="text-xs font-mono bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded p-3 whitespace-pre-wrap break-all overflow-auto max-h-40 text-gray-700 dark:text-gray-200"
                        >{{ detailCache.get(log.request_id).req_body_snippet }}</pre>
                        <p v-else class="text-xs text-gray-400 italic">No request body captured.</p>

                        <p class="text-xs font-bold text-gray-500 dark:text-gray-400 uppercase tracking-wider mt-4 mb-2">Attribution</p>
                        <div class="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
                          <span class="text-gray-500 dark:text-gray-400">Session ID</span>
                          <RouterLink v-if="detailCache.get(log.request_id).session_id"
                                      :to="{ path: '/logs', query: { session_id: detailCache.get(log.request_id).session_id } }"
                                      @click="resetDimensions"
                                      class="font-mono text-indigo-600 dark:text-indigo-400 hover:underline break-all"
                                      title="View all requests in this session">
                            {{ detailCache.get(log.request_id).session_id }}
                          </RouterLink>
                          <span v-else class="text-gray-400">—</span>
                          <span class="text-gray-500 dark:text-gray-400">Team</span>
                          <span class="text-gray-800 dark:text-gray-200 font-bold">{{ detailCache.get(log.request_id).team_name || '—' }}</span>
                          <span class="text-gray-500 dark:text-gray-400">Application</span>
                          <span class="text-gray-800 dark:text-gray-200 font-bold">{{ detailCache.get(log.request_id).app_name || '—' }}</span>
                          <span class="text-gray-500 dark:text-gray-400">Key</span>
                          <span class="text-gray-800 dark:text-gray-200 font-bold">{{ detailCache.get(log.request_id).key_name || '—' }}</span>
                          <span class="text-gray-500 dark:text-gray-400">Pool</span>
                          <span class="text-gray-800 dark:text-gray-200 font-bold">{{ detailCache.get(log.request_id).pool_name || '—' }}</span>
                        </div>
                      </div>

                      <!-- RIGHT COLUMN: Response body + Tokens & Cost -->
                      <div>
                        <p class="text-xs font-bold text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-2">Response</p>
                        <p class="text-xs text-gray-500 dark:text-gray-400 mb-1">
                          {{ detailCache.get(log.request_id).status_code >= 400 &&
                             detailCache.get(log.request_id).resp_body_snippet
                             ? 'Error Response' : 'Response Body' }}
                        </p>
                        <pre
                          v-if="detailCache.get(log.request_id).resp_body_snippet"
                          class="text-xs font-mono bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded p-3 whitespace-pre-wrap break-all overflow-auto max-h-40 text-gray-700 dark:text-gray-200"
                        >{{ detailCache.get(log.request_id).resp_body_snippet }}</pre>
                        <p v-else class="text-xs text-gray-400 italic">No response body captured.</p>

                        <p class="text-xs font-bold text-gray-500 dark:text-gray-400 uppercase tracking-wider mt-4 mb-2">Tokens &amp; Cost</p>
                        <div class="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
                          <span class="text-gray-500 dark:text-gray-400">Input</span>
                          <span class="text-gray-800 dark:text-gray-200 font-bold">{{ (detailCache.get(log.request_id).prompt_tokens || 0).toLocaleString() }}</span>
                          <span class="text-gray-500 dark:text-gray-400">Output</span>
                          <span class="text-gray-800 dark:text-gray-200 font-bold">{{ (detailCache.get(log.request_id).completion_tokens || 0).toLocaleString() }}</span>
                          <span class="text-gray-500 dark:text-gray-400">Cache Read</span>
                          <span class="text-gray-800 dark:text-gray-200 font-bold">{{ (detailCache.get(log.request_id).cache_read_tokens || 0).toLocaleString() }}</span>
                          <span class="text-gray-500 dark:text-gray-400">Cache Write</span>
                          <span class="text-gray-800 dark:text-gray-200 font-bold">{{ (detailCache.get(log.request_id).cache_write_tokens || 0).toLocaleString() }}</span>
                          <span class="text-gray-500 dark:text-gray-400">TTFT</span>
                          <span class="text-gray-800 dark:text-gray-200 font-bold">
                            {{ detailCache.get(log.request_id).ttft_ms != null
                               ? detailCache.get(log.request_id).ttft_ms + 'ms' : '—' }}
                          </span>
                          <span class="text-gray-500 dark:text-gray-400">Cost</span>
                          <span class="text-gray-800 dark:text-gray-200 font-bold">{{ detailCache.get(log.request_id).usage_known === false ? 'Unknown' : formatCost(detailCache.get(log.request_id).total_cost) }}</span>
                        </div>
                      </div>
                    </div>
                  </td>
                </tr>
              </template>
              <tr v-if="!logsData.logs || !logsData.logs.length">
                <td colspan="12" class="px-4 py-12 text-center text-gray-500 dark:text-gray-400">
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
          class="px-4 py-3 border-t border-gray-100 dark:border-gray-700 flex items-center justify-between"
        >
          <p class="text-xs text-gray-500 dark:text-gray-400">
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
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { useRoute, useRouter, RouterLink } from 'vue-router'
import { api } from '../api/client.js'
import LoadingSpinner from '../components/LoadingSpinner.vue'
import ErrorAlert from '../components/ErrorAlert.vue'

const route = useRoute()
const router = useRouter()
const filterSession = computed(() => typeof route.query.session_id === 'string' ? route.query.session_id : '')
const sessionSearch = ref(filterSession.value)

function applySession() {
  const sessionID = sessionSearch.value.trim()
  router.push({ query: { ...route.query, session_id: sessionID || undefined } })
}

function removeSession() {
  sessionSearch.value = ''
  router.push({ query: { ...route.query, session_id: undefined } })
}

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
    filterModel.value !== '' || filterSession.value !== ''
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

let loadSequence = 0
async function load() {
  const sequence = ++loadSequence
  loading.value = true
  error.value = ''
  try {
    const dateParams = buildDateParams()
    const result = await api.logs({
      limit: pageSize,
      offset: offset.value,
      model: filterModel.value || undefined,
      session_id: filterSession.value || undefined,
      team_id: filterTeam.value,
      app_id: filterApp.value,
      key_id: filterKey.value,
      provider: filterProvider.value || undefined,
      pool_name: filterPool.value || undefined,
      ...dateParams,
    })
    if (sequence === loadSequence) logsData.value = result
  } catch (e) {
    if (sequence === loadSequence) error.value = e?.message || 'Failed to load logs'
  } finally {
    if (sequence === loadSequence) loading.value = false
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
  [datePreset, filterTeam, filterApp, filterKey, filterProvider, filterPool, filterModel, filterSession],
  () => {
    offset.value = 0
    load()
  }
)

watch(filterSession, (value) => {
  sessionSearch.value = value
  expandedRows.value = new Set()
})

let customDateDebounceTimer = null
function onCustomDateChange() {
  clearTimeout(customDateDebounceTimer)
  customDateDebounceTimer = setTimeout(() => {
    offset.value = 0
    load()
  }, 300)
}
watch([customFrom, customTo], onCustomDateChange)

onBeforeUnmount(() => {
  clearTimeout(customDateDebounceTimer)
  loadSequence++
})

onMounted(async () => {
  await loadMeta()
  await load()
})

function clearFilters() {
  resetDimensions()
  removeSession()
}

function resetDimensions() {
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

async function toggleRow(requestId) {
  if (expandedRows.value.has(requestId)) {
    expandedRows.value.delete(requestId)
    expandedRows.value = new Set(expandedRows.value)
    return
  }
  expandedRows.value.add(requestId)
  expandedRows.value = new Set(expandedRows.value)

  if (!detailCache.value.has(requestId)) {
    detailLoading.value.add(requestId)
    detailLoading.value = new Set(detailLoading.value)
    try {
      const detail = await api.logDetail(requestId)
      detailCache.value.set(requestId, detail)
      detailCache.value = new Map(detailCache.value)
    } catch (e) {
      detailErrors.value.set(requestId, e.message || 'Unknown error')
      detailErrors.value = new Map(detailErrors.value)
    } finally {
      detailLoading.value.delete(requestId)
      detailLoading.value = new Set(detailLoading.value)
    }
  }
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
