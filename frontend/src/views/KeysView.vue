<template>
  <div class="p-6">
    <h1 class="text-2xl font-semibold text-gray-900 mb-6">API Keys</h1>

    <div class="flex gap-6">
      <!-- Left panel: Team list (w-64) -->
      <div class="w-64 flex-shrink-0">
        <div v-if="loadingTeams" class="text-gray-500 text-sm">Loading...</div>
        <div v-else-if="teamsError" class="text-red-600 text-sm">{{ teamsError }}</div>
        <ul v-else class="space-y-1">
          <li
            v-for="team in teams"
            :key="team.id"
            class="px-3 py-2 rounded-md cursor-pointer text-sm"
            :class="selectedTeam?.id === team.id ? 'bg-indigo-50 text-indigo-700' : 'text-gray-700 hover:bg-gray-50'"
            @click="selectTeam(team)"
          >
            {{ team.name }}
          </li>
          <li v-if="teams.length === 0" class="px-3 py-2 text-sm text-gray-400 italic">
            No teams found
          </li>
        </ul>
        <div v-if="!selectedTeam && !loadingTeams && teams.length > 0" class="mt-4 text-sm text-gray-400 italic">
          Select a team to view applications
        </div>
      </div>

      <!-- Middle panel: App list (w-64) — visible when a team is selected -->
      <div class="w-64 flex-shrink-0" v-if="selectedTeam">
        <div v-if="loadingApps" class="text-gray-500 text-sm">Loading...</div>
        <div v-else-if="appsError" class="text-red-600 text-sm">{{ appsError }}</div>
        <ul v-else class="space-y-1">
          <li
            v-for="app in apps"
            :key="app.id"
            class="px-3 py-2 rounded-md cursor-pointer text-sm"
            :class="selectedApp?.id === app.id ? 'bg-indigo-50 text-indigo-700' : 'text-gray-700 hover:bg-gray-50'"
            @click="selectApp(app)"
          >
            {{ app.name }}
          </li>
          <li v-if="apps.length === 0 && !loadingApps" class="px-3 py-2 text-sm text-gray-400 italic">
            No applications found
          </li>
        </ul>
        <div v-if="!selectedApp && !loadingApps && apps.length > 0" class="mt-4 text-sm text-gray-400 italic">
          Select an application to view its keys
        </div>
      </div>
      <div class="w-64 flex-shrink-0 flex items-start pt-2" v-else>
        <span class="text-sm text-gray-400 italic">Select a team to view applications</span>
      </div>

      <!-- Right panel: Keys table + Create form (flex-1) -->
      <div class="flex-1" v-if="selectedApp">
        <!-- Error banner -->
        <div v-if="error" class="mb-4 px-3 py-2 bg-red-50 border border-red-200 rounded text-sm text-red-600 flex justify-between items-center">
          <span>{{ error }}</span>
          <button @click="error = null" class="ml-3 text-red-400 hover:text-red-600">&#10005;</button>
        </div>

        <!-- Loading state -->
        <div v-if="loadingKeys" class="text-gray-500 text-sm">Loading keys...</div>
        <div v-else>
          <!-- Keys table -->
          <table class="min-w-full divide-y divide-gray-200 mb-6">
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
              <tr v-for="key in keys" :key="key.id">
                <!-- Prefix -->
                <td class="px-4 py-3 font-mono text-sm text-gray-900">{{ key.key_prefix }}...</td>
                <!-- Name -->
                <td class="px-4 py-3 text-sm font-medium text-gray-900">{{ key.name }}</td>
                <!-- Models -->
                <td class="px-4 py-3 text-sm text-gray-500">
                  <!-- Phase 3: show allowed model count once API includes it in list response -->
                  <span class="italic">All models</span>
                </td>
                <!-- Spend / Budget -->
                <td class="px-4 py-3 text-sm text-gray-700">
                  <!-- Phase 3: fetch spend totals from cost API and display "$X.XX / $Y.YY" -->
                  <span v-if="key.max_budget != null">Budget: ${{ key.max_budget.toFixed(2) }}</span>
                  <span v-else class="text-gray-400">Unlimited</span>
                </td>
                <!-- Status badge -->
                <td class="px-4 py-3 text-sm">
                  <span
                    v-if="key.is_active"
                    class="bg-green-100 text-green-700 text-xs rounded-full px-2 py-0.5"
                  >active</span>
                  <span
                    v-else
                    class="bg-gray-100 text-gray-500 text-xs rounded-full px-2 py-0.5"
                  >revoked</span>
                </td>
                <!-- Actions: inline revoke confirmation -->
                <td class="px-4 py-3 text-sm">
                  <template v-if="revokeConfirm === key.id">
                    <span class="text-xs text-gray-500 mr-1">Revoke {{ key.name }}?</span>
                    <button
                      :data-testid="`confirm-revoke-${key.id}`"
                      @click="confirmRevoke(key.id)"
                      class="text-xs text-red-600 hover:text-red-800 mr-1 font-medium"
                    >Revoke key</button>
                    <button
                      @click="revokeConfirm = null"
                      class="text-xs text-gray-500 hover:text-gray-700"
                    >Keep key</button>
                  </template>
                  <button
                    v-else-if="key.is_active"
                    :data-testid="`revoke-key-${key.id}`"
                    @click="revokeConfirm = key.id"
                    class="text-xs text-red-500 hover:text-red-700"
                  >Revoke</button>
                </td>
              </tr>
            </tbody>
          </table>

          <!-- Create Key form (admin/member only — hidden for viewer role) -->
          <div v-if="currentUser?.is_admin || currentUser?.role !== 'viewer'" class="border-t border-gray-200 pt-4">
            <h3 class="text-sm font-medium text-gray-700 mb-3">New Key</h3>

            <!-- Form-level validation error -->
            <div v-if="formError" class="mb-4 px-3 py-2 bg-red-50 border border-red-200 rounded text-sm text-red-600 flex justify-between items-center">
              <span>{{ formError }}</span>
              <button @click="formError = null" class="ml-3 text-red-400 hover:text-red-600">&#10005;</button>
            </div>

            <form @submit.prevent="handleCreateKey" class="space-y-3 max-w-md">
              <!-- Name (required) -->
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

              <!-- Allowed models -->
              <div>
                <label class="block text-xs text-gray-500 mb-1">Allowed models (leave empty for all)</label>
                <input
                  v-model="form.allowedModels"
                  type="text"
                  placeholder="e.g. gpt-4, claude-3-opus (comma-separated)"
                  autocomplete="off"
                  data-1p-ignore
                  data-lpignore="true"
                  class="input w-full"
                />
              </div>

              <!-- Rate limits -->
              <div class="flex gap-3">
                <div class="flex-1">
                  <label class="block text-xs text-gray-500 mb-1">Rate limit (requests/min)</label>
                  <input
                    v-model="form.maxRPM"
                    type="number"
                    min="0"
                    step="1"
                    placeholder="Unlimited"
                    autocomplete="off"
                    data-1p-ignore
                    data-lpignore="true"
                    class="input w-full"
                  />
                </div>
                <div class="flex-1">
                  <label class="block text-xs text-gray-500 mb-1">Rate limit (requests/day)</label>
                  <input
                    v-model="form.maxRPD"
                    type="number"
                    min="0"
                    step="1"
                    placeholder="Unlimited"
                    autocomplete="off"
                    data-1p-ignore
                    data-lpignore="true"
                    class="input w-full"
                  />
                </div>
              </div>

              <!-- Budget limits -->
              <div class="flex gap-3">
                <div class="flex-1">
                  <label class="block text-xs text-gray-500 mb-1">Hard budget limit ($)</label>
                  <input
                    v-model="form.maxBudget"
                    type="number"
                    min="0"
                    step="0.01"
                    placeholder="Unlimited"
                    autocomplete="off"
                    data-1p-ignore
                    data-lpignore="true"
                    class="input w-full"
                  />
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

              <button
                type="submit"
                class="btn btn-primary"
                :disabled="!form.name.trim() || submitting"
              >Create Key</button>
            </form>
          </div>
        </div>
      </div>

      <!-- Right panel: no app selected -->
      <div class="flex-1 flex items-center justify-center text-gray-400 text-sm" v-else>
        Select an application to view its keys
      </div>
    </div>

    <!-- Post-creation modal: no Escape/overlay dismiss — user must click Done -->
    <div v-if="newKey" class="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div class="bg-white rounded-lg p-6 max-w-md w-full mx-4">
        <h2 class="text-lg font-semibold text-gray-900 mb-4">API Key Created</h2>
        <div class="bg-yellow-50 border border-yellow-300 text-sm text-yellow-800 rounded px-3 py-2 mb-4">
          This key will not be shown again. Copy it now and store it securely.
        </div>
        <div class="font-mono text-sm bg-gray-100 rounded px-3 py-2 break-all select-all text-gray-900 mb-4">
          {{ newKey }}
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
import { ref, computed, onMounted } from 'vue'
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
const newKey = ref(null)       // plaintext key from creation — cleared on Done
const copied = ref(false)
const revokeConfirm = ref(null) // key.id of the row showing inline confirm
const submitting = ref(false)
const formError = ref(null)
const form = ref({
  name: '',
  allowedModels: '',
  maxRPM: '',
  maxRPD: '',
  maxBudget: '',
  softBudget: '',
})

