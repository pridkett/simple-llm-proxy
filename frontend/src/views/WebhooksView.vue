<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-semibold text-gray-900">Webhooks</h1>
      <button class="btn-primary text-sm" @click="startCreate" :disabled="editingId !== null">Add Webhook</button>
    </div>

    <LoadingSpinner v-if="loading" />
    <ErrorAlert v-else-if="error" title="Failed to load webhooks" :message="error" />

    <div v-else-if="webhooks && webhooks.length === 0 && editingId === null" class="card">
      <div class="px-6 py-12 text-center">
        <h3 class="text-base font-semibold text-gray-900">No webhooks configured</h3>
        <p class="mt-2 text-sm text-gray-500">Webhooks notify external services when routing events occur. Click "Add Webhook" to create one, or define webhooks in your YAML configuration file.</p>
      </div>
    </div>

    <div class="card overflow-hidden" v-else-if="webhooks">
      <div class="overflow-x-auto">
        <table class="min-w-full divide-y divide-gray-100 text-sm">
          <thead class="bg-gray-50">
            <tr>
              <th class="px-6 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wider">URL</th>
              <th class="px-6 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wider">Events</th>
              <th class="px-6 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wider">Source</th>
              <th class="px-6 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wider">Status</th>
              <th class="px-6 py-3 text-right text-xs font-semibold text-gray-500 uppercase tracking-wider">Actions</th>
            </tr>
          </thead>
          <tbody class="bg-white divide-y divide-gray-50">
            <!-- Create form row (when editingId === -1) -->
            <tr v-if="editingId === -1">
              <td colspan="5" class="px-6 py-4 bg-gray-50 border-b border-gray-100">
                <div class="space-y-3">
                  <div>
                    <label class="block text-xs font-semibold text-gray-500 uppercase tracking-wider mb-1">URL</label>
                    <input v-model="form.url" type="url" class="input text-sm w-full" placeholder="https://example.com/webhook" />
                  </div>
                  <div>
                    <label class="block text-xs font-semibold text-gray-500 uppercase tracking-wider mb-1">Events</label>
                    <div class="flex gap-4">
                      <label class="flex items-center gap-2 text-sm text-gray-700">
                        <input type="checkbox" value="provider_failover" v-model="form.events" class="rounded border-gray-300 text-indigo-600 focus:ring-indigo-500" />
                        Provider Failover
                      </label>
                      <label class="flex items-center gap-2 text-sm text-gray-700">
                        <input type="checkbox" value="budget_exhausted" v-model="form.events" class="rounded border-gray-300 text-indigo-600 focus:ring-indigo-500" />
                        Budget Exhausted
                      </label>
                      <label class="flex items-center gap-2 text-sm text-gray-700">
                        <input type="checkbox" value="pool_cooldown" v-model="form.events" class="rounded border-gray-300 text-indigo-600 focus:ring-indigo-500" />
                        Pool Cooldown
                      </label>
                    </div>
                  </div>
                  <div>
                    <label class="block text-xs font-semibold text-gray-500 uppercase tracking-wider mb-1">Secret</label>
                    <input v-model="form.secret" type="password" class="input text-sm w-full" placeholder="Enter shared secret for HMAC signing" />
                  </div>
                  <div>
                    <label class="flex items-center gap-2 text-sm text-gray-700">
                      <input type="checkbox" v-model="form.enabled" class="rounded border-gray-300 text-indigo-600 focus:ring-indigo-500" />
                      Enabled
                    </label>
                  </div>
                  <p v-if="formError" class="text-red-600 text-xs">{{ formError }}</p>
                </div>
                <div class="flex gap-2 mt-3">
                  <button class="btn-primary text-xs" @click="save" :disabled="saving">
                    {{ saving ? 'Saving Webhook...' : 'Save Webhook' }}
                  </button>
                  <button class="btn-secondary text-xs" @click="cancelEdit">Discard Changes</button>
                </div>
              </td>
            </tr>

            <template v-for="wh in webhooks" :key="wh.id">
              <!-- Webhook data row -->
              <tr class="hover:bg-gray-50 transition-colors">
                <td class="px-6 py-3 font-mono text-xs truncate max-w-xs">{{ wh.url }}</td>
                <td class="px-6 py-3 text-gray-600 text-xs">{{ wh.events.join(', ') }}</td>
                <td class="px-6 py-3">
                  <span v-if="wh.source === 'yaml'" class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-gray-100 text-gray-600">YAML</span>
                  <span v-else class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-indigo-50 text-indigo-700">UI</span>
                </td>
                <td class="px-6 py-3 text-gray-600 text-xs">{{ wh.enabled ? 'Enabled' : 'Disabled' }}</td>
                <td class="px-6 py-3 text-right">
                  <template v-if="!wh.read_only">
                    <template v-if="confirmingDeleteId === wh.id">
                      <span class="text-xs text-gray-600 mr-2">Are you sure?</span>
                      <button class="text-xs text-red-600 hover:text-red-800 font-medium mr-2" @click="confirmDelete(wh.id)">Yes, delete</button>
                      <button class="text-xs text-gray-600 hover:text-gray-800 font-medium" @click="confirmingDeleteId = null">Keep Webhook</button>
                    </template>
                    <template v-else>
                      <button class="text-xs text-indigo-600 hover:text-indigo-800 font-medium mr-3" @click="startEdit(wh)">Edit</button>
                      <button class="text-xs text-red-600 hover:text-red-800 font-medium" @click="confirmingDeleteId = wh.id">Delete</button>
                    </template>
                  </template>
                </td>
              </tr>

              <!-- Edit form row (shown below the webhook being edited) -->
              <tr v-if="editingId === wh.id">
                <td colspan="5" class="px-6 py-4 bg-gray-50 border-b border-gray-100">
                  <div class="space-y-3">
                    <div>
                      <label class="block text-xs font-semibold text-gray-500 uppercase tracking-wider mb-1">URL</label>
                      <input v-model="form.url" type="url" class="input text-sm w-full" placeholder="https://example.com/webhook" />
                    </div>
                    <div>
                      <label class="block text-xs font-semibold text-gray-500 uppercase tracking-wider mb-1">Events</label>
                      <div class="flex gap-4">
                        <label class="flex items-center gap-2 text-sm text-gray-700">
                          <input type="checkbox" value="provider_failover" v-model="form.events" class="rounded border-gray-300 text-indigo-600 focus:ring-indigo-500" />
                          Provider Failover
                        </label>
                        <label class="flex items-center gap-2 text-sm text-gray-700">
                          <input type="checkbox" value="budget_exhausted" v-model="form.events" class="rounded border-gray-300 text-indigo-600 focus:ring-indigo-500" />
                          Budget Exhausted
                        </label>
                        <label class="flex items-center gap-2 text-sm text-gray-700">
                          <input type="checkbox" value="pool_cooldown" v-model="form.events" class="rounded border-gray-300 text-indigo-600 focus:ring-indigo-500" />
                          Pool Cooldown
                        </label>
                      </div>
                    </div>
                    <div>
                      <label class="block text-xs font-semibold text-gray-500 uppercase tracking-wider mb-1">Secret</label>
                      <input v-model="form.secret" type="password" class="input text-sm w-full" placeholder="Enter shared secret for HMAC signing" />
                    </div>
                    <div>
                      <label class="flex items-center gap-2 text-sm text-gray-700">
                        <input type="checkbox" v-model="form.enabled" class="rounded border-gray-300 text-indigo-600 focus:ring-indigo-500" />
                        Enabled
                      </label>
                    </div>
                    <p v-if="formError" class="text-red-600 text-xs">{{ formError }}</p>
                  </div>
                  <div class="flex gap-2 mt-3">
                    <button class="btn-primary text-xs" @click="save" :disabled="saving">
                      {{ saving ? 'Saving Webhook...' : 'Save Webhook' }}
                    </button>
                    <button class="btn-secondary text-xs" @click="cancelEdit">Discard Changes</button>
                  </div>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { api } from '../api/client.js'
