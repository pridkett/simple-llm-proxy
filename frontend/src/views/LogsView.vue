<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold text-gray-900">Request Logs</h1>
      <button class="btn-secondary text-xs" :disabled="loading" @click="load">Refresh</button>
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
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Model</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Provider</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Endpoint</th>
                <th class="px-4 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider">Tokens</th>
                <th class="px-4 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider">Latency</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Status</th>
              </tr>
            </thead>
            <tbody class="bg-white divide-y divide-gray-50">
              <tr v-for="log in logsData.logs" :key="log.request_id" class="hover:bg-gray-50 transition-colors">
                <td class="px-4 py-3 text-gray-500 whitespace-nowrap text-xs">
                  {{ formatDate(log.request_time) }}
                </td>
                <td class="px-4 py-3 text-gray-500 font-mono text-xs whitespace-nowrap" :title="log.request_id">
                  {{ truncateID(log.request_id) }}
                </td>
                <td class="px-4 py-3 font-medium">{{ log.model }}</td>
                <td class="px-4 py-3 text-gray-600 capitalize">{{ log.provider }}</td>
                <td class="px-4 py-3 text-gray-500 font-mono text-xs">{{ log.endpoint }}</td>
                <td class="px-4 py-3 text-right text-gray-600">{{ log.total_tokens.toLocaleString() }}</td>
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
              <tr v-if="!logsData.logs?.length">
                <td colspan="8" class="px-4 py-12 text-center text-gray-500">
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
import { ref, onMounted } from 'vue'
import { api } from '../api/client.js'
import LoadingSpinner from '../components/LoadingSpinner.vue'
import ErrorAlert from '../components/ErrorAlert.vue'

const logsData = ref(null)
const loading = ref(false)
const error = ref('')
const offset = ref(0)
const pageSize = 50

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
</script>
