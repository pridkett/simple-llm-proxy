<template>
  <section class="min-w-0">
    <div class="flex flex-wrap items-center justify-between gap-3 mb-4">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">
        {{ application.name }}
      </h2>
      <RouterLink
        v-if="isAdmin && !isCreating"
        :to="`${applicationPath}/keys/new`"
        class="btn btn-primary"
        >Create key</RouterLink
      >
    </div>
    <p
      v-if="
        route.params.keyId &&
        !loadingKeys &&
        !error &&
        !editingKey &&
        !viewingKey
      "
      role="alert"
      class="mb-4 text-sm text-red-600 dark:text-red-400"
    >
      Key not found in this application.
    </p>
    <!-- Error banner -->
    <div
      v-if="error"
      role="alert"
      class="mb-4 px-3 py-2 bg-red-50 dark:bg-red-950 border border-red-200 dark:border-red-800 rounded text-sm text-red-600 dark:text-red-400 flex justify-between items-center"
    >
      <span>{{ error }}</span>
      <button
        @click="error = null"
        class="ml-3 text-red-400 hover:text-red-600 dark:hover:text-red-400"
      >
        &#10005;
      </button>
    </div>

    <!-- Keys table -->
    <div
      class="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg overflow-x-auto mb-4"
    >
      <div v-if="loadingKeys" class="px-4 py-3 text-gray-500 dark:text-gray-400 text-sm">
        Loading keys...
      </div>
      <table v-else class="min-w-[38rem] w-full divide-y divide-gray-200 dark:divide-gray-700">
        <thead class="bg-gray-50 dark:bg-gray-900">
          <tr>
            <th
              class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase"
            >
              Prefix
            </th>
            <th
              class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase"
            >
              Name
            </th>
            <th
              class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase"
            >
              Models
            </th>
            <th
              class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase"
            >
              Budget
            </th>
            <th
              class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase"
            >
              Status
            </th>
            <th
              class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase"
            >
              Actions
            </th>
          </tr>
        </thead>
        <tbody class="bg-white dark:bg-gray-800 divide-y divide-gray-200 dark:divide-gray-700">
          <tr v-if="keys.length === 0">
            <td
              colspan="6"
              class="px-4 py-8 text-center text-sm text-gray-400 italic"
            >
              No keys yet.
            </td>
          </tr>
          <tr
            v-for="key in keys"
            :key="key.id"
            :class="editingKey?.id === key.id ? 'bg-indigo-50 dark:bg-indigo-950' : ''"
          >
            <td class="px-4 py-3 font-mono text-sm text-gray-900 dark:text-gray-100">
              {{ key.key_prefix }}...
            </td>
            <td class="px-4 py-3 text-sm font-medium text-gray-900 dark:text-gray-100">
              <RouterLink
                :to="`${applicationPath}/keys/${key.id}`"
                class="text-indigo-600 dark:text-indigo-400 hover:underline"
                >{{ key.name }}</RouterLink
              >
            </td>
            <td class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">
              <span
                v-if="!key.allowed_models || key.allowed_models.length === 0"
                class="italic"
                >All models</span
              >
              <span v-else>
                {{ key.allowed_models.slice(0, 2).join(', ') }}
                <span
                  v-if="key.allowed_models.length > 2"
                  class="text-gray-400"
                >
                  +{{ key.allowed_models.length - 2 }} more</span
                >
              </span>
            </td>
            <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-200">
              <span v-if="key.max_budget != null">
                ${{ (keySpend[key.id] ?? 0).toFixed(4) }} / ${{
                  key.max_budget.toFixed(2)
                }}
              </span>
              <span v-else>
                ${{ (keySpend[key.id] ?? 0).toFixed(4) }} / &#8734;
              </span>
            </td>
            <td class="px-4 py-3 text-sm">
              <span
                v-if="key.is_active"
                class="bg-green-100 dark:bg-green-900 text-green-700 dark:text-green-300 text-xs rounded-full px-2 py-0.5"
                >active</span
              >
              <span
                v-else
                class="bg-gray-100 dark:bg-gray-700 text-gray-500 dark:text-gray-400 text-xs rounded-full px-2 py-0.5"
                >revoked</span
              >
            </td>
            <td class="px-4 py-3 text-sm space-x-2">
              <template v-if="!key.is_active">
                <button
                  @click="openKey(key)"
                  class="text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200"
                >
                  View
                </button>
              </template>
              <template v-else-if="isAdmin">
                <!-- Edit button -->
                <button
                  @click="openKey(key)"
                  class="text-xs text-indigo-500 dark:text-indigo-400 hover:text-indigo-700 dark:hover:text-indigo-300"
                >
                  Edit
                </button>
                <!-- Revoke inline confirmation -->
                <template v-if="revokeConfirm === key.id">
                  <span class="text-xs text-gray-500 dark:text-gray-400">Revoke?</span>
                  <button
                    :data-testid="`confirm-revoke-${key.id}`"
                    @click="confirmRevoke(key.id)"
                    class="text-xs text-red-600 dark:text-red-400 hover:text-red-800 dark:hover:text-red-200 font-medium"
                  >
                    Yes
                  </button>
                  <button
                    @click="revokeConfirm = null"
                    class="text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200"
                  >
                    No
                  </button>
                </template>
                <button
                  v-else
                  :data-testid="`revoke-key-${key.id}`"
                  @click="revokeConfirm = key.id"
                  class="text-xs text-red-500 dark:text-red-400 hover:text-red-700 dark:hover:text-red-300"
                >
                  Revoke
                </button>
              </template>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Read-only view for revoked key -->
    <div
      v-if="viewingKey && !editingKey"
      class="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg p-4 mb-4"
    >
      <div class="flex items-center justify-between mb-3">
        <div>
          <h3 class="text-sm font-medium text-gray-700 dark:text-gray-200">
            Key Settings — {{ viewingKey.name }} —
            <span class="font-mono text-gray-500 dark:text-gray-400"
              >{{ viewingKey.key_prefix }}...</span
            >
          </h3>
          <p v-if="!viewingKey.is_active" class="text-xs text-gray-400 mt-0.5">
            This key has been revoked and cannot be edited.
          </p>
        </div>
        <button
          @click="closeKey"
          class="text-xs text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
        >
          &#10005; Close
        </button>
      </div>
      <dl class="space-y-2 text-sm max-w-lg">
        <div class="flex gap-2">
          <dt class="w-40 text-xs text-gray-500 dark:text-gray-400 shrink-0">Name</dt>
          <dd class="text-gray-900 dark:text-gray-100">{{ viewingKey.name }}</dd>
        </div>
        <div class="flex gap-2">
          <dt class="w-40 text-xs text-gray-500 dark:text-gray-400 shrink-0">Allowed models</dt>
          <dd class="text-gray-900 dark:text-gray-100">
            <span
              v-if="!viewingKey.allowed_models?.length"
              class="italic text-gray-400"
              >All models</span
            >
            <span v-else>{{ viewingKey.allowed_models.join(', ') }}</span>
            <p class="mt-1">Decision models: {{ viewingKey.allow_decisions ? 'Allowed' : 'Disabled' }}</p>
          </dd>
        </div>
        <div class="flex gap-2">
          <dt class="w-40 text-xs text-gray-500 dark:text-gray-400 shrink-0">Rate limit (RPM)</dt>
          <dd class="text-gray-900 dark:text-gray-100">{{ viewingKey.max_rpm ?? '—' }}</dd>
        </div>
        <div class="flex gap-2">
          <dt class="w-40 text-xs text-gray-500 dark:text-gray-400 shrink-0">Rate limit (RPD)</dt>
          <dd class="text-gray-900 dark:text-gray-100">{{ viewingKey.max_rpd ?? '—' }}</dd>
        </div>
        <div class="flex gap-2">
          <dt class="w-40 text-xs text-gray-500 dark:text-gray-400 shrink-0">Hard budget</dt>
          <dd class="text-gray-900 dark:text-gray-100">
            {{
              viewingKey.max_budget != null
                ? `$${viewingKey.max_budget.toFixed(2)}`
                : '—'
            }}
          </dd>
        </div>
        <div class="flex gap-2">
          <dt class="w-40 text-xs text-gray-500 dark:text-gray-400 shrink-0">Soft budget alert</dt>
          <dd class="text-gray-900 dark:text-gray-100">
            {{
              viewingKey.soft_budget != null
                ? `$${viewingKey.soft_budget.toFixed(2)}`
                : '—'
            }}
          </dd>
        </div>
      </dl>
    </div>

    <!-- Create / Edit Key form -->
    <div
      v-if="isAdmin && (editingKey || isCreating)"
      class="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg p-4"
      :class="editingKey ? 'border-indigo-300 dark:border-indigo-700 ring-1 ring-indigo-200 dark:ring-indigo-800' : ''"
      ref="formRef"
    >
      <!-- Form heading -->
      <div class="flex items-center justify-between mb-3">
        <div>
          <h3 class="text-sm font-medium text-gray-700 dark:text-gray-200">
            <template v-if="editingKey">
              Edit Key — {{ editingKey.name }} —
              <span class="font-mono text-gray-500 dark:text-gray-400"
                >{{ editingKey.key_prefix }}...</span
              >
            </template>
            <template v-else>New Key</template>
          </h3>
          <p v-if="editingKey" class="text-xs text-gray-400 mt-0.5">
            Changes apply immediately. The key value is not affected.
          </p>
        </div>
        <button
          v-if="editingKey"
          @click="closeKey"
          class="text-xs text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
        >
          &#10005; Cancel edit
        </button>
      </div>

      <div
        v-if="formError"
        role="alert"
        class="mb-4 px-3 py-2 bg-red-50 dark:bg-red-950 border border-red-200 dark:border-red-800 rounded text-sm text-red-600 dark:text-red-400 flex justify-between items-center"
      >
        <span>{{ formError }}</span>
        <button
          @click="formError = null"
          class="ml-3 text-red-400 hover:text-red-600 dark:hover:text-red-400"
        >
          &#10005;
        </button>
      </div>

      <form @submit.prevent="handleSubmitForm" class="space-y-3 max-w-lg">
        <div>
          <label for="key-name" class="block text-xs text-gray-500 dark:text-gray-400 mb-1"
            >Name</label
          >
          <input
            id="key-name"
            v-model="form.name"
            type="text"
            placeholder="Key name"
            autocomplete="off"
            data-1p-ignore
            data-lpignore="true"
            class="input w-full"
          />
        </div>

        <details :open="!!editingKey">
          <summary class="cursor-pointer text-sm text-gray-600 dark:text-gray-300">
            Models and limits
          </summary>
          <label class="flex items-center gap-2 text-sm py-2" for="key-allow-decisions">
            <input id="key-allow-decisions" type="checkbox" v-model="form.allowDecisions" />
            Allow decision models
          </label>
          <p class="text-xs text-gray-500 dark:text-gray-400 mb-2">The model allowlist and spending limits also apply to decision calls.</p>
          <!-- Allowed models with autocomplete -->
          <div class="relative">
            <label for="key-models" class="block text-xs text-gray-500 dark:text-gray-400 mb-1"
              >Allowed models (leave empty for all)</label
            >
            <input
              id="key-models"
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
              class="absolute z-10 mt-1 w-full bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md shadow-lg max-h-48 overflow-y-auto"
            >
              <li
                v-for="(model, idx) in filteredSuggestions"
                :key="model"
                class="px-3 py-2 text-sm cursor-pointer hover:bg-indigo-50 dark:hover:bg-indigo-950"
                :class="
                  idx === suggestionIndex
                    ? 'bg-indigo-50 dark:bg-indigo-950 text-indigo-700 dark:text-indigo-300'
                    : 'text-gray-700 dark:text-gray-200'
                "
                @mousedown.prevent="selectSuggestion(model)"
              >
                {{ model }}
              </li>
            </ul>
          </div>

          <div class="flex flex-wrap gap-3">
            <div class="flex-1 min-w-[12rem]">
              <label for="key-rpm" class="block text-xs text-gray-500 dark:text-gray-400 mb-1"
                >Rate limit (requests/min)</label
              >
              <input
                id="key-rpm"
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
            <div class="flex-1 min-w-[12rem]">
              <label for="key-rpd" class="block text-xs text-gray-500 dark:text-gray-400 mb-1"
                >Rate limit (requests/day)</label
              >
              <input
                id="key-rpd"
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

          <div class="flex flex-wrap gap-3">
            <div class="flex-1 min-w-[12rem]">
              <label for="key-budget" class="block text-xs text-gray-500 dark:text-gray-400 mb-1"
                >Hard budget limit ($)</label
              >
              <input
                id="key-budget"
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
            <div class="flex-1 min-w-[12rem]">
              <label
                for="key-soft-budget"
                class="block text-xs text-gray-500 dark:text-gray-400 mb-1"
                >Soft budget alert ($)</label
              >
              <input
                id="key-soft-budget"
                v-model="form.softBudget"
                type="number"
                min="0"
                step="0.01"
                placeholder="None"
                autocomplete="off"
                data-1p-ignore
                data-lpignore="true"
                class="input w-full"
                :class="
                  softBudgetError ? 'border-red-400 focus:ring-red-400' : ''
                "
              />
              <p v-if="softBudgetError" class="mt-1 text-xs text-red-500 dark:text-red-400">
                Must be less than the hard budget limit.
              </p>
            </div>
          </div>
        </details>
        <div class="flex gap-2">
          <button
            type="submit"
            class="btn btn-primary"
            :disabled="!form.name.trim() || submitting"
          >
            {{ editingKey ? 'Save Changes' : 'Create Key' }}
          </button>
          <button type="button" @click="closeKey" class="btn btn-secondary">
            Cancel
          </button>
        </div>
      </form>
    </div>
  </section>
