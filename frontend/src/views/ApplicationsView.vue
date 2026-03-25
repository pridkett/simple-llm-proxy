<template>
  <div class="p-6">
    <h1 class="text-2xl font-bold text-gray-900 mb-6">Applications</h1>

    <!-- Team selector -->
    <div class="mb-6">
      <label for="team-select" class="block text-sm font-medium text-gray-700 mb-1">Filter by Team</label>
      <select
        id="team-select"
        v-model="selectedTeamId"
        @change="handleTeamChange"
        class="border border-gray-300 rounded-md px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
      >
        <option value="">Select a team...</option>
        <option v-for="team in teams" :key="team.id" :value="String(team.id)">
          {{ team.name }}
        </option>
      </select>
    </div>

    <!-- Create application form (shown when team is selected, admin only) -->
    <form
      v-if="selectedTeamId && currentUser?.is_admin"
      @submit.prevent="handleCreateApplication"
      class="mb-6 flex gap-3 items-end"
    >
      <div>
        <label class="block text-xs text-gray-500 mb-1">Application Name</label>
        <input
          v-model="newAppName"
          type="text"
          placeholder="Application name"
          class="border border-gray-300 rounded-md px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
        />
      </div>
      <button
        type="submit"
        class="px-4 py-2 bg-indigo-600 text-white text-sm font-medium rounded-md hover:bg-indigo-700 transition-colors"
      >
        Create
      </button>
    </form>

    <!-- Applications list -->
    <div v-if="loadingApps" class="text-gray-500 text-sm">Loading...</div>
    <div v-else-if="appsError" class="text-red-600 text-sm">{{ appsError }}</div>
    <div v-else-if="!selectedTeamId" class="text-gray-400 text-sm">Select a team to view applications.</div>
    <table v-else class="min-w-full divide-y divide-gray-200">
      <thead class="bg-gray-50">
        <tr>
          <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Name</th>
          <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Team</th>
          <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Created</th>
          <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Actions</th>
        </tr>
      </thead>
      <tbody class="bg-white divide-y divide-gray-200">
        <tr v-for="app in applications" :key="app.id">
          <td class="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-900">{{ app.name }}</td>
          <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{{ teamName(app.team_id) }}</td>
          <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
            {{ new Date(app.created_at).toLocaleDateString() }}
          </td>
          <td class="px-6 py-4 whitespace-nowrap text-sm">
            <template v-if="pendingDeleteAppId === app.id">
              <span class="text-xs text-gray-500 mr-1">Delete {{ app.name }}?</span>
              <button
                :data-testid="`confirm-delete-app-${app.id}`"
                @click="confirmDeleteApp(app.id)"
                class="text-xs text-red-600 hover:text-red-800 mr-1 font-medium"
              >Yes</button>
              <button
                @click="pendingDeleteAppId = null"
                class="text-xs text-gray-500 hover:text-gray-700"
              >No</button>
            </template>
            <button
              v-else
              :data-testid="`delete-app-${app.id}`"
              @click="startDeleteApp(app.id)"
              class="text-xs text-red-500 hover:text-red-700"
            >Delete</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api/client.js'
import { useSession } from '../composables/useSession.js'

const { currentUser } = useSession()

const teams = ref([])
const selectedTeamId = ref('')
const applications = ref([])
const loadingApps = ref(false)
const appsError = ref(null)
const newAppName = ref('')
const pendingDeleteAppId = ref(null)

function teamName(teamId) {
  const team = teams.value.find(t => t.id === teamId)
  return team?.name || String(teamId)
}

async function loadApplications(teamId) {
  if (!teamId) {
    applications.value = []
    return
  }
  loadingApps.value = true
  appsError.value = null
  try {
    applications.value = await api.applications(Number(teamId))
  } catch (e) {
    appsError.value = e.message
  } finally {
    loadingApps.value = false
  }
}

function handleTeamChange() {
  pendingDeleteAppId.value = null
  loadApplications(selectedTeamId.value)
}

async function handleCreateApplication() {
  if (!newAppName.value.trim() || !selectedTeamId.value) return
  try {
    await api.createApplication({
      team_id: Number(selectedTeamId.value),
      name: newAppName.value.trim(),
    })
    newAppName.value = ''
    await loadApplications(selectedTeamId.value)
  } catch (e) {
    appsError.value = e.message
  }
}

function startDeleteApp(appId) {
  pendingDeleteAppId.value = appId
}

async function confirmDeleteApp(appId) {
  try {
    await api.deleteApplication(appId)
    pendingDeleteAppId.value = null
    await loadApplications(selectedTeamId.value)
  } catch (e) {
    appsError.value = e.message
  }
}

onMounted(async () => {
  try {
    teams.value = await api.teams()
  } catch (e) {
    appsError.value = e.message
  }
})
</script>
