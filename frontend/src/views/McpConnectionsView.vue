<template>
  <main class="max-w-3xl mx-auto px-4 py-8">
    <h1 class="text-2xl font-bold mb-2">MCP Connections</h1>
    <p class="text-sm text-gray-600 dark:text-gray-300 mb-6">
      Manage your upstream keys and issue bearer tokens for your MCP clients.
    </p>

    <p v-if="error" role="alert" class="text-sm text-red-700 dark:text-red-300 mb-4">{{ error }}</p>
    <p v-if="loading" role="status">Loading connections…</p>
    <p v-else-if="!error && !connections.length">No MCP servers are assigned to your account.</p>

    <section v-for="connection in connections" :key="connection.server" class="card p-5 mb-4">
      <h2 class="font-semibold">{{ connection.server }}</h2>
      <p class="text-sm break-all mt-1"><code>{{ endpointURL(connection.endpoint) }}</code></p>

      <form v-if="connection.credential_scope === 'user'" class="mt-4" @submit.prevent="saveKey(connection.server)">
        <label :for="`mcp-key-${connection.server}`" class="block text-sm font-medium mb-1">
          {{ connection.key_set ? 'Replace upstream API key' : 'Upstream API key' }}
        </label>
        <div class="flex flex-wrap gap-2">
          <input
            :id="`mcp-key-${connection.server}`"
            v-model="keys[connection.server]"
            class="input flex-1"
            type="password"
            autocomplete="off"
            required
          />
          <button class="btn-secondary" :disabled="busy">Save key</button>
        </div>
      </form>

      <p v-if="hasToken(connection)" class="mt-4 text-sm">
        Gateway token expires {{ new Date(connection.token_expires * 1000).toLocaleString() }}.
      </p>
      <div class="flex flex-wrap gap-2 mt-4">
        <button
          type="button"
          class="btn-primary"
          :disabled="busy || (connection.credential_scope === 'user' && !connection.key_set)"
          @click="mintToken(connection.server)"
        >{{ hasToken(connection) ? 'Rotate token' : 'Issue token' }}</button>
        <button v-if="hasToken(connection)" type="button" class="btn-secondary" :disabled="busy"
          @click="update(() => api.revokeMcpToken(connection.server))">Revoke token</button>
        <button v-if="connection.key_set" type="button" class="btn-secondary" :disabled="busy"
          @click="update(() => api.disconnectMcp(connection.server))">Disconnect</button>
      </div>

      <div v-if="issued?.server === connection.server" class="mt-4 space-y-2" role="status">
        <p class="text-sm">Copy this gateway token now. It is shown once.</p>
        <input aria-label="New gateway token" class="input w-full font-mono" :value="issued.token" readonly />
        <button type="button" class="btn-secondary" @click="issued = null">Hide token</button>
      </div>
    </section>
  </main>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { api } from '../api/client.js'

const connections = ref([])
const keys = reactive(Object.create(null))
const issued = ref(null)
const loading = ref(true)
const busy = ref(false)
const error = ref('')

function endpointURL(path) {
  return new URL(path, window.location.origin).href
}

function hasToken(connection) {
  return connection.token_expires * 1000 > Date.now()
}

async function refresh() {
  connections.value = (await api.mcpConnections()) ?? []
}

onMounted(async () => {
  try {
    await refresh()
  } catch (err) {
    error.value = err.message
  } finally {
    loading.value = false
  }
})

async function update(action) {
  busy.value = true
  error.value = ''
  issued.value = null
  try {
    await action()
    await refresh()
  } catch (err) {
    error.value = err.message
  } finally {
    busy.value = false
  }
}

function saveKey(server) {
  return update(async () => {
    await api.saveMcpKey(server, keys[server])
    keys[server] = ''
  })
}

function mintToken(server) {
  return update(async () => {
    const result = await api.mintMcpToken(server)
    if (result) issued.value = { server, token: result.token }
  })
}
</script>
