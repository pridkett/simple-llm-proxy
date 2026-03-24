<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
    <div class="flex items-center justify-between mb-6">
      <div>
        <h1 class="text-2xl font-bold text-gray-900">Configuration</h1>
        <p class="mt-1 text-sm text-gray-500">Current proxy configuration. Secrets are redacted.</p>
      </div>
      <div class="flex items-center gap-2">
        <span v-if="reloadSuccess" class="text-xs text-green-700">Config reloaded.</span>
        <span v-if="reloadError" class="text-xs text-red-600">{{ reloadError }}</span>
        <button class="btn-primary text-xs" :disabled="reloading || loading" @click="triggerReload">
          {{ reloading ? 'Reloading…' : 'Reload Config' }}
        </button>
        <button class="btn-secondary text-xs" :disabled="loading" @click="load">Refresh</button>
      </div>
    </div>

    <LoadingSpinner v-if="loading" />
    <ErrorAlert v-else-if="error" title="Failed to load config" :message="error" />

    <template v-else-if="data">
      <!-- General settings -->
      <div class="card mb-4">
        <div class="px-6 py-4 border-b border-gray-100">
          <h2 class="text-base font-semibold text-gray-900">General Settings</h2>
        </div>
        <dl class="px-6 py-4 grid grid-cols-1 sm:grid-cols-3 gap-4">
          <div>
            <dt class="text-xs text-gray-500">Port</dt>
            <dd class="mt-1 text-sm font-medium text-gray-900">{{ data.general_settings.port }}</dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500">Master Key</dt>
            <dd class="mt-1 text-sm font-medium" :class="data.general_settings.master_key_set ? 'text-green-700' : 'text-yellow-700'">
              {{ data.general_settings.master_key_set ? 'Set' : 'Not configured' }}
            </dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500">Database</dt>
            <dd class="mt-1 text-sm font-medium text-gray-900 font-mono text-xs">
              {{ data.general_settings.database_url || '—' }}
            </dd>
          </div>
        </dl>
      </div>

      <!-- Router settings -->
      <div class="card mb-4">
        <div class="px-6 py-4 border-b border-gray-100">
          <h2 class="text-base font-semibold text-gray-900">Router Settings</h2>
        </div>
        <dl class="px-6 py-4 grid grid-cols-2 sm:grid-cols-4 gap-4">
          <div>
            <dt class="text-xs text-gray-500">Strategy</dt>
            <dd class="mt-1 text-sm font-medium text-gray-900">{{ data.router_settings.routing_strategy }}</dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500">Retries</dt>
            <dd class="mt-1 text-sm font-medium text-gray-900">{{ data.router_settings.num_retries }}</dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500">Allowed Fails</dt>
            <dd class="mt-1 text-sm font-medium text-gray-900">{{ data.router_settings.allowed_fails }}</dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500">Cooldown Time</dt>
            <dd class="mt-1 text-sm font-medium text-gray-900">{{ data.router_settings.cooldown_time }}</dd>
          </div>
        </dl>
      </div>

      <!-- Model list -->
      <div class="card">
        <div class="px-6 py-4 border-b border-gray-100">
          <h2 class="text-base font-semibold text-gray-900">Models ({{ data.model_list.length }})</h2>
        </div>
        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-100 text-sm">
            <thead class="bg-gray-50">
              <tr>
                <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Name</th>
                <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Provider</th>
                <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Model</th>
                <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">API Key</th>
                <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">API Base</th>
                <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">RPM</th>
                <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">TPM</th>
              </tr>
            </thead>
            <tbody class="bg-white divide-y divide-gray-50">
              <tr v-for="m in data.model_list" :key="m.model_name">
                <td class="px-6 py-3 font-medium">{{ m.model_name }}</td>
                <td class="px-6 py-3 text-gray-600 capitalize">{{ m.provider }}</td>
                <td class="px-6 py-3 text-gray-600 font-mono text-xs">{{ m.actual_model }}</td>
                <td class="px-6 py-3">
                  <span
                    class="text-xs px-2 py-0.5 rounded font-medium"
                    :class="m.api_key_set ? 'bg-green-50 text-green-700' : 'bg-yellow-50 text-yellow-700'"
                  >
                    {{ m.api_key_set ? 'Set' : 'Missing' }}
                  </span>
                </td>
                <td class="px-6 py-3 text-gray-500 font-mono text-xs">{{ m.api_base || '—' }}</td>
                <td class="px-6 py-3 text-gray-600">{{ m.rpm || '—' }}</td>
                <td class="px-6 py-3 text-gray-600">{{ m.tpm || '—' }}</td>
              </tr>
              <tr v-if="!data.model_list?.length">
                <td colspan="7" class="px-6 py-8 text-center text-gray-500">
                  No models configured.
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>

    <!-- Cost map (always visible, independent of config load state) -->
    <div class="card mt-4">
      <div class="px-6 py-4 border-b border-gray-100 flex items-center justify-between">
        <h2 class="text-base font-semibold text-gray-900">LiteLLM Cost Map</h2>
        <div class="flex items-center gap-2">
          <span v-if="costMapReloadSuccess" class="text-xs text-green-700">Reloaded.</span>
          <span v-if="costMapReloadError" class="text-xs text-red-600">{{ costMapReloadError }}</span>
          <button class="btn-primary text-xs" :disabled="costMapReloading" @click="triggerCostMapReload">
            {{ costMapReloading ? 'Reloading…' : 'Reload Cost Map' }}
          </button>
        </div>
      </div>
      <dl class="px-6 py-4 grid grid-cols-1 sm:grid-cols-3 gap-4">
        <div>
          <dt class="text-xs text-gray-500">Status</dt>
          <dd class="mt-1 text-sm font-medium" :class="costMapData?.loaded ? 'text-green-700' : 'text-yellow-700'">
            {{ costMapData?.loaded ? 'Loaded' : 'Not loaded' }}
          </dd>
        </div>
        <div>
          <dt class="text-xs text-gray-500">Models</dt>
          <dd class="mt-1 text-sm font-medium text-gray-900">
            {{ costMapData?.model_count ?? '—' }}
          </dd>
        </div>
        <div>
          <dt class="text-xs text-gray-500">Last Loaded</dt>
          <dd class="mt-1 text-sm font-medium text-gray-900 font-mono text-xs">
            {{ costMapData?.loaded_at ? new Date(costMapData.loaded_at).toLocaleString() : '—' }}
          </dd>
        </div>
        <div class="sm:col-span-3">
          <dt class="text-xs text-gray-500 mb-1">Source URL</dt>
          <div class="flex gap-2 items-center">
            <input
              v-model="costMapURL"
              type="text"
              class="flex-1 text-xs font-mono border border-gray-300 rounded px-2 py-1 focus:outline-none focus:ring-1 focus:ring-blue-400"
              placeholder="https://..."
            />
            <button class="btn-secondary text-xs" :disabled="costMapURLSaving" @click="saveCostMapURL">
              {{ costMapURLSaving ? 'Saving…' : 'Update URL' }}
            </button>
          </div>
          <p v-if="costMapURLError" class="text-xs text-red-600 mt-1">{{ costMapURLError }}</p>
          <p v-if="costMapURLSuccess" class="text-xs text-green-700 mt-1">URL updated.</p>
        </div>
      </dl>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api/client.js'
