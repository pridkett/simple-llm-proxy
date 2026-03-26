<template>
  <div class="p-6">
    <h1 class="text-2xl font-semibold text-gray-900 mb-6">API Keys</h1>

    <div class="flex gap-4">
      <!-- Left panel: Team list -->
      <div class="w-56 flex-shrink-0 bg-white border border-gray-200 rounded-lg overflow-hidden self-start">
        <div class="px-3 py-2 text-xs font-semibold text-gray-400 uppercase tracking-wide border-b border-gray-200 bg-gray-50">
          Teams
        </div>
        <div v-if="loadingTeams" class="px-3 py-3 text-gray-500 text-sm">Loading...</div>
        <div v-else-if="teamsError" class="px-3 py-3 text-red-600 text-sm">{{ teamsError }}</div>
        <ul v-else>
          <li
            v-for="team in teams"
            :key="team.id"
            class="px-3 py-2 cursor-pointer text-sm border-b border-gray-100 last:border-0"
            :class="selectedTeam?.id === team.id ? 'bg-indigo-50 text-indigo-700 font-medium' : 'text-gray-700 hover:bg-gray-50'"
            @click="selectTeam(team)"
          >
            {{ team.name }}
          </li>
          <li v-if="teams.length === 0" class="px-3 py-3 text-sm text-gray-400 italic">
            No teams found
          </li>
        </ul>
      </div>

      <!-- Middle panel: App list -->
      <div class="w-56 flex-shrink-0 bg-white border border-gray-200 rounded-lg overflow-hidden self-start">
        <div class="px-3 py-2 text-xs font-semibold text-gray-400 uppercase tracking-wide border-b border-gray-200 bg-gray-50">
          Applications
        </div>
        <template v-if="selectedTeam">
          <div v-if="loadingApps" class="px-3 py-3 text-gray-500 text-sm">Loading...</div>
          <div v-else-if="appsError" class="px-3 py-3 text-red-600 text-sm">{{ appsError }}</div>
          <ul v-else>
            <li
              v-for="app in apps"
              :key="app.id"
              class="px-3 py-2 cursor-pointer text-sm border-b border-gray-100 last:border-0"
              :class="selectedApp?.id === app.id ? 'bg-indigo-50 text-indigo-700 font-medium' : 'text-gray-700 hover:bg-gray-50'"
              @click="selectApp(app)"
            >
              {{ app.name }}
            </li>
            <li v-if="apps.length === 0 && !loadingApps" class="px-3 py-3 text-sm text-gray-400 italic">
              No applications found
            </li>
          </ul>
        </template>
        <div v-else class="px-3 py-3 text-sm text-gray-400 italic">
          Select a team first
        </div>
      </div>

      <!-- Right panel: Keys table + Create form -->
      <div class="flex-1 min-w-0">
        <template v-if="selectedApp">
          <!-- Error banner -->
          <div v-if="error" class="mb-4 px-3 py-2 bg-red-50 border border-red-200 rounded text-sm text-red-600 flex justify-between items-center">
            <span>{{ error }}</span>
            <button @click="error = null" class="ml-3 text-red-400 hover:text-red-600">&#10005;</button>
          </div>

          <!-- Keys table -->
          <div class="bg-white border border-gray-200 rounded-lg overflow-hidden mb-4">
            <div class="px-4 py-2 text-xs font-semibold text-gray-400 uppercase tracking-wide border-b border-gray-200 bg-gray-50">
              Keys — {{ selectedApp.name }}
            </div>
            <div v-if="loadingKeys" class="px-4 py-3 text-gray-500 text-sm">Loading keys...</div>
            <table v-else class="min-w-full divide-y divide-gray-200">
              <thead class="bg-gray-50">
                <tr>
                  <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Prefix</th>
                  <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Name</th>
                  <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Models</th>
                  <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Budget</th>
                  <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
                  <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Actions</th>
                </tr>
              </thead>
              <tbody class="bg-white divide-y divide-gray-200">
                <tr v-if="keys.length === 0">
                  <td colspan="6" class="px-4 py-8 text-center text-sm text-gray-400 italic">
                    No keys yet — create the first one below
                  </td>
                </tr>
                <template v-for="key in keys" :key="key.id">
                  <tr>
                    <td class="px-4 py-3 font-mono text-sm text-gray-900">{{ key.key_prefix }}...</td>
                    <td class="px-4 py-3 text-sm font-medium text-gray-900">{{ key.name }}</td>
                    <td class="px-4 py-3 text-sm text-gray-500">
                      <template v-if="editingModelsKeyId !== key.id">
                        <span v-if="!key.allowed_models || key.allowed_models.length === 0" class="italic">All models</span>
                        <span v-else>
                          {{ key.allowed_models.slice(0, 2).join(', ') }}
                          <span v-if="key.allowed_models.length > 2" class="text-gray-400"> +{{ key.allowed_models.length - 2 }} more</span>
                        </span>
                        <button
                          v-if="key.is_active"
                          @click="startEditModels(key)"
                          class="ml-2 text-xs text-indigo-500 hover:text-indigo-700"
                        >Edit</button>
                      </template>
                    </td>
                    <td class="px-4 py-3 text-sm text-gray-700">
                      <!-- Phase 3: show "$X.XX / $Y.YY" once spend totals API is available -->
                      <span v-if="key.max_budget != null">Budget: ${{ key.max_budget.toFixed(2) }}</span>
                      <span v-else class="text-gray-400">Unlimited</span>
                    </td>
                    <td class="px-4 py-3 text-sm">
                      <span v-if="key.is_active" class="bg-green-100 text-green-700 text-xs rounded-full px-2 py-0.5">active</span>
                      <span v-else class="bg-gray-100 text-gray-500 text-xs rounded-full px-2 py-0.5">revoked</span>
                    </td>
                    <td class="px-4 py-3 text-sm">
                      <template v-if="revokeConfirm === key.id">
                        <span class="text-xs text-gray-500 mr-1">Revoke {{ key.name }}?</span>
                        <button
                          :data-testid="`confirm-revoke-${key.id}`"
                          @click="confirmRevoke(key.id)"
                          class="text-xs text-red-600 hover:text-red-800 mr-1 font-medium"
                        >Revoke key</button>
                        <button @click="revokeConfirm = null" class="text-xs text-gray-500 hover:text-gray-700">Keep key</button>
                      </template>
                      <button
                        v-else-if="key.is_active"
                        :data-testid="`revoke-key-${key.id}`"
                        @click="revokeConfirm = key.id"
                        class="text-xs text-red-500 hover:text-red-700"
                      >Revoke</button>
                    </td>
                  </tr>
                  <!-- Inline edit models row -->
                  <tr v-if="editingModelsKeyId === key.id">
                    <td colspan="6" class="px-4 py-3 bg-indigo-50 border-t border-indigo-100">
                      <div class="flex items-start gap-3">
                        <div class="relative flex-1 max-w-md">
                          <label class="block text-xs text-gray-500 mb-1">Allowed models (leave empty for all)</label>
                          <input
                            v-model="editModelsInput"
                            type="text"
                            placeholder="e.g. gpt-4, claude-3-opus (comma-separated)"
                            autocomplete="off"
                            data-1p-ignore
                            data-lpignore="true"
                            class="input w-full"
                            @input="onEditModelsInput"
                            @keydown="onEditModelsKeydown"
                            @blur="hideEditSuggestionsDelayed"
                            @focus="onEditModelsInput"
                            ref="editModelsInputRef"
                          />
                          <ul
                            v-if="editModelsShowSuggestions && editModelsFilteredSuggestions.length > 0"
                            class="absolute z-10 mt-1 w-full bg-white border border-gray-200 rounded-md shadow-lg max-h-48 overflow-y-auto"
                          >
                            <li
                              v-for="(model, idx) in editModelsFilteredSuggestions"
                              :key="model"
                              class="px-3 py-2 text-sm cursor-pointer hover:bg-indigo-50"
                              :class="idx === editModelsSuggestionIndex ? 'bg-indigo-50 text-indigo-700' : 'text-gray-700'"
                              @mousedown.prevent="selectEditModelsSuggestion(model)"
                            >
                              {{ model }}
                            </li>
                          </ul>
                          <p v-if="editModelsError" class="mt-1 text-xs text-red-500">{{ editModelsError }}</p>
                        </div>
                        <div class="flex items-end gap-2 pt-5">
                          <button
                            @click="saveEditModels(key)"
                            :disabled="editModelsSubmitting"
                            class="btn btn-primary text-xs py-1 px-3"
                          >{{ editModelsSubmitting ? 'Saving…' : 'Save' }}</button>
                          <button
                            @click="cancelEditModels"
                            class="btn btn-secondary text-xs py-1 px-3"
                          >Cancel</button>
                        </div>
                      </div>
                    </td>
                  </tr>
                </template>
              </tbody>
            </table>
          </div>

          <!-- Create Key form -->
          <div v-if="currentUser?.is_admin || currentUser?.role !== 'viewer'" class="bg-white border border-gray-200 rounded-lg p-4">
            <h3 class="text-sm font-medium text-gray-700 mb-3">New Key</h3>

            <div v-if="formError" class="mb-4 px-3 py-2 bg-red-50 border border-red-200 rounded text-sm text-red-600 flex justify-between items-center">
              <span>{{ formError }}</span>
              <button @click="formError = null" class="ml-3 text-red-400 hover:text-red-600">&#10005;</button>
            </div>

            <form @submit.prevent="handleCreateKey" class="space-y-3 max-w-lg">
              <div>
                <label class="block text-xs text-gray-500 mb-1">Name</label>
                <input
                  v-model="form.name"
                  type="text"
                  placeholder="Key name"
                  autocomplete="off"
                  data-1p-ignore
                  data-lpignore="true"
                  class="input w-full"
                />
              </div>

              <!-- Allowed models with autocomplete -->
              <div class="relative">
                <label class="block text-xs text-gray-500 mb-1">Allowed models (leave empty for all)</label>
                <input
                  v-model="form.allowedModels"
                  type="text"
                  placeholder="e.g. gpt-4, claude-3-opus (comma-separated)"
                  autocomplete="off"
                  data-1p-ignore
                  data-lpignore="true"
                  class="input w-full"
                  @input="onModelsInput"
                  @keydown="onModelsKeydown"
                  @blur="hideSuggestionsDelayed"
                  @focus="onModelsInput"
                  ref="modelsInputRef"
                />
                <!-- Autocomplete dropdown -->
                <ul
                  v-if="showSuggestions && filteredSuggestions.length > 0"
                  class="absolute z-10 mt-1 w-full bg-white border border-gray-200 rounded-md shadow-lg max-h-48 overflow-y-auto"
                >
                  <li
                    v-for="(model, idx) in filteredSuggestions"
                    :key="model"
                    class="px-3 py-2 text-sm cursor-pointer hover:bg-indigo-50"
                    :class="idx === suggestionIndex ? 'bg-indigo-50 text-indigo-700' : 'text-gray-700'"
                    @mousedown.prevent="selectSuggestion(model)"
                  >
                    {{ model }}
                  </li>
                </ul>
              </div>

              <div class="flex gap-3">
                <div class="flex-1">
                  <label class="block text-xs text-gray-500 mb-1">Rate limit (requests/min)</label>
                  <input v-model="form.maxRPM" type="number" min="0" step="1" placeholder="Unlimited" autocomplete="off" data-1p-ignore data-lpignore="true" class="input w-full" />
                </div>
                <div class="flex-1">
                  <label class="block text-xs text-gray-500 mb-1">Rate limit (requests/day)</label>
                  <input v-model="form.maxRPD" type="number" min="0" step="1" placeholder="Unlimited" autocomplete="off" data-1p-ignore data-lpignore="true" class="input w-full" />
                </div>
              </div>

              <div class="flex gap-3">
                <div class="flex-1">
                  <label class="block text-xs text-gray-500 mb-1">Hard budget limit ($)</label>
                  <input v-model="form.maxBudget" type="number" min="0" step="0.01" placeholder="Unlimited" autocomplete="off" data-1p-ignore data-lpignore="true" class="input w-full" />
                </div>
                <div class="flex-1">
                  <label class="block text-xs text-gray-500 mb-1">Soft budget alert ($)</label>
                  <input
                    v-model="form.softBudget"
                    type="number"
                    min="0"
                    step="0.01"
                    placeholder="None"
                    autocomplete="off"
                    data-1p-ignore
                    data-lpignore="true"
                    class="input w-full"
                    :class="softBudgetError ? 'border-red-400 focus:ring-red-400' : ''"
                  />
                  <p v-if="softBudgetError" class="mt-1 text-xs text-red-500">Must be less than the hard budget limit.</p>
                </div>
              </div>

              <button type="submit" class="btn btn-primary" :disabled="!form.name.trim() || submitting">Create Key</button>
            </form>
          </div>
        </template>

        <!-- Right panel: no app selected -->
        <div v-else class="bg-white border border-gray-200 rounded-lg flex items-center justify-center py-16 text-gray-400 text-sm italic">
          Select a team and application to manage its keys
        </div>
      </div>
    </div>

    <!-- Post-creation modal: no Escape/overlay dismiss — user must click Done -->
    <div v-if="newKey" class="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div class="bg-white rounded-lg p-6 max-w-md w-full mx-4">
        <h2 class="text-lg font-semibold text-gray-900 mb-4">API Key Created</h2>
        <div class="bg-yellow-50 border border-yellow-300 text-sm text-yellow-800 rounded px-3 py-2 mb-4">
          This key will not be shown again. Copy it now and store it securely.
        </div>
        <!-- Key display with inline copy emoji -->
        <div class="relative mb-4">
          <div class="font-mono text-sm bg-gray-100 rounded px-3 py-2 break-all select-all text-gray-900 pr-10">
            {{ newKey }}
          </div>
          <button
            @click="copyKey"
            :title="copied ? 'Copied!' : 'Copy to clipboard'"
            class="absolute right-2 top-1/2 -translate-y-1/2 text-lg leading-none hover:scale-110 transition-transform"
          >{{ copied ? '✅' : '📋' }}</button>
        </div>
        <div class="flex gap-2">
          <button class="btn btn-secondary" @click="copyKey">{{ copied ? 'Copied!' : 'Copy Key' }}</button>
          <button class="btn btn-primary" @click="dismissModal">Done</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, nextTick } from 'vue'
