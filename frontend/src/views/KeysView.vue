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

      <!-- Right panel: Keys table + Create/Edit form -->
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
                <tr
                  v-for="key in keys"
                  :key="key.id"
                  :class="editingKey?.id === key.id ? 'bg-indigo-50' : ''"
                >
                  <td class="px-4 py-3 font-mono text-sm text-gray-900">{{ key.key_prefix }}...</td>
                  <td class="px-4 py-3 text-sm font-medium text-gray-900">{{ key.name }}</td>
                  <td class="px-4 py-3 text-sm text-gray-500">
                    <span v-if="!key.allowed_models || key.allowed_models.length === 0" class="italic">All models</span>
                    <span v-else>
                      {{ key.allowed_models.slice(0, 2).join(', ') }}
                      <span v-if="key.allowed_models.length > 2" class="text-gray-400"> +{{ key.allowed_models.length - 2 }} more</span>
                    </span>
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
                  <td class="px-4 py-3 text-sm space-x-2">
                    <template v-if="key.is_active">
                      <!-- Edit button -->
                      <button
                        @click="startEditKey(key)"
                        class="text-xs text-indigo-500 hover:text-indigo-700"
                      >Edit</button>
                      <!-- Revoke inline confirmation -->
                      <template v-if="revokeConfirm === key.id">
                        <span class="text-xs text-gray-500">Revoke?</span>
                        <button
                          :data-testid="`confirm-revoke-${key.id}`"
                          @click="confirmRevoke(key.id)"
                          class="text-xs text-red-600 hover:text-red-800 font-medium"
                        >Yes</button>
                        <button @click="revokeConfirm = null" class="text-xs text-gray-500 hover:text-gray-700">No</button>
                      </template>
                      <button
                        v-else
                        :data-testid="`revoke-key-${key.id}`"
                        @click="revokeConfirm = key.id"
                        class="text-xs text-red-500 hover:text-red-700"
                      >Revoke</button>
                    </template>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Create / Edit Key form -->
          <div
            v-if="currentUser?.is_admin || currentUser?.role !== 'viewer'"
            class="bg-white border border-gray-200 rounded-lg p-4"
            :class="editingKey ? 'border-indigo-300 ring-1 ring-indigo-200' : ''"
            ref="formRef"
          >
            <!-- Form heading -->
            <div class="flex items-center justify-between mb-3">
              <div>
                <h3 class="text-sm font-medium text-gray-700">
                  <template v-if="editingKey">
                    Edit Key — {{ editingKey.name }} —
                    <span class="font-mono text-gray-500">{{ editingKey.key_prefix }}...</span>
                  </template>
                  <template v-else>New Key</template>
                </h3>
                <p v-if="editingKey" class="text-xs text-gray-400 mt-0.5">
                  Changes apply immediately. The key value is not affected.
                </p>
              </div>
              <button
                v-if="editingKey"
                @click="cancelEdit"
                class="text-xs text-gray-400 hover:text-gray-600"
              >&#10005; Cancel edit</button>
            </div>

            <div v-if="formError" class="mb-4 px-3 py-2 bg-red-50 border border-red-200 rounded text-sm text-red-600 flex justify-between items-center">
              <span>{{ formError }}</span>
              <button @click="formError = null" class="ml-3 text-red-400 hover:text-red-600">&#10005;</button>
            </div>

            <form @submit.prevent="handleSubmitForm" class="space-y-3 max-w-lg">
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

              <div class="flex gap-2">
                <button
                  type="submit"
                  class="btn btn-primary"
                  :disabled="!form.name.trim() || submitting"
                >{{ editingKey ? 'Save Changes' : 'Create Key' }}</button>
                <button
                  v-if="editingKey"
                  type="button"
                  @click="cancelEdit"
                  class="btn btn-secondary"
                >Cancel</button>
              </div>
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
const editingKey = ref(null)
const formRef = ref(null)
const form = ref({ name: '', allowedModels: '', maxRPM: '', maxRPD: '', maxBudget: '', softBudget: '' })

// Model autocomplete
const availableModels = ref([])
const showSuggestions = ref(false)
const suggestionIndex = ref(-1)
const modelsInputRef = ref(null)

// --- Autocomplete ---

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

async function loadModels() {
  try {
    const result = await api.models()
    availableModels.value = (result?.data ?? []).map((m) => m.id)
  } catch {
    // non-fatal
  }
}

// --- Edit key ---

function startEditKey(key) {
  editingKey.value = key
  form.value = {
    name: key.name,
    allowedModels: (key.allowed_models ?? []).join(', '),
    maxRPM: key.max_rpm ?? '',
    maxRPD: key.max_rpd ?? '',
    maxBudget: key.max_budget ?? '',
    softBudget: key.soft_budget ?? '',
  }
  revokeConfirm.value = null
  formError.value = null
  nextTick(() => formRef.value?.scrollIntoView({ behavior: 'smooth', block: 'nearest' }))
}

function cancelEdit() {
  editingKey.value = null
  form.value = { name: '', allowedModels: '', maxRPM: '', maxRPD: '', maxBudget: '', softBudget: '' }
  formError.value = null
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
  cancelEdit()
  loadingApps.value = true
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
  cancelEdit()
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

function buildFormBody() {
  const allowedModels = [...new Set(
    form.value.allowedModels.split(',').map((m) => m.trim()).filter((m) => m.length > 0)
  )]
  const body = { name: form.value.name.trim() }
  if (allowedModels.length > 0) body.allowed_models = allowedModels
  else body.allowed_models = []
  const rpm = parseOptionalInt(form.value.maxRPM)
  const rpd = parseOptionalInt(form.value.maxRPD)
  const maxBudget = parseOptionalFloat(form.value.maxBudget)
  const softBudget = parseOptionalFloat(form.value.softBudget)
  if (rpm !== undefined) body.max_rpm = rpm
  if (rpd !== undefined) body.max_rpd = rpd
  if (maxBudget !== undefined) body.max_budget = maxBudget
  if (softBudget !== undefined) body.soft_budget = softBudget
  return body
}

async function handleSubmitForm() {
  if (!form.value.name.trim()) return
  if (softBudgetError.value) {
    formError.value = 'Soft budget alert must be less than the hard budget limit.'
    return
  }
  formError.value = null
  submitting.value = true
  try {
    if (editingKey.value) {
      await api.updateAPIKey(editingKey.value.id, buildFormBody())
      cancelEdit()
      if (selectedApp.value) await loadKeys(selectedApp.value.id)
    } else {
      if (!selectedApp.value) return
      const body = buildFormBody()
      // For create, omit empty allowed_models array
      if (body.allowed_models?.length === 0) delete body.allowed_models
      const result = await api.createAPIKey(selectedApp.value.id, body)
      newKey.value = result.key
      form.value = { name: '', allowedModels: '', maxRPM: '', maxRPD: '', maxBudget: '', softBudget: '' }
    }
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
    if (editingKey.value?.id === keyId) cancelEdit()
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