import LoadingSpinner from '../components/LoadingSpinner.vue'
import ErrorAlert from '../components/ErrorAlert.vue'

const webhooks = ref(null)
const loading = ref(false)
const error = ref('')
const editingId = ref(null) // null = not editing, -1 = creating, >0 = editing existing
const confirmingDeleteId = ref(null)
const saving = ref(false)
const formError = ref('')
const form = reactive({
  url: '',
  events: [],
  secret: '',
  enabled: true,
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await api.webhooks()
    webhooks.value = data.webhooks || []
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

onMounted(load)

function startCreate() {
  editingId.value = -1
  Object.assign(form, { url: '', events: [], secret: '', enabled: true })
  formError.value = ''
  confirmingDeleteId.value = null
}

function startEdit(wh) {
  editingId.value = wh.id
  Object.assign(form, { url: wh.url, events: [...wh.events], secret: '', enabled: wh.enabled })
  formError.value = ''
  confirmingDeleteId.value = null
}

function cancelEdit() {
  editingId.value = null
  formError.value = ''
}

async function save() {
  saving.value = true
  formError.value = ''
  const payload = {
    url: form.url,
    events: form.events,
    secret: form.secret,
    enabled: form.enabled,
  }
  try {
    if (editingId.value === -1) {
      await api.createWebhook(payload)
    } else {
      await api.updateWebhook(editingId.value, payload)
    }
    editingId.value = null
    await load()
  } catch (e) {
    formError.value = e.message
  } finally {
    saving.value = false
  }
}

async function confirmDelete(id) {
  try {
    await api.deleteWebhook(id)
    confirmingDeleteId.value = null
    await load()
  } catch (e) {
    formError.value = e.message
  }
}
</script>