import { api } from '../api/client.js'
import { useSession } from '../composables/useSession.js'

const { currentUser } = useSession()

// State
const teams = ref([])
const apps = ref([])
const keys = ref([])
const selectedTeam = ref(null)
const selectedApp = ref(null)
const loadingTeams = ref(false)
const loadingApps = ref(false)
const loadingKeys = ref(false)
const teamsError = ref(null)
const appsError = ref(null)
const error = ref(null)
const newKey = ref(null)
const copied = ref(false)
const revokeConfirm = ref(null)
const submitting = ref(false)
const formError = ref(null)
const form = ref({ name: '', allowedModels: '', maxRPM: '', maxRPD: '', maxBudget: '', softBudget: '' })

// Model autocomplete (create form)
const availableModels = ref([])
const showSuggestions = ref(false)
const suggestionIndex = ref(-1)
const modelsInputRef = ref(null)

// Edit models inline
const editingModelsKeyId = ref(null)
const editModelsInput = ref('')
const editModelsError = ref(null)
const editModelsSubmitting = ref(false)
const editModelsShowSuggestions = ref(false)
const editModelsSuggestionIndex = ref(-1)
const editModelsInputRef = ref(null)

// --- Create form autocomplete ---

const alreadySelected = computed(() => {
  const parts = form.value.allowedModels.split(',')
  return new Set(parts.slice(0, -1).map((m) => m.trim().toLowerCase()).filter((m) => m.length > 0))
})

