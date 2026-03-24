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

          <!-- Cost information section -->
          <div class="border-t border-gray-100">
            <div class="px-6 py-3 bg-white flex items-center justify-between">
              <div class="flex items-center gap-2">
                <span class="text-xs font-medium text-gray-500 uppercase tracking-wider">Cost Info</span>
                <span
                  v-if="model.costs?.source"
                  class="text-xs px-1.5 py-0.5 rounded"
                  :class="{
                    'bg-green-100 text-green-700': model.costs.source === 'auto',
                    'bg-blue-100 text-blue-700': model.costs.source === 'override',
                    'bg-purple-100 text-purple-700': model.costs.source === 'custom',
                  }"
                >
                  {{ model.costs.source }}
                </span>
                <span v-else class="text-xs text-gray-400">not mapped</span>
              </div>
              <div class="flex items-center gap-4 text-xs text-gray-600">
                <span v-if="model.costs?.input_cost_per_token">
                  In: {{ formatCostPerToken(model.costs.input_cost_per_token) }}
                </span>
                <span v-if="model.costs?.output_cost_per_token">
                  Out: {{ formatCostPerToken(model.costs.output_cost_per_token) }}
                </span>
                <span v-if="model.costs?.max_tokens">
                  Max: {{ model.costs.max_tokens.toLocaleString() }} tok
                </span>
                <button
                  class="btn-secondary text-xs"
                  @click="toggleCostEditor(model.model_name)"
                >
                  {{ openCostEditors.has(model.model_name) ? 'Hide' : 'Edit' }}
                </button>
              </div>
            </div>

            <!-- Cost editor (collapsible) -->
            <div v-if="openCostEditors.has(model.model_name)" class="px-6 pb-4 bg-gray-50 border-t border-gray-100">
              <!-- Tab switcher -->
              <div class="flex gap-2 mt-3 mb-4 border-b border-gray-200">
                <button
                  class="px-3 py-1.5 text-xs font-medium border-b-2 -mb-px"
                  :class="activeCostTab(model.model_name) === 'key'
                    ? 'border-indigo-500 text-indigo-600'
                    : 'border-transparent text-gray-500 hover:text-gray-700'"
                  @click="setCostTab(model.model_name, 'key')"
                >
                  Cost Map Key
                </button>
                <button
                  class="px-3 py-1.5 text-xs font-medium border-b-2 -mb-px"
                  :class="activeCostTab(model.model_name) === 'custom'
                    ? 'border-indigo-500 text-indigo-600'
                    : 'border-transparent text-gray-500 hover:text-gray-700'"
                  @click="setCostTab(model.model_name, 'custom')"
                >
                  Custom Costs
                </button>
              </div>

              <!-- Cost map key tab -->
              <div v-if="activeCostTab(model.model_name) === 'key'" class="space-y-2">
                <p class="text-xs text-gray-500">
                  Override which LiteLLM cost map entry is used for this model (e.g. <code class="font-mono">openai/gpt-4</code>).
                </p>
                <div class="flex gap-2">
                  <input
                    :value="costKeyInputs[model.model_name] ?? model.costs?.cost_map_key ?? ''"
                    class="flex-1 text-xs border border-gray-300 rounded px-2 py-1.5 font-mono"
                    placeholder="e.g. openai/gpt-4"
                    @input="costKeyInputs[model.model_name] = $event.target.value"
                  />
                  <button
                    class="btn-primary text-xs"
                    :disabled="saving[model.model_name]"
                    @click="saveCostMapKey(model.model_name)"
                  >
                    {{ saving[model.model_name] ? 'Saving…' : 'Save' }}
                  </button>
                </div>
                <p v-if="saveErrors[model.model_name]" class="text-xs text-red-600">{{ saveErrors[model.model_name] }}</p>
              </div>

              <!-- Custom costs tab -->
              <div v-if="activeCostTab(model.model_name) === 'custom'" class="space-y-3">
                <p class="text-xs text-gray-500">
                  Define fully custom cost values for this model, bypassing the cost map.
                </p>
                <div class="grid grid-cols-2 gap-3 sm:grid-cols-3">
                  <label class="block">
                    <span class="text-xs text-gray-600">Input cost / token</span>
                    <input
                      type="number" step="any" min="0"
                      :value="customCostInputs[model.model_name]?.input_cost_per_token ?? model.costs?.input_cost_per_token ?? ''"
                      class="mt-0.5 w-full text-xs border border-gray-300 rounded px-2 py-1.5"
                      @input="setCustomField(model.model_name, 'input_cost_per_token', parseFloat($event.target.value))"
                    />
                  </label>
                  <label class="block">
                    <span class="text-xs text-gray-600">Output cost / token</span>
                    <input
                      type="number" step="any" min="0"
                      :value="customCostInputs[model.model_name]?.output_cost_per_token ?? model.costs?.output_cost_per_token ?? ''"
                      class="mt-0.5 w-full text-xs border border-gray-300 rounded px-2 py-1.5"
                      @input="setCustomField(model.model_name, 'output_cost_per_token', parseFloat($event.target.value))"
                    />
                  </label>
                  <label class="block">
                    <span class="text-xs text-gray-600">Max tokens</span>
                    <input
                      type="number" step="1" min="0"
                      :value="customCostInputs[model.model_name]?.max_tokens ?? model.costs?.max_tokens ?? ''"
                      class="mt-0.5 w-full text-xs border border-gray-300 rounded px-2 py-1.5"
                      @input="setCustomField(model.model_name, 'max_tokens', parseInt($event.target.value))"
                    />
                  </label>
                  <label class="block">
                    <span class="text-xs text-gray-600">Max input tokens</span>
                    <input
                      type="number" step="1" min="0"
                      :value="customCostInputs[model.model_name]?.max_input_tokens ?? model.costs?.max_input_tokens ?? ''"
                      class="mt-0.5 w-full text-xs border border-gray-300 rounded px-2 py-1.5"
                      @input="setCustomField(model.model_name, 'max_input_tokens', parseInt($event.target.value))"
                    />
                  </label>
                  <label class="block">
                    <span class="text-xs text-gray-600">Max output tokens</span>
                    <input
                      type="number" step="1" min="0"
                      :value="customCostInputs[model.model_name]?.max_output_tokens ?? model.costs?.max_output_tokens ?? ''"
                      class="mt-0.5 w-full text-xs border border-gray-300 rounded px-2 py-1.5"
                      @input="setCustomField(model.model_name, 'max_output_tokens', parseInt($event.target.value))"
                    />
                  </label>
                </div>
                <div class="flex gap-2 items-center">
                  <button
                    class="btn-primary text-xs"
                    :disabled="saving[model.model_name]"
                    @click="saveCustomCosts(model.model_name)"
                  >
                    {{ saving[model.model_name] ? 'Saving…' : 'Save custom costs' }}
                  </button>
                </div>
                <p v-if="saveErrors[model.model_name]" class="text-xs text-red-600">{{ saveErrors[model.model_name] }}</p>
              </div>
            </div>
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
const openCostEditors = reactive(new Set())
const costTabs = reactive({})       // modelName → 'key' | 'custom'
const costKeyInputs = reactive({})  // modelName → string
const customCostInputs = reactive({}) // modelName → partial ModelSpec
const saving = reactive({})         // modelName → bool
const saveErrors = reactive({})     // modelName → string

