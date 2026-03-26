<template>
  <div class="p-6">
    <h1 class="text-2xl font-bold text-gray-900 mb-6">Applications</h1>

    <div class="flex gap-6">
      <!-- Left panel: team list -->
      <div class="w-56 flex-shrink-0">
        <!-- Filter input -->
        <div class="mb-3">
          <input
            v-model="teamSearch"
            type="text"
            placeholder="Filter teams"
            autocomplete="off"
            data-1p-ignore
            data-lpignore="true"
            class="w-full border border-gray-300 rounded-md px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
          />
        </div>

        <div class="bg-white border border-gray-200 rounded-lg overflow-hidden">
          <div class="px-3 py-2 text-xs font-semibold text-gray-400 uppercase tracking-wide border-b border-gray-200 bg-gray-50">
            Teams
          </div>
          <div v-if="loadingTeams" class="px-3 py-3 text-gray-500 text-sm">Loading...</div>
          <div v-else-if="teamsError" class="px-3 py-3 text-red-600 text-sm">{{ teamsError }}</div>
          <ul v-else>
            <li
              v-for="team in filteredTeams"
              :key="team.id"
              class="px-3 py-2 cursor-pointer text-sm border-b border-gray-100 last:border-0"
              :class="selectedTeam?.id === team.id ? 'bg-indigo-50 text-indigo-700 font-medium' : 'text-gray-700 hover:bg-gray-50'"
              @click="selectTeam(team)"
            >
              {{ team.name }}
            </li>
            <li v-if="filteredTeams.length === 0" class="px-3 py-3 text-sm text-gray-400 italic">
              No teams found
            </li>
          </ul>
        </div>
      </div>

      <!-- Right panel: applications for selected team -->
      <div v-if="selectedTeam" class="flex-1">
        <h2 class="text-lg font-semibold text-gray-900 mb-4">{{ selectedTeam.name }} — Applications</h2>

        <div v-if="loadingApps" class="text-gray-500 text-sm">Loading applications...</div>
        <div v-else>
          <!-- Applications table -->
          <table class="min-w-full divide-y divide-gray-200 mb-6">
            <thead class="bg-gray-50">
              <tr>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Name</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Created</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Keys</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Actions</th>
              </tr>
            </thead>
            <tbody class="bg-white divide-y divide-gray-200">
              <tr v-if="applications.length === 0">
                <td colspan="4" class="px-4 py-8 text-center text-sm text-gray-400 italic">
                  No applications yet — create the first one below
                </td>
              </tr>
              <tr v-for="app in applications" :key="app.id">
                <td class="px-4 py-3 text-sm font-medium text-gray-900">{{ app.name }}</td>
                <td class="px-4 py-3 text-sm text-gray-500">
                  {{ new Date(app.created_at).toLocaleDateString() }}
                </td>
                <td class="px-4 py-3 text-sm">
                  <button
                    @click="goToKeys(app)"
                    class="text-indigo-600 hover:text-indigo-800 hover:underline tabular-nums"
                  >{{ appKeyCounts[app.id] ?? 0 }} {{ (appKeyCounts[app.id] ?? 0) === 1 ? 'key' : 'keys' }}</button>
                </td>
                <td class="px-4 py-3 text-sm">
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

          <!-- Inline error banner -->
          <div v-if="appsError" class="mb-4 px-3 py-2 bg-red-50 border border-red-200 rounded text-sm text-red-600 flex justify-between items-center">
            <span>{{ appsError }}</span>
            <button @click="appsError = null" class="ml-3 text-red-400 hover:text-red-600">✕</button>
          </div>

          <!-- Create application form (admin only) -->
          <div v-if="currentUser?.is_admin" class="border-t border-gray-200 pt-4">
            <h3 class="text-sm font-medium text-gray-700 mb-3">New Application</h3>
            <form @submit.prevent="handleCreateApplication" class="flex items-end gap-3">
              <div>
                <label class="block text-xs text-gray-500 mb-1">Name</label>
                <input
                  v-model="newAppName"
                  type="text"
                  placeholder="Application name"
                  autocomplete="off"
                  data-1p-ignore
                  data-lpignore="true"
                  class="w-64 border border-gray-300 rounded-md px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
                />
              </div>
              <button
                type="submit"
                :disabled="!newAppName.trim()"
                class="px-4 py-2 bg-indigo-600 text-white text-sm font-medium rounded-md hover:bg-indigo-700 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
              >Create</button>
            </form>
          </div>
        </div>
      </div>

      <div v-else class="flex-1 flex items-center justify-center text-gray-400 text-sm">
        Select a team to view applications
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api/client.js'
import { useSession } from '../composables/useSession.js'

const { currentUser } = useSession()
const router = useRouter()

const allTeams = ref([])
const appKeyCounts = ref({})
const teamSearch = ref('')
const loadingTeams = ref(true)
const teamsError = ref(null)

const selectedTeam = ref(null)
const applications = ref([])
const loadingApps = ref(false)
const appsError = ref(null)
const newAppName = ref('')
const pendingDeleteAppId = ref(null)

const filteredTeams = computed(() => {
  const q = teamSearch.value.toLowerCase().trim()
  if (!q) return allTeams.value
  return allTeams.value.filter((t) => t.name.toLowerCase().includes(q))
})

async function selectTeam(team) {
  selectedTeam.value = team
  pendingDeleteAppId.value = null
  appsError.value = null
  await loadApplications(team.id)
}

async function loadApplications(teamId) {
  loadingApps.value = true
  appsError.value = null
  try {
    applications.value = await api.applications(teamId) ?? []
    // Fetch key counts for all apps in parallel (non-blocking — counts are cosmetic)
    const counts = await Promise.all(
      applications.value.map((app) =>
        api.apiKeys(app.id).then((keys) => ({ id: app.id, count: (keys ?? []).length })).catch(() => ({ id: app.id, count: 0 }))
      )
    )
    appKeyCounts.value = Object.fromEntries(counts.map(({ id, count }) => [id, count]))
  } catch (e) {
    appsError.value = e.message
  } finally {
    loadingApps.value = false
  }
}

async function handleCreateApplication() {
  if (!newAppName.value.trim() || !selectedTeam.value) return
  appsError.value = null
  try {
    await api.createApplication({
      team_id: selectedTeam.value.id,
      name: newAppName.value.trim(),
    })
    newAppName.value = ''
    await loadApplications(selectedTeam.value.id)
  } catch (e) {
    appsError.value = e.message
  }
}

function goToKeys(app) {
  router.push({ path: '/keys', query: { team_id: selectedTeam.value?.id, app_id: app.id } })
}

function startDeleteApp(appId) {
  pendingDeleteAppId.value = appId
}

async function confirmDeleteApp(appId) {
  appsError.value = null
  try {
    await api.deleteApplication(appId)
    pendingDeleteAppId.value = null
    await loadApplications(selectedTeam.value.id)
  } catch (e) {
    appsError.value = e.message
  }
}

onMounted(async () => {
  loadingTeams.value = true
  try {
    if (currentUser.value?.is_admin) {
      // Admins see all teams so they can manage apps even if not a member
      allTeams.value = await api.teams() ?? []
    } else {
      // Non-admins see only teams they belong to
      const memberships = await api.myTeams() ?? []
      allTeams.value = memberships.map((m) => ({ id: m.team_id, name: m.team_name }))
    }
  } catch (e) {
    teamsError.value = e.message
  } finally {
    loadingTeams.value = false
  }
})
</script>