const currentToken = computed(() => {
  const parts = form.value.allowedModels.split(',')
  return parts[parts.length - 1].trim().toLowerCase()
})

const filteredSuggestions = computed(() => {
  const token = currentToken.value
  const selected = alreadySelected.value
  const candidates = availableModels.value.filter((m) => !selected.has(m.toLowerCase()))
  if (!token) return candidates.slice(0, 8)
  return candidates.filter((m) => m.toLowerCase().includes(token)).slice(0, 8)
})

function onModelsInput() {
  showSuggestions.value = true
  suggestionIndex.value = -1
}

function onModelsKeydown(e) {
  if (!showSuggestions.value || filteredSuggestions.value.length === 0) return
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    suggestionIndex.value = Math.min(suggestionIndex.value + 1, filteredSuggestions.value.length - 1)
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    suggestionIndex.value = Math.max(suggestionIndex.value - 1, -1)
  } else if (e.key === 'Enter' && suggestionIndex.value >= 0) {
    e.preventDefault()
    selectSuggestion(filteredSuggestions.value[suggestionIndex.value])
  } else if (e.key === 'Escape') {
    showSuggestions.value = false
  }
}

function selectSuggestion(model) {
  const parts = form.value.allowedModels.split(',')
  parts[parts.length - 1] = model
  form.value.allowedModels = parts.join(', ') + ', '
  showSuggestions.value = false
  suggestionIndex.value = -1
  modelsInputRef.value?.focus()
}