async function load() {
  loading.value = true
  error.value = ''
  try {
    const statusData = await api.status()
    // Fetch cost detail for all models in parallel; ignore individual failures gracefully.
    const details = await Promise.all(
      statusData.models.map((m) =>
        api.modelDetail(m.model_name).catch(() => null)
      )
    )
    data.value = {
      ...statusData,
      models: statusData.models.map((m, i) => ({
        ...m,
        costs: details[i]?.costs ?? null,
      })),
    }
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

function toggleCostEditor(modelName) {
  if (openCostEditors.has(modelName)) {
    openCostEditors.delete(modelName)
  } else {
    openCostEditors.add(modelName)
    if (!costTabs[modelName]) costTabs[modelName] = 'key'
  }
}

function activeCostTab(modelName) {
  return costTabs[modelName] || 'key'
}

function setCostTab(modelName, tab) {
  costTabs[modelName] = tab
}

function setCustomField(modelName, field, value) {
  if (!customCostInputs[modelName]) customCostInputs[modelName] = {}
  customCostInputs[modelName][field] = value
}

async function saveCostMapKey(modelName) {
  const key = costKeyInputs[modelName] ?? ''
  if (!key.trim()) {
    saveErrors[modelName] = 'Cost map key must not be empty'
    return
  }
  saving[modelName] = true
  saveErrors[modelName] = ''
  try {
    await api.patchModelCostMapKey(modelName, key.trim())
    await load()
    openCostEditors.delete(modelName)
  } catch (e) {
    saveErrors[modelName] = e.message
  } finally {
    saving[modelName] = false
  }
}

async function saveCustomCosts(modelName) {
  const fields = customCostInputs[modelName] || {}
  saving[modelName] = true
  saveErrors[modelName] = ''
  try {
    await api.patchModelCosts(modelName, fields)
    await load()
    openCostEditors.delete(modelName)
  } catch (e) {
    saveErrors[modelName] = e.message
  } finally {
    saving[modelName] = false
  }
}

function formatTime(iso) {
  return new Date(iso).toLocaleTimeString()
}

function formatCostPerToken(val) {
  if (!val) return '—'
  return `$${(val * 1_000_000).toFixed(4)}/MTok`
}
</script>
