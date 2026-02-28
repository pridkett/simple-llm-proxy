<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold text-gray-900">Models</h1>
      <button class="btn-secondary text-xs" :disabled="loading" @click="load">Refresh</button>
    </div>

    <LoadingSpinner v-if="loading" />
    <ErrorAlert v-else-if="error" title="Failed to load models" :message="error" />

    <template v-else-if="data">
      <div class="space-y-4">
        <div
          v-for="model in data.models"
          :key="model.model_name"
          class="card overflow-hidden"
        >
          <!-- Model header -->
          <div class="px-6 py-4 bg-gray-50 border-b border-gray-100 flex items-center justify-between">
            <div>
              <h2 class="text-base font-semibold text-gray-900">{{ model.model_name }}</h2>
              <p class="text-xs text-gray-500 mt-0.5">
                {{ model.total_deployments }} deployment{{ model.total_deployments !== 1 ? 's' : '' }}
              </p>
            </div>
            <div class="flex items-center gap-3">
              <StatusBadge
                :status="model.healthy_deployments > 0 ? 'healthy' : 'cooldown'"
              />
              <button
                class="btn-secondary text-xs"
                @click="togglePlayground(model.model_name)"
              >
                {{ openPlaygrounds.has(model.model_name) ? 'Hide test' : 'Test' }}
              </button>
            </div>
          </div>

          <!-- Deployments table -->
          <div class="overflow-x-auto">
            <table class="min-w-full divide-y divide-gray-100 text-sm">
              <thead class="bg-white">
                <tr>
                  <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Provider</th>
                  <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Model</th>
                  <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Status</th>
                  <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Failures</th>
                  <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">RPM</th>
                  <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">TPM</th>
                </tr>
              </thead>
              <tbody class="bg-white divide-y divide-gray-50">
                <tr v-for="(dep, i) in model.deployments" :key="i">
                  <td class="px-6 py-3 font-medium capitalize">{{ dep.provider }}</td>
                  <td class="px-6 py-3 text-gray-600 font-mono text-xs">{{ dep.actual_model }}</td>
                  <td class="px-6 py-3">
                    <StatusBadge :status="dep.status" />
                    <span v-if="dep.cooldown_until" class="ml-2 text-xs text-gray-400">
                      until {{ formatTime(dep.cooldown_until) }}
                    </span>
                  </td>
                  <td class="px-6 py-3 text-gray-600">{{ dep.failure_count }}</td>
                  <td class="px-6 py-3 text-gray-600">{{ dep.rpm || '—' }}</td>
                  <td class="px-6 py-3 text-gray-600">{{ dep.tpm || '—' }}</td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Inline playground (toggleable) -->
          <ModelPlayground
            v-if="openPlaygrounds.has(model.model_name)"
            :model-name="model.model_name"
          />
        </div>

        <div v-if="!data.models?.length" class="card px-6 py-12 text-center text-sm text-gray-500">
          No models configured. Add models to your config.yaml.
        </div>
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { api } from '../api/client.js'
import LoadingSpinner from '../components/LoadingSpinner.vue'
import ErrorAlert from '../components/ErrorAlert.vue'
import StatusBadge from '../components/StatusBadge.vue'
import ModelPlayground from '../components/ModelPlayground.vue'

const data = ref(null)
const loading = ref(false)
const error = ref('')
const openPlaygrounds = reactive(new Set())

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

function togglePlayground(modelName) {
  if (openPlaygrounds.has(modelName)) {
    openPlaygrounds.delete(modelName)
  } else {
    openPlaygrounds.add(modelName)
  }
}

function formatTime(iso) {
  return new Date(iso).toLocaleTimeString()
}
</script>
