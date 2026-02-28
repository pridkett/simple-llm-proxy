<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
    <h1 class="text-2xl font-bold text-gray-900 mb-6">Dashboard</h1>

    <LoadingSpinner v-if="loading" />

    <ErrorAlert
      v-else-if="error"
      title="Failed to load status"
      :message="error"
    />

    <template v-else-if="data">
      <!-- Summary cards -->
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
        <div class="card p-5">
          <p class="text-sm text-gray-500 font-medium">Proxy Status</p>
          <div class="mt-2 flex items-center gap-2">
            <span class="w-2.5 h-2.5 rounded-full bg-green-500" />
            <span class="text-xl font-semibold text-gray-900 capitalize">{{ data.status }}</span>
          </div>
        </div>

        <div class="card p-5">
          <p class="text-sm text-gray-500 font-medium">Uptime</p>
          <p class="mt-2 text-xl font-semibold text-gray-900">{{ uptime }}</p>
        </div>

        <div class="card p-5">
          <p class="text-sm text-gray-500 font-medium">Models</p>
          <p class="mt-2 text-xl font-semibold text-gray-900">{{ data.models?.length ?? 0 }}</p>
        </div>

        <div class="card p-5">
          <p class="text-sm text-gray-500 font-medium">In Cooldown</p>
          <p class="mt-2 text-xl font-semibold" :class="cooldownCount > 0 ? 'text-red-600' : 'text-gray-900'">
            {{ cooldownCount }}
          </p>
        </div>
      </div>

      <!-- Models overview -->
      <div class="card">
        <div class="px-6 py-4 border-b border-gray-100">
          <h2 class="text-base font-semibold text-gray-900">Model Deployments</h2>
        </div>
        <div class="divide-y divide-gray-50">
          <div
            v-for="model in data.models"
            :key="model.model_name"
            class="px-6 py-4 flex items-center justify-between"
          >
            <div>
              <p class="text-sm font-medium text-gray-900">{{ model.model_name }}</p>
              <p class="text-xs text-gray-500 mt-0.5">
                {{ model.healthy_deployments }} / {{ model.total_deployments }} deployments healthy
              </p>
            </div>
            <StatusBadge
              :status="model.healthy_deployments > 0 ? 'healthy' : 'cooldown'"
            />
          </div>
          <div v-if="!data.models?.length" class="px-6 py-8 text-center text-sm text-gray-500">
            No models configured.
          </div>
        </div>
      </div>

      <!-- Router settings -->
      <div class="card mt-4">
        <div class="px-6 py-4 border-b border-gray-100">
          <h2 class="text-base font-semibold text-gray-900">Router Settings</h2>
        </div>
        <dl class="grid grid-cols-2 sm:grid-cols-4 gap-px bg-gray-100">
          <div v-for="item in routerInfo" :key="item.label" class="bg-white px-6 py-4">
            <dt class="text-xs text-gray-500">{{ item.label }}</dt>
            <dd class="mt-1 text-sm font-medium text-gray-900">{{ item.value }}</dd>
          </div>
        </dl>
      </div>
    </template>

    <button
      class="mt-4 btn-secondary text-xs"
      :disabled="loading"
      @click="load"
    >
      Refresh
    </button>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { api } from '../api/client.js'
import LoadingSpinner from '../components/LoadingSpinner.vue'
import ErrorAlert from '../components/ErrorAlert.vue'
import StatusBadge from '../components/StatusBadge.vue'

const data = ref(null)
const loading = ref(false)
const error = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    data.value = await api.status()
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

onMounted(load)

const uptime = computed(() => {
  if (!data.value) return '-'
  const s = data.value.uptime_seconds
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m ${s % 60}s`
  return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`
})

const cooldownCount = computed(() => {
  if (!data.value?.models) return 0
  return data.value.models.reduce((acc, m) => {
    return acc + (m.total_deployments - m.healthy_deployments)
  }, 0)
})

const routerInfo = computed(() => {
  if (!data.value?.router_settings) return []
  const s = data.value.router_settings
  return [
    { label: 'Strategy', value: s.routing_strategy },
    { label: 'Retries', value: s.num_retries },
    { label: 'Allowed Fails', value: s.allowed_fails },
    { label: 'Cooldown Time', value: s.cooldown_time },
  ]
})
</script>