</template>
<script setup>
import { ref, computed, onMounted, nextTick, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api/client.js'
import { useSession } from '../composables/useSession.js'

const { currentUser } = useSession()
const route = useRoute()
const router = useRouter()
const props = defineProps({ application: { type: Object, required: true } })
const emit = defineEmits(['created', 'changed', 'selected-key', 'busy'])
const isAdmin = computed(() => !!currentUser.value?.is_admin)
const applicationPath = computed(
  () =>
    `/teams/${props.application.team_id}/applications/${props.application.id}`
)
const isCreating = computed(() => route.name === 'team-key-new')

// State
const keys = ref([])
const keySpend = ref({})
const selectedApp = computed(() => props.application)
const loadingKeys = ref(false)
const error = ref(null)
const revokeConfirm = ref(null)
const viewingKey = ref(null)
const submitting = ref(false)
const formError = ref(null)
const editingKey = ref(null)
const formRef = ref(null)
const form = ref({
  name: '',
  allowedModels: '',
  allowDecisions: false,
  maxRPM: '',
  maxRPD: '',
  maxBudget: '',
  softBudget: ''
})

// Model autocomplete
const availableModels = ref([])
const showSuggestions = ref(false)
const suggestionIndex = ref(-1)
const modelsInputRef = ref(null)

// --- Autocomplete ---

const alreadySelected = computed(() => {
  const parts = form.value.allowedModels.split(',')
  return new Set(
    parts
      .slice(0, -1)
      .map((m) => m.trim().toLowerCase())
      .filter((m) => m.length > 0)
  )
})

const currentToken = computed(() => {
  const parts = form.value.allowedModels.split(',')
  return parts[parts.length - 1].trim().toLowerCase()
})

const filteredSuggestions = computed(() => {
  const token = currentToken.value
  const selected = alreadySelected.value
  const candidates = availableModels.value.filter(
    (m) => !selected.has(m.toLowerCase())
  )
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
    suggestionIndex.value = Math.min(
      suggestionIndex.value + 1,
      filteredSuggestions.value.length - 1
    )
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
  setTimeout(() => {
    showSuggestions.value = false
  }, 150)
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

function fillKeyForm(key) {
  viewingKey.value = null
  editingKey.value = key
  form.value = {
    name: key.name,
    allowedModels: (key.allowed_models ?? []).join(', '),
    allowDecisions: key.allow_decisions === true,
    maxRPM: key.max_rpm ?? '',
    maxRPD: key.max_rpd ?? '',
    maxBudget: key.max_budget ?? '',
    softBudget: key.soft_budget ?? ''
  }
  revokeConfirm.value = null
  formError.value = null
  nextTick(() =>
    formRef.value?.scrollIntoView?.({ behavior: 'smooth', block: 'nearest' })
  )
}

function cancelEdit() {
  editingKey.value = null
  form.value = {
    name: '',
    allowedModels: '',
  allowDecisions: false,
    maxRPM: '',
    maxRPD: '',
    maxBudget: '',
    softBudget: ''
  }
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

async function loadKeys(appId) {
  loadingKeys.value = true
  error.value = null
  try {
    keys.value = (await api.apiKeys(appId)) ?? []
  } catch (e) {
    error.value = e.message
  } finally {
    loadingKeys.value = false
  }
}

async function loadKeySpend(appId) {
  try {
    const data = await api.spend({ appId })
    if (data && data.rows) {
      const map = {}
      data.rows.forEach((r) => {
        map[r.key_id] = r.total_spend
      })
      keySpend.value = map
    }
  } catch {
    // Non-critical — spend column shows $0.00 gracefully on error
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
  const allowedModels = [
    ...new Set(
      form.value.allowedModels
        .split(',')
        .map((m) => m.trim())
        .filter((m) => m.length > 0)
    )
  ]
  const body = { name: form.value.name.trim(), allow_decisions: form.value.allowDecisions }
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
  if (submitting.value || !isAdmin.value || !form.value.name.trim()) return
  if (softBudgetError.value) {
    formError.value =
      'Soft budget alert must be less than the hard budget limit.'
    return
  }
  formError.value = null
  submitting.value = true
  emit('busy', true)
  try {
    if (editingKey.value) {
      await api.updateAPIKey(editingKey.value.id, buildFormBody())
      cancelEdit()
      if (selectedApp.value) await loadKeys(selectedApp.value.id)
      emit('busy', false)
      await router.push(applicationPath.value)
    } else {
      if (!selectedApp.value) return
      const body = buildFormBody()
      // For create, omit empty allowed_models array
      if (body.allowed_models?.length === 0) delete body.allowed_models
      const result = await api.createAPIKey(selectedApp.value.id, body)
      emit('busy', false)
      await router.push(applicationPath.value)
      emit('created', result.key)
      await loadKeys(selectedApp.value.id)
      emit('changed')
      form.value = {
        name: '',
        allowedModels: '',
  allowDecisions: false,
        maxRPM: '',
        maxRPD: '',
        maxBudget: '',
        softBudget: ''
      }
    }
  } catch (e) {
    formError.value = e.message
  } finally {
    submitting.value = false
    emit('busy', false)
  }
}

async function confirmRevoke(keyId) {
  error.value = null
  try {
    await api.revokeAPIKey(keyId)
    revokeConfirm.value = null
    if (editingKey.value?.id === keyId) cancelEdit()
    if (selectedApp.value) await loadKeys(selectedApp.value.id)
    emit('changed')
    if (Number(route.params.keyId) === keyId)
      await router.push(applicationPath.value)
  } catch (e) {
    error.value = e.message || 'Failed to revoke key. Try again.'
    revokeConfirm.value = null
  }
}

function openKey(key) {
  router.push(`${applicationPath.value}/keys/${key.id}`)
}

function closeKey() {
  router.push(applicationPath.value)
}

watch([() => route.params.keyId, () => route.name, keys], () => {
  cancelEdit()
  viewingKey.value = null
  const key = keys.value.find((k) => k.id === Number(route.params.keyId))
  emit('selected-key', key ?? null)
  if (key) {
    if (isAdmin.value && key.is_active) fillKeyForm(key)
    else viewingKey.value = key
  }
})

onMounted(async () => {
  await Promise.all([
    loadKeys(props.application.id),
    loadModels(),
    loadKeySpend(props.application.id)
  ])
})
</script>