// Validation: soft budget must be <= hard budget
const softBudgetError = computed(() => {
  const hard = parseFloat(form.value.maxBudget)
  const soft = parseFloat(form.value.softBudget)
  if (form.value.softBudget !== '' && form.value.maxBudget !== '') {
    if (!isNaN(hard) && !isNaN(soft) && soft >= hard) {
      return true
    }
  }
  return false
})

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
  await loadApps(team.id)
}

async function loadApps(teamId) {
  loadingApps.value = true
  appsError.value = null
  try {
    apps.value = await api.applications(teamId) ?? []
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
  if (val === '' || val === null || val === undefined) return undefined
  const n = parseInt(val, 10)
  return isNaN(n) ? undefined : n
}

function parseOptionalFloat(val) {
  if (val === '' || val === null || val === undefined) return undefined
  const n = parseFloat(val)
  return isNaN(n) ? undefined : n
}

async function handleCreateKey() {
  if (!form.value.name.trim() || !selectedApp.value) return

  // Validate soft budget <= hard budget
  if (softBudgetError.value) {
    formError.value = 'Soft budget alert must be less than the hard budget limit.'
    return
  }

  formError.value = null
  submitting.value = true

  // Normalize allowed models: trim, deduplicate, filter empty tokens
  const allowedModels = form.value.allowedModels
    .split(',')
    .map((m) => m.trim())
    .filter((m) => m.length > 0)
  const uniqueModels = [...new Set(allowedModels)]

  const body = {
    name: form.value.name.trim(),
  }
  if (uniqueModels.length > 0) {
    body.allowed_models = uniqueModels
  }
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
    // Reset form
    form.value = {
      name: '',
      allowedModels: '',
      maxRPM: '',
      maxRPD: '',
      maxBudget: '',
      softBudget: '',
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
  // Refresh key list after modal close
  if (selectedApp.value) {
    await loadKeys(selectedApp.value.id)
  }
}

async function copyKey() {
  try {
    await navigator.clipboard.writeText(newKey.value)
    copied.value = true
    setTimeout(() => {
      copied.value = false
    }, 2000)
  } catch (e) {
    // Fallback: select the text for manual copy
  }
}

async function confirmRevoke(keyId) {
  error.value = null
  try {
    await api.revokeAPIKey(keyId)
    revokeConfirm.value = null
    if (selectedApp.value) {
      await loadKeys(selectedApp.value.id)
    }
  } catch (e) {
    error.value = e.message || 'Failed to revoke key. Try again.'
    revokeConfirm.value = null
  }
}

onMounted(async () => {
  await loadTeams()
})
</script>
