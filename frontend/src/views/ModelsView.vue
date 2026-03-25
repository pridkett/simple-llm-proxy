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
              <StatusBadge :status="model.healthy_deployments > 0 ? 'healthy' : 'cooldown'" />
              <button class="btn-secondary text-xs" @click="togglePlayground(model.model_name)">
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
                >{{ model.costs.source }}</span>
                <span v-else class="text-xs text-gray-400">not mapped</span>
              </div>
              <div class="flex items-center gap-4 text-xs text-gray-600">
                <span v-if="model.costs?.input_cost_per_token">In: {{ formatCostPerToken(model.costs.input_cost_per_token) }}</span>
                <span v-if="model.costs?.output_cost_per_token">Out: {{ formatCostPerToken(model.costs.output_cost_per_token) }}</span>
                <span v-if="model.costs?.max_tokens">Max: {{ model.costs.max_tokens.toLocaleString() }} tok</span>
                <button
                  v-if="model.costs?.source && model.costs.source !== 'auto'"
                  class="btn-secondary text-xs text-red-600 hover:text-red-700"
                  :disabled="clearing[model.model_name]"
                  @click="clearCostOverride(model.model_name)"
                >{{ clearing[model.model_name] ? 'Clearing…' : 'Clear' }}</button>
                <button class="btn-secondary text-xs" @click="toggleCostEditor(model.model_name)">
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
                  :class="activeCostTab(model.model_name) === 'key' ? 'border-indigo-500 text-indigo-600' : 'border-transparent text-gray-500 hover:text-gray-700'"
                  @click="setCostTab(model.model_name, 'key')"
                >Cost Map Key</button>
                <button
                  class="px-3 py-1.5 text-xs font-medium border-b-2 -mb-px"
                  :class="activeCostTab(model.model_name) === 'custom' ? 'border-indigo-500 text-indigo-600' : 'border-transparent text-gray-500 hover:text-gray-700'"
                  @click="setCostTab(model.model_name, 'custom')"
                >Custom Costs</button>
              </div>

              <!-- Cost map key tab — autocomplete combobox -->
              <div v-if="activeCostTab(model.model_name) === 'key'" class="space-y-2">
                <p class="text-xs text-gray-500">
                  Select the LiteLLM cost map entry for this model. Type to filter.
                </p>
                <div class="flex gap-2">
                  <input
                    :value="costKeyInputs[model.model_name] ?? model.costs?.cost_map_key ?? ''"
                    class="flex-1 text-xs border border-gray-300 rounded px-2 py-1.5 font-mono"
                    placeholder="e.g. openai/gpt-4"
                    autocomplete="off"
                    @input="onCostKeyInput(model.model_name, $event.target.value, $event)"
                    @focus="openDropdown(model.model_name, $event)"
                    @blur="scheduleCloseDropdown(model.model_name)"
                  />
                  <button
                    class="btn-primary text-xs"
                    :disabled="saving[model.model_name]"
                    @click="saveCostMapKey(model.model_name)"
                  >{{ saving[model.model_name] ? 'Saving…' : 'Save' }}</button>
                </div>
                <!-- Dropdown teleported to <body> to escape card's overflow:hidden -->
                <Teleport to="body">
                  <div
                    v-if="showDropdowns[model.model_name] && filteredCostMapModels(model.model_name).length"
                    class="fixed z-50 bg-white border border-gray-200 rounded shadow-xl overflow-y-auto"
                    :style="{
                      top: (dropdownPos[model.model_name]?.top ?? 0) + 'px',
                      left: (dropdownPos[model.model_name]?.left ?? 0) + 'px',
                      width: (dropdownPos[model.model_name]?.width ?? 300) + 'px',
                      maxHeight: '240px',
                    }"
                  >
                    <button
                      v-for="entry in filteredCostMapModels(model.model_name)"
                      :key="entry.name"
                      type="button"
                      class="w-full text-left px-3 py-2.5 hover:bg-indigo-50 border-b border-gray-100 last:border-0"
                      @mousedown.prevent="selectCostMapKey(model.model_name, entry)"
                    >
                      <div class="font-mono text-xs text-gray-900 truncate">{{ entry.name }}</div>
                      <div class="text-xs text-gray-400 mt-0.5 flex gap-3">
                        <span v-if="entry.input_cost_per_token">In: {{ formatCostPerToken(entry.input_cost_per_token) }}</span>
                        <span v-if="entry.output_cost_per_token">Out: {{ formatCostPerToken(entry.output_cost_per_token) }}</span>
                        <span v-if="entry.max_tokens">{{ entry.max_tokens.toLocaleString() }} tok</span>
                      </div>
                    </button>
                  </div>
                </Teleport>
                <p v-if="saveErrors[model.model_name]" class="text-xs text-red-600">{{ saveErrors[model.model_name] }}</p>
              </div>

              <!-- Custom costs tab — inputs in $/MTok -->
              <div v-if="activeCostTab(model.model_name) === 'custom'" class="space-y-3">
                <p class="text-xs text-gray-500">
                  Define fully custom cost values, bypassing the cost map.
                  Cost fields are in <strong>$ per million tokens ($/MTok)</strong>.
                </p>
                <div class="grid grid-cols-2 gap-3 sm:grid-cols-3">
                  <label class="block">
                    <span class="text-xs text-gray-600">Input cost ($/MTok)</span>
                    <input
                      type="number" step="0.0001" min="0"
                      :value="getCustomMTok(model.model_name, 'input_cost_per_token', model.costs)"
                      class="mt-0.5 w-full text-xs border border-gray-300 rounded px-2 py-1.5"
                      @input="setCustomMTokField(model.model_name, 'input_cost_per_token', $event.target.value)"
                    />
                  </label>
                  <label class="block">
                    <span class="text-xs text-gray-600">Output cost ($/MTok)</span>
                    <input
                      type="number" step="0.0001" min="0"
                      :value="getCustomMTok(model.model_name, 'output_cost_per_token', model.costs)"
                      class="mt-0.5 w-full text-xs border border-gray-300 rounded px-2 py-1.5"
                      @input="setCustomMTokField(model.model_name, 'output_cost_per_token', $event.target.value)"
                    />
                  </label>
                  <label class="block">
                    <span class="text-xs text-gray-600">Cache read ($/MTok)</span>
                    <input
                      type="number" step="0.0001" min="0"
                      :value="getCustomMTok(model.model_name, 'cache_read_input_token_cost', model.costs)"
                      class="mt-0.5 w-full text-xs border border-gray-300 rounded px-2 py-1.5"
                      @input="setCustomMTokField(model.model_name, 'cache_read_input_token_cost', $event.target.value)"
                    />
                  </label>
                  <label class="block">
                    <span class="text-xs text-gray-600">Cache write ($/MTok)</span>
                    <input
                      type="number" step="0.0001" min="0"
                      :value="getCustomMTok(model.model_name, 'cache_creation_input_token_cost', model.costs)"
                      class="mt-0.5 w-full text-xs border border-gray-300 rounded px-2 py-1.5"
                      @input="setCustomMTokField(model.model_name, 'cache_creation_input_token_cost', $event.target.value)"
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
                    @click="saveCustomCosts(model.model_name, model.costs)"
                  >{{ saving[model.model_name] ? 'Saving…' : 'Save custom costs' }}</button>
                </div>
                <p v-if="saveErrors[model.model_name]" class="text-xs text-red-600">{{ saveErrors[model.model_name] }}</p>
              </div>
            </div>
          </div>

          <!-- Inline playground (toggleable) -->
          <ModelPlayground v-if="openPlaygrounds.has(model.model_name)" :model-name="model.model_name" />
        </div>

        <div v-if="!data.models?.length" class="card px-6 py-12 text-center text-sm text-gray-500">
          No models configured. Add models to your config.yaml.
        </div>
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onUnmounted } from 'vue'
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
const costTabs = reactive({})
const costKeyInputs = reactive({})
const customCostInputs = reactive({})
const saving = reactive({})
const clearing = reactive({})
const saveErrors = reactive({})
const showDropdowns = reactive({})
const dropdownPos = reactive({}) // modelName → {top, left, width} in viewport px
const activeScrollModelName = ref(null) // string | null — which dropdown is tracking scroll
const activeScrollEl = ref(null)         // HTMLElement | null — the tracked input element