function hideSuggestionsDelayed() {
  setTimeout(() => { showSuggestions.value = false }, 150)
}

// --- Edit models inline autocomplete ---

const editModelsCurrentToken = computed(() => {
  const parts = editModelsInput.value.split(',')
  return parts[parts.length - 1].trim().toLowerCase()
})

const editModelsAlreadySelected = computed(() => {
  const parts = editModelsInput.value.split(',')
  return new Set(parts.slice(0, -1).map((m) => m.trim().toLowerCase()).filter((m) => m.length > 0))
})

const editModelsFilteredSuggestions = computed(() => {
  const token = editModelsCurrentToken.value
  const selected = editModelsAlreadySelected.value
  const candidates = availableModels.value.filter((m) => !selected.has(m.toLowerCase()))
  if (!token) return candidates.slice(0, 8)
  return candidates.filter((m) => m.toLowerCase().includes(token)).slice(0, 8)
})

function onEditModelsInput() {
  editModelsShowSuggestions.value = true
  editModelsSuggestionIndex.value = -1
}

function onEditModelsKeydown(e) {
  if (!editModelsShowSuggestions.value || editModelsFilteredSuggestions.value.length === 0) return
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    editModelsSuggestionIndex.value = Math.min(editModelsSuggestionIndex.value + 1, editModelsFilteredSuggestions.value.length - 1)
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    editModelsSuggestionIndex.value = Math.max(editModelsSuggestionIndex.value - 1, -1)
  } else if (e.key === 'Enter' && editModelsSuggestionIndex.value >= 0) {
    e.preventDefault()
    selectEditModelsSuggestion(editModelsFilteredSuggestions.value[editModelsSuggestionIndex.value])
  } else if (e.key === 'Escape') {
    editModelsShowSuggestions.value = false
  }
}

