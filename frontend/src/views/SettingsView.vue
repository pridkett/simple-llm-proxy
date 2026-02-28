<template>
  <div class="max-w-lg mx-auto px-4 sm:px-6 lg:px-8 py-8">
    <h1 class="text-2xl font-bold text-gray-900 mb-6">Settings</h1>

    <!-- API Key card -->
    <div class="card p-6 mb-4">
      <h2 class="text-base font-semibold text-gray-900 mb-1">API Key</h2>
      <p class="text-sm text-gray-500 mb-4">
        Enter your proxy master key. It is stored in browser localStorage and used to
        authenticate all admin requests.
      </p>

      <form @submit.prevent="save">
        <div class="flex gap-2">
          <input
            v-model="keyInput"
            :type="showKey ? 'text' : 'password'"
            class="input flex-1"
            placeholder="sk-…"
            autocomplete="off"
          />
          <button
            type="button"
            class="btn-secondary"
            @click="showKey = !showKey"
            :title="showKey ? 'Hide key' : 'Show key'"
          >
            <svg v-if="showKey" class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                d="M13.875 18.825A10.05 10.05 0 0112 19c-4.478 0-8.268-2.943-9.543-7a9.97 9.97 0 011.563-3.029m5.858.908a3 3 0 114.243 4.243M9.878 9.878l4.242 4.242M9.88 9.88l-3.29-3.29m7.532 7.532l3.29 3.29M3 3l3.59 3.59m0 0A9.953 9.953 0 0112 5c4.478 0 8.268 2.943 9.542 7a10.025 10.025 0 01-4.132 5.411m0 0L21 21" />
            </svg>
            <svg v-else class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />
            </svg>
          </button>
        </div>

        <div class="flex gap-2 mt-3">
          <button type="submit" class="btn-primary">Save</button>
          <button type="button" class="btn-secondary" @click="clear">Clear</button>
        </div>
      </form>

      <div v-if="saved" class="mt-3 text-sm text-green-700 font-medium">Key saved.</div>
    </div>

    <!-- Connection test -->
    <div class="card p-6">
      <h2 class="text-base font-semibold text-gray-900 mb-1">Connection Test</h2>
      <p class="text-sm text-gray-500 mb-4">Verify the proxy is reachable and your key is valid.</p>

      <button class="btn-primary" :disabled="testing" @click="testConnection">
        {{ testing ? 'Testing…' : 'Test Connection' }}
      </button>

      <div v-if="testResult" class="mt-3">
        <div
          v-if="testResult.ok"
          class="text-sm text-green-700 font-medium"
        >
          Connected. Uptime: {{ testResult.uptime }}s, {{ testResult.models }} model(s) configured.
        </div>
        <div v-else class="text-sm text-red-700 font-medium">
          {{ testResult.error }}
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useAuth } from '../composables/useAuth.js'
import { api } from '../api/client.js'

const { apiKey, setApiKey, clearApiKey } = useAuth()

const keyInput = ref(apiKey.value)
const showKey = ref(false)
const saved = ref(false)
const testing = ref(false)
const testResult = ref(null)

function save() {
  setApiKey(keyInput.value)
  saved.value = true
  setTimeout(() => { saved.value = false }, 2000)
}

function clear() {
  keyInput.value = ''
  clearApiKey()
}

async function testConnection() {
  testing.value = true
  testResult.value = null
  try {
    const status = await api.status()
    testResult.value = {
      ok: true,
      uptime: status.uptime_seconds,
      models: status.models?.length ?? 0,
    }
  } catch (e) {
    testResult.value = { ok: false, error: e.message }
  } finally {
    testing.value = false
  }
}
</script>