import LoadingSpinner from '../components/LoadingSpinner.vue'
import ErrorAlert from '../components/ErrorAlert.vue'

const data = ref(null)
const loading = ref(false)
const error = ref('')
const reloading = ref(false)
const reloadSuccess = ref(false)
const reloadError = ref('')

// Cost map state
const costMapData = ref(null)
const costMapURL = ref('')
const costMapReloading = ref(false)
const costMapReloadSuccess = ref(false)
const costMapReloadError = ref('')
const costMapURLSaving = ref(false)
const costMapURLSuccess = ref(false)
const costMapURLError = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    data.value = await api.config()
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

async function triggerReload() {
  reloading.value = true
  reloadSuccess.value = false
  reloadError.value = ''
  try {
    await api.reload()
    reloadSuccess.value = true
    setTimeout(() => { reloadSuccess.value = false }, 3000)
    await load()
  } catch (e) {
    reloadError.value = e.message
    setTimeout(() => { reloadError.value = '' }, 5000)
  } finally {
    reloading.value = false
  }
}

async function loadCostMapStatus() {
  try {
    costMapData.value = await api.costMapStatus()
    costMapURL.value = costMapData.value?.url ?? ''
  } catch {
    // Non-fatal: cost map section shows "Not loaded" on error
  }
}

async function triggerCostMapReload() {
  costMapReloading.value = true
  costMapReloadSuccess.value = false
  costMapReloadError.value = ''
  try {
    await api.costMapReload()
    costMapReloadSuccess.value = true
    setTimeout(() => { costMapReloadSuccess.value = false }, 3000)
    await loadCostMapStatus()
  } catch (e) {
    costMapReloadError.value = e.message
    setTimeout(() => { costMapReloadError.value = '' }, 5000)
  } finally {
    costMapReloading.value = false
  }
}

async function saveCostMapURL() {
  costMapURLSaving.value = true
  costMapURLSuccess.value = false
  costMapURLError.value = ''
  try {
    await api.costMapSetURL(costMapURL.value)
    costMapURLSuccess.value = true
    setTimeout(() => { costMapURLSuccess.value = false }, 3000)
  } catch (e) {
    costMapURLError.value = e.message
  } finally {
    costMapURLSaving.value = false
  }
}

onMounted(() => {
  load()
  loadCostMapStatus()
})
</script>
