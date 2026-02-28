<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
    <div class="flex items-center justify-between mb-6">
      <div>
        <h1 class="text-2xl font-bold text-gray-900">Configuration</h1>
        <p class="mt-1 text-sm text-gray-500">Current proxy configuration. Secrets are redacted.</p>
      </div>
      <button class="btn-secondary text-xs" :disabled="loading" @click="load">Refresh</button>
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

onMounted(load)
</script>