function selectEditModelsSuggestion(model) {
  const parts = editModelsInput.value.split(',')
  parts[parts.length - 1] = model
  editModelsInput.value = parts.join(', ') + ', '
  editModelsShowSuggestions.value = false
  editModelsSuggestionIndex.value = -1
  editModelsInputRef.value?.focus()
}

function hideEditSuggestionsDelayed() {
  setTimeout(() => { editModelsShowSuggestions.value = false }, 150)
}

function startEditModels(key) {
  editingModelsKeyId.value = key.id
  editModelsInput.value = (key.allowed_models ?? []).join(', ')
  editModelsError.value = null
  editModelsShowSuggestions.value = false
  nextTick(() => editModelsInputRef.value?.focus())
}

function cancelEditModels() {
  editingModelsKeyId.value = null
  editModelsInput.value = ''
  editModelsError.value = null
}

async function saveEditModels(key) {
  editModelsError.value = null
  editModelsSubmitting.value = true
  const models = [...new Set(
    editModelsInput.value.split(',').map((m) => m.trim()).filter((m) => m.length > 0)
  )]
  try {
    await api.updateKeyModels(key.id, models)
    // Update the local key object immediately
    key.allowed_models = models
    cancelEditModels()
  } catch (e) {
    editModelsError.value = e.message || 'Failed to update models.'
  } finally {
    editModelsSubmitting.value = false
  }
}

// --- Models loader ---

async function loadModels() {
  try {
    const result = await api.models()
    availableModels.value = (result?.data ?? []).map((m) => m.id)
  } catch {
    // non-fatal — autocomplete just won't show
  }
}

// --- Validation ---

