<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold text-gray-900">Request Logs</h1>
      <button class="btn-secondary text-xs" :disabled="loading" @click="load">Refresh</button>
    </div>

    <!-- Filters -->
    <div class="mb-4 flex flex-wrap gap-3 items-end">
      <div>
        <label class="block text-xs font-medium text-gray-500 mb-1">Model</label>
        <select v-model="filterModel" class="input-field text-sm py-1.5 min-w-[160px]" @change="applyFilters">
          <option value="">All Models</option>
          <option v-for="m in uniqueModels" :key="m" :value="m">{{ m }}</option>
        </select>
      </div>
      <div>
        <label class="block text-xs font-medium text-gray-500 mb-1">Team</label>
        <select v-model="filterTeam" class="input-field text-sm py-1.5 min-w-[160px]" @change="applyFilters">
          <option value="">All Teams</option>
          <option v-for="t in uniqueTeams" :key="t" :value="t">{{ t }}</option>
        </select>
      </div>
      <div>
        <label class="block text-xs font-medium text-gray-500 mb-1">Application</label>
        <select v-model="filterApp" class="input-field text-sm py-1.5 min-w-[160px]" @change="applyFilters">
          <option value="">All Applications</option>
          <option v-for="a in uniqueApps" :key="a" :value="a">{{ a }}</option>
        </select>
      </div>
      <button
        v-if="filterModel || filterTeam || filterApp"
        class="btn-secondary text-xs py-1.5"
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
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Time</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Request ID</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Key</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Model</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Provider</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Endpoint</th>
                <th class="px-4 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider">Input Tokens</th>
                <th class="px-4 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider">Output Tokens</th>
                <th class="px-4 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider">Cost</th>
                <th class="px-4 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider">Latency</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Status</th>
              </tr>
            </thead>
            <tbody class="bg-white divide-y divide-gray-50">
              <tr v-for="log in filteredLogs" :key="log.request_id" class="hover:bg-gray-50 transition-colors">
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
                <td class="px-4 py-3">
                  <span
                    class="px-2 py-0.5 rounded text-xs font-medium"
                    :class="log.status_code < 400 ? 'bg-green-50 text-green-700' : 'bg-red-50 text-red-700'"
                  >
                    {{ log.status_code }}
                  </span>
                </td>
              </tr>
              <tr v-if="!filteredLogs.length">
                <td colspan="11" class="px-4 py-12 text-center text-gray-500">
                  No request logs yet. Logs appear after sending requests through the proxy.
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
import { ref, computed, onMounted } from 'vue'
import { api } from '../api/client.js'
import LoadingSpinner from '../components/LoadingSpinner.vue'
import ErrorAlert from '../components/ErrorAlert.vue'

const logsData = ref(null)
const loading = ref(false)
const error = ref('')
const offset = ref(0)
const pageSize = 50

// Filter state
const filterModel = ref('')
const filterTeam = ref('')
const filterApp = ref('')

// Unique values for filter dropdowns, extracted from loaded data.
const uniqueModels = computed(() => {
  if (!logsData.value?.logs) return []
  return [...new Set(logsData.value.logs.map(l => l.model).filter(Boolean))].sort()
})

const uniqueTeams = computed(() => {
  if (!logsData.value?.logs) return []
  return [...new Set(logsData.value.logs.map(l => l.team_name).filter(Boolean))].sort()
})

const uniqueApps = computed(() => {
  if (!logsData.value?.logs) return []
  return [...new Set(logsData.value.logs.map(l => l.app_name).filter(Boolean))].sort()
})

// Client-side filtering of the loaded page.
const filteredLogs = computed(() => {
  if (!logsData.value?.logs) return []
  return logsData.value.logs.filter(log => {
    if (filterModel.value && log.model !== filterModel.value) return false
    if (filterTeam.value && log.team_name !== filterTeam.value) return false
    if (filterApp.value && log.app_name !== filterApp.value) return false
    return true
  })
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    logsData.value = await api.logs({ limit: pageSize, offset: offset.value })
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

onMounted(load)

function applyFilters() {
  // Filters apply client-side to the currently loaded page.
  // No additional fetch needed.
}

function clearFilters() {
  filterModel.value = ''
  filterTeam.value = ''
  filterApp.value = ''
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
