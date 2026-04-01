<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-semibold text-gray-900">Pools</h1>
      <div class="flex items-center gap-3">
        <span class="text-xs text-gray-500" aria-live="polite">Updated {{ secondsAgo }}s ago</span>
        <button class="btn-secondary text-xs" @click="togglePolling">
          {{ polling ? 'Pause' : 'Resume' }}
        </button>
      </div>
    </div>

    <LoadingSpinner v-if="loading" />

    <ErrorAlert
      v-else-if="error"
      title="Failed to load pool status"
      :message="error"
    />

    <div v-else-if="pools && pools.length === 0" class="card">
      <div class="px-6 py-12 text-center">
        <h3 class="text-base font-semibold text-gray-900">No pools configured</h3>
        <p class="mt-2 text-sm text-gray-500">Provider pools are defined in your YAML configuration file. Add a <code class="font-mono text-xs">provider_pools</code> section to config.yaml to group deployments for load balancing and failover.</p>
      </div>
    </div>

    <div class="space-y-4" v-else-if="pools">
      <div v-for="pool in pools" :key="pool.name" class="card overflow-hidden">
        <!-- Card header: pool name, strategy, budget -->
        <div class="px-6 py-4 bg-gray-50 border-b border-gray-100 flex items-center justify-between">
          <div>
            <h2 class="text-base font-semibold text-gray-900">{{ pool.name }}</h2>
            <p class="text-xs text-gray-500 mt-0.5">{{ pool.strategy }}</p>
          </div>
          <div class="text-sm font-semibold text-gray-700">
            <template v-if="pool.budget_cap > 0">
              ${{ pool.budget_spent.toFixed(2) }} / ${{ pool.budget_cap.toFixed(2) }}
            </template>
            <template v-else>Unlimited</template>
          </div>
        </div>

        <!-- Deployment table -->
        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-100 text-sm">
            <thead class="bg-gray-50">
              <tr>
                <th class="px-6 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wider">Provider</th>
                <th class="px-6 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wider">Model</th>
                <th class="px-6 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wider">Status</th>
                <th class="px-6 py-3 text-right text-xs font-semibold text-gray-500 uppercase tracking-wider">Failures</th>
                <th class="px-6 py-3 text-right text-xs font-semibold text-gray-500 uppercase tracking-wider">Weight</th>
              </tr>
            </thead>
            <tbody class="bg-white divide-y divide-gray-50">
              <tr v-for="dep in pool.deployments" :key="dep.provider + ':' + dep.actual_model" class="hover:bg-gray-50 transition-colors">
                <td class="px-6 py-3 text-gray-600 capitalize">{{ dep.provider }}</td>
                <td class="px-6 py-3 font-mono text-xs">{{ dep.actual_model }}</td>
                <td class="px-6 py-3">
                  <StatusBadge :status="dep.status" />
                  <p v-if="dep.status === 'cooldown' && dep.cooldown_until" class="text-xs text-gray-500 mt-1">
                    resumes in {{ cooldownRemaining(dep.cooldown_until) }}s
                  </p>
                </td>
                <td class="px-6 py-3 text-right text-gray-600">{{ dep.failure_count }}</td>
                <td class="px-6 py-3 text-right text-gray-600">{{ dep.weight }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { api } from '../api/client.js'
import LoadingSpinner from '../components/LoadingSpinner.vue'
import ErrorAlert from '../components/ErrorAlert.vue'
import StatusBadge from '../components/StatusBadge.vue'

const pools = ref(null)
const loading = ref(false)
const error = ref('')
const polling = ref(true)
const lastUpdated = ref(null)
const now = ref(Date.now())
let pollTimer = null
let tickTimer = null

const secondsAgo = computed(() => {
  if (!lastUpdated.value) return 0
  return Math.floor((now.value - lastUpdated.value) / 1000)
})

async function load() {
  loading.value = pools.value === null // only show spinner on first load
  error.value = ''
  try {
    const data = await api.status()
    pools.value = data.pools || []
    lastUpdated.value = Date.now()
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

function startPolling() {
  pollTimer = setInterval(() => {
    if (polling.value) load()
  }, 15000)
}

function togglePolling() {
  polling.value = !polling.value
  if (polling.value) load() // immediate fetch on resume
}

function startTick() {
  tickTimer = setInterval(() => {
    now.value = Date.now()
  }, 1000)
}

function cooldownRemaining(isoDate) {
  const until = new Date(isoDate).getTime()
  const remaining = Math.max(0, Math.floor((until - Date.now()) / 1000))
  return remaining
}

onMounted(() => {
  load()
  startPolling()
  startTick()
})

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
  if (tickTimer) clearInterval(tickTimer)
})
</script>