// Cost map model list for autocomplete — loaded once on first editor open.
const costMapModelList = ref([])
const costMapModelsLoaded = ref(false)

async function loadCostMapModels() {
  if (costMapModelsLoaded.value) return
  try {
    const models = await api.costMapModels()
    costMapModelList.value = models
    costMapModelsLoaded.value = true
  } catch {
    // Non-fatal: autocomplete simply won't show suggestions.
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const statusData = await api.status()
    const details = await Promise.all(
      statusData.models.map((m) => api.modelDetail(m.model_name).catch(() => null))
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
onUnmounted(() => { detachScrollListener() })

function togglePlayground(modelName) {
  if (openPlaygrounds.has(modelName)) openPlaygrounds.delete(modelName)
  else openPlaygrounds.add(modelName)
}

function toggleCostEditor(modelName) {
  if (openCostEditors.has(modelName)) {
    openCostEditors.delete(modelName)
    scheduleCloseDropdown(modelName)
  } else {
    openCostEditors.add(modelName)
    if (!costTabs[modelName]) costTabs[modelName] = 'key'
    loadCostMapModels()
  }
}

function activeCostTab(modelName) { return costTabs[modelName] || 'key' }
function setCostTab(modelName, tab) { costTabs[modelName] = tab }

// --- Autocomplete helpers ---

function handleWindowScroll() {
  if (activeScrollModelName.value && activeScrollEl.value) {
    updateDropdownPos(activeScrollModelName.value, activeScrollEl.value)
  }
}

function attachScrollListener(modelName, el) {
  detachScrollListener()
  activeScrollModelName.value = modelName
  activeScrollEl.value = el
  // capture:true catches scroll on inner containers (table uses overflow-x-auto)
  window.addEventListener('scroll', handleWindowScroll, { passive: true, capture: true })
}

function detachScrollListener() {
  window.removeEventListener('scroll', handleWindowScroll, { passive: true, capture: true })
  activeScrollModelName.value = null
  activeScrollEl.value = null
}

function updateDropdownPos(modelName, el) {
  const rect = el.getBoundingClientRect()
  dropdownPos[modelName] = {
    top: rect.bottom + 4,  // 4px gap below the input; fixed = viewport-relative
    left: rect.left,
    width: rect.width,
  }
}

function onCostKeyInput(modelName, value, event) {
  costKeyInputs[modelName] = value
  showDropdowns[modelName] = true
  updateDropdownPos(modelName, event.target)
  if (activeScrollModelName.value !== modelName) {
    attachScrollListener(modelName, event.target)
  }
}

function openDropdown(modelName, event) {
  showDropdowns[modelName] = true
  updateDropdownPos(modelName, event.target)
  attachScrollListener(modelName, event.target)
}

function scheduleCloseDropdown(modelName) {
  // Delay allows mousedown on a dropdown item to fire before the dropdown closes.
  setTimeout(() => {
    showDropdowns[modelName] = false
    if (activeScrollModelName.value === modelName) {
      detachScrollListener()
    }
  }, 150)
}

function selectCostMapKey(modelName, entry) {
  costKeyInputs[modelName] = entry.name
  showDropdowns[modelName] = false
  if (activeScrollModelName.value === modelName) {
    detachScrollListener()
  }
  // Pre-populate custom cost fields with the selected entry's values so the user
  // can use them as a starting point if they switch to the custom tab.
  if (!customCostInputs[modelName]) customCostInputs[modelName] = {}
  const mtokFields = ['input_cost_per_token', 'output_cost_per_token',
                       'cache_read_input_token_cost', 'cache_creation_input_token_cost']
  for (const f of mtokFields) {
    if (entry[f]) customCostInputs[modelName][f] = Math.round(entry[f] * PER_MTOK * 10000) / 10000
  }
  for (const f of ['max_tokens', 'max_input_tokens', 'max_output_tokens']) {
    if (entry[f]) customCostInputs[modelName][f] = entry[f]
  }
}

function filteredCostMapModels(modelName) {
  const query = (costKeyInputs[modelName] ?? '').toLowerCase()
  const list = costMapModelList.value
  if (!query) return list.slice(0, 10)
  const prefix = []
  const rest = []
  for (const e of list) {
    const name = e.name.toLowerCase()
    if (name.startsWith(query)) prefix.push(e)
    else if (name.includes(query)) rest.push(e)
  }
  return [...prefix, ...rest].slice(0, 100)
}

// --- Custom costs: per-MTok display helpers ---
// The API stores costs as per-token (e.g. 0.00003).
// We display and accept input in $/MTok (e.g. 30).

const PER_MTOK = 1_000_000

function getCustomMTok(modelName, field, costs) {
  // User-edited value takes precedence over the saved cost data.
  const edited = customCostInputs[modelName]?.[field]
  if (edited !== undefined) return edited
  const saved = costs?.[field]
  if (!saved) return ''
  // Convert per-token → per-MTok for display.
  const mtok = saved * PER_MTOK
  // Round to 4 decimal places to avoid floating-point noise.
  return Math.round(mtok * 10000) / 10000
}

function setCustomMTokField(modelName, field, displayValue) {
  if (!customCostInputs[modelName]) customCostInputs[modelName] = {}
  // Store the display value ($/MTok) directly so the input stays stable.
  customCostInputs[modelName][field] = displayValue === '' ? '' : parseFloat(displayValue)
}

function setCustomField(modelName, field, value) {
  if (!customCostInputs[modelName]) customCostInputs[modelName] = {}
  customCostInputs[modelName][field] = value
}

async function clearCostOverride(modelName) {
  clearing[modelName] = true
  try {
    await api.deleteModelCosts(modelName)
    await load()
    openCostEditors.delete(modelName)
  } catch (e) {
    saveErrors[modelName] = e.message
  } finally {
    clearing[modelName] = false
  }
}

async function saveCostMapKey(modelName) {
  const key = (costKeyInputs[modelName] ?? '').trim()
  if (!key) {
    saveErrors[modelName] = 'Cost map key must not be empty'
    return
  }
  saving[modelName] = true
  saveErrors[modelName] = ''
  try {
    await api.patchModelCostMapKey(modelName, key)
    await load()
    openCostEditors.delete(modelName)
  } catch (e) {
    saveErrors[modelName] = e.message
  } finally {
    saving[modelName] = false
  }
}

async function saveCustomCosts(modelName, existingCosts) {
  const edits = customCostInputs[modelName] || {}
  const fields = {}
  // MTok fields: edited value (stored as $/MTok) takes priority, else use existing per-token value.
  const mtokFields = ['input_cost_per_token', 'output_cost_per_token',
                       'cache_read_input_token_cost', 'cache_creation_input_token_cost']
  for (const f of mtokFields) {
    const edited = edits[f]
    if (edited !== undefined && edited !== '') {
      fields[f] = parseFloat(edited) / PER_MTOK
    } else if (existingCosts?.[f]) {
      fields[f] = existingCosts[f] // already per-token
    }
  }
  // Integer fields: edited value takes priority, else use existing.
  for (const f of ['max_tokens', 'max_input_tokens', 'max_output_tokens']) {
    const edited = edits[f]
    if (edited !== undefined) {
      fields[f] = edited
    } else if (existingCosts?.[f]) {
      fields[f] = existingCosts[f]
    }
  }
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
  return `$${(val * PER_MTOK).toFixed(4)}/MTok`
}
</script>
