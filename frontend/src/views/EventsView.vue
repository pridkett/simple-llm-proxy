<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-semibold text-gray-900">Events</h1>
      <div class="flex items-center gap-3">
        <select
          v-model="eventTypeFilter"
          @change="onFilterChange"
          class="input text-sm"
        >
          <option value="">All event types</option>
          <option value="provider_failover">Provider Failover</option>
          <option value="budget_exhausted">Budget Exhausted</option>
          <option value="pool_cooldown">Pool Cooldown</option>
        </select>
        <button class="btn-secondary text-xs" :disabled="loading" @click="load">Refresh</button>
      </div>
    </div>

    <LoadingSpinner v-if="loading" />
    <ErrorAlert v-else-if="error" title="Failed to load events" :message="error" />

    <template v-else-if="eventsData">
      <div class="card overflow-hidden">
        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-100 text-sm">
            <thead class="bg-gray-50">
              <tr>
                <th class="px-6 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wider">Time</th>
                <th class="px-6 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wider">Type</th>
                <th class="px-6 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wider">Details</th>
              </tr>
            </thead>
            <tbody class="bg-white divide-y divide-gray-50">
              <template v-for="event in eventsData.events" :key="event.id">
                <tr class="hover:bg-gray-50 transition-colors cursor-pointer" @click="toggleDetail(event.id)">
                  <td class="px-6 py-3 text-gray-500 whitespace-nowrap text-xs" :title="formatFullDate(event.created_at)">
                    {{ relativeTime(event.created_at) }}
                  </td>
                  <td class="px-6 py-3">
                    <span
                      class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium"
                      :class="eventTypeBadgeClasses(event.event_type)"
                    >
                      {{ formatEventType(event.event_type) }}
                    </span>
                  </td>
                  <td class="px-6 py-3 text-gray-700">
                    <span class="font-medium">{{ event.payload?.value1 }}</span>
                    <span v-if="event.payload?.value2" class="text-gray-500 ml-1">{{ event.payload.value2 }}</span>
                  </td>
                </tr>
                <tr v-if="expandedId === event.id">
                  <td colspan="3" class="px-6 py-4 bg-gray-50 border-b border-gray-100">
                    <h4 class="text-xs font-semibold text-gray-500 uppercase tracking-wider mb-3">Event Details</h4>
                    <dl class="grid grid-cols-2 gap-x-6 gap-y-2 text-sm">
                      <template v-for="(value, key) in flattenContext(event.payload?.context)" :key="key">
                        <dt class="text-gray-500 font-semibold">{{ formatContextKey(key) }}</dt>
                        <dd class="text-gray-900">{{ formatContextValue(value) }}</dd>
                      </template>
                    </dl>
                  </td>
                </tr>
              </template>
              <tr v-if="!eventsData.events?.length">
                <td colspan="3" class="px-6 py-12 text-center">
                  <h3 class="text-base font-semibold text-gray-900">No events recorded</h3>
                  <p class="mt-2 text-sm text-gray-500">Routing events appear here when provider failovers, budget exhaustion, or pool cooldowns occur. Events are retained for 30 days.</p>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div
          v-if="eventsData.total > pageSize"
          class="px-4 py-3 border-t border-gray-100 flex items-center justify-between"
        >
          <p class="text-xs text-gray-500">
            Showing {{ offset + 1 }}&ndash;{{ Math.min(offset + pageSize, eventsData.total) }}
            of {{ eventsData.total }} events
          </p>
          <div class="flex gap-2">
            <button class="btn-secondary text-xs" :disabled="offset === 0" @click="prevPage">Previous</button>
            <button class="btn-secondary text-xs" :disabled="offset + pageSize >= eventsData.total" @click="nextPage">Next</button>
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

const eventsData = ref(null)
const loading = ref(false)
const error = ref('')
const offset = ref(0)
const pageSize = 50
const eventTypeFilter = ref('')
const expandedId = ref(null)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const params = { limit: pageSize, offset: offset.value }
    if (eventTypeFilter.value) params.event_type = eventTypeFilter.value
    eventsData.value = await api.events(params)
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

onMounted(load)

function onFilterChange() {
  offset.value = 0
  load()
}

function prevPage() {
  offset.value = Math.max(0, offset.value - pageSize)
  load()
}

function nextPage() {
  offset.value = offset.value + pageSize
  load()
}

function toggleDetail(id) {
  expandedId.value = expandedId.value === id ? null : id
}

function relativeTime(iso) {
  const now = Date.now()
  const then = new Date(iso).getTime()
  const diffSec = Math.floor((now - then) / 1000)
  if (diffSec < 60) return 'just now'
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)}m ago`
  if (diffSec < 86400) return `${Math.floor(diffSec / 3600)}h ago`
  return `${Math.floor(diffSec / 86400)}d ago`
}

function formatFullDate(iso) {
  return new Date(iso).toLocaleString()
}

function eventTypeBadgeClasses(type) {
  switch (type) {
    case 'provider_failover': return 'bg-amber-50 text-amber-700'
    case 'budget_exhausted': return 'bg-red-50 text-red-700'
    case 'pool_cooldown': return 'bg-red-50 text-red-700'
    default: return 'bg-gray-100 text-gray-600'
  }
}

function formatEventType(type) {
  switch (type) {
    case 'provider_failover': return 'Provider Failover'
    case 'budget_exhausted': return 'Budget Exhausted'
    case 'pool_cooldown': return 'Pool Cooldown'
    default: return type
  }
}

function flattenContext(context) {
  if (!context || typeof context !== 'object') return {}
  const result = {}
  for (const [key, value] of Object.entries(context)) {
    result[key] = value
  }
  return result
}

function formatContextKey(key) {
  return key.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase())
}

function formatContextValue(value) {
  if (value === null || value === undefined) return '\u2014'
  if (Array.isArray(value)) return value.join(', ')
  return String(value)
}
</script>