const softBudgetError = computed(() => {
  const hard = parseFloat(form.value.maxBudget)
  const soft = parseFloat(form.value.softBudget)
  if (form.value.softBudget !== '' && form.value.maxBudget !== '') {
    if (!isNaN(hard) && !isNaN(soft) && soft >= hard) return true
  }
  return false
})

// --- Data loading ---

async function loadTeams() {
  loadingTeams.value = true
  teamsError.value = null
  try {
    if (currentUser.value?.is_admin) {
      teams.value = await api.teams() ?? []
    } else {
      const memberships = await api.myTeams() ?? []
      teams.value = memberships.map((m) => ({ id: m.team_id, name: m.team_name }))
    }
  } catch (e) {
    teamsError.value = e.message
  } finally {
    loadingTeams.value = false
  }
}

async function selectTeam(team) {
  selectedTeam.value = team
  selectedApp.value = null
  keys.value = []
  error.value = null
  appsError.value = null
  revokeConfirm.value = null
  cancelEditModels()
  loadingApps.value = true
  appsError.value = null
  try {
    apps.value = await api.applications(team.id) ?? []
  } catch (e) {
    appsError.value = e.message
  } finally {
    loadingApps.value = false
  }
}

async function selectApp(app) {
  selectedApp.value = app
  error.value = null
  revokeConfirm.value = null
  cancelEditModels()
  await loadKeys(app.id)
}

async function loadKeys(appId) {
  loadingKeys.value = true
  error.value = null
  try {
    keys.value = await api.apiKeys(appId) ?? []
  } catch (e) {
    error.value = e.message
  } finally {
    loadingKeys.value = false
  }
}

function parseOptionalInt(val) {
  if (val === '' || val == null) return undefined
  const n = parseInt(val, 10)
  return isNaN(n) ? undefined : n
}

function parseOptionalFloat(val) {
  if (val === '' || val == null) return undefined
  const n = parseFloat(val)
  return isNaN(n) ? undefined : n
}

async function handleCreateKey() {
  if (!form.value.name.trim() || !selectedApp.value) return
  if (softBudgetError.value) {
    formError.value = 'Soft budget alert must be less than the hard budget limit.'
    return
  }
  formError.value = null
  submitting.value = true

  const allowedModels = [...new Set(
    form.value.allowedModels.split(',').map((m) => m.trim()).filter((m) => m.length > 0)
  )]

  const body = { name: form.value.name.trim() }
  if (allowedModels.length > 0) body.allowed_models = allowedModels
  const rpm = parseOptionalInt(form.value.maxRPM)
  const rpd = parseOptionalInt(form.value.maxRPD)
  const maxBudget = parseOptionalFloat(form.value.maxBudget)
  const softBudget = parseOptionalFloat(form.value.softBudget)
  if (rpm !== undefined) body.max_rpm = rpm
  if (rpd !== undefined) body.max_rpd = rpd
  if (maxBudget !== undefined) body.max_budget = maxBudget
  if (softBudget !== undefined) body.soft_budget = softBudget

  try {
    const result = await api.createAPIKey(selectedApp.value.id, body)
    newKey.value = result.key
    form.value = { name: '', allowedModels: '', maxRPM: '', maxRPD: '', maxBudget: '', softBudget: '' }
  } catch (e) {
    formError.value = e.message
  } finally {
    submitting.value = false
  }
}

async function dismissModal() {
  newKey.value = null
  copied.value = false
  if (selectedApp.value) await loadKeys(selectedApp.value.id)
}

async function copyKey() {
  try {
    await navigator.clipboard.writeText(newKey.value)
    copied.value = true
    setTimeout(() => { copied.value = false }, 2000)
  } catch {
    // fallback: text is select-all so user can Ctrl+C
  }
}

async function confirmRevoke(keyId) {
  error.value = null
  try {
    await api.revokeAPIKey(keyId)
    revokeConfirm.value = null
    if (selectedApp.value) await loadKeys(selectedApp.value.id)
  } catch (e) {
    error.value = e.message || 'Failed to revoke key. Try again.'
    revokeConfirm.value = null
  }
}

onMounted(async () => {
  await Promise.all([loadTeams(), loadModels()])
})
</script>
