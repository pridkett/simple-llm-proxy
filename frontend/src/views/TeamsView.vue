<template>
  <div class="p-4 sm:p-6 max-w-7xl mx-auto">
    <nav v-if="breadcrumbs.length" aria-label="Breadcrumb" class="mb-6">
      <ol class="flex flex-wrap items-center gap-2 text-sm">
        <li
          v-for="(crumb, index) in breadcrumbs"
          :key="index"
          class="flex items-center gap-2"
        >
          <span v-if="index" aria-hidden="true" class="text-gray-400">›</span>
          <RouterLink
            v-if="crumb.to"
            :to="crumb.to"
            class="text-indigo-600 dark:text-indigo-400 hover:underline"
            >{{ crumb.label }}</RouterLink
          >
          <span v-else aria-current="page" class="text-gray-900 dark:text-gray-100">{{
            crumb.label
          }}</span>
        </li>
      </ol>
    </nav>

    <p v-if="loadingTeams" role="status" class="text-sm text-gray-500 dark:text-gray-400">
      Loading teams…
    </p>
    <div v-else-if="teamsError" role="alert" class="text-sm text-red-600 dark:text-red-400">
      {{ teamsError }}
      <button class="underline ml-2" @click="loadContext">Retry</button>
    </div>
    <template v-else-if="isOverview">
      <div class="flex flex-wrap items-center justify-between gap-3 mb-6">
        <h1 class="text-2xl font-semibold text-gray-900 dark:text-gray-100">Teams</h1>
        <RouterLink v-if="isAdmin" to="/teams/new" class="btn btn-primary"
          >Create team</RouterLink
        >
      </div>
      <p v-if="actionError" role="alert" class="mb-4 text-sm text-red-600 dark:text-red-400">
        {{ actionError }}
      </p>
      <div class="overflow-x-auto">
        <table class="min-w-full text-sm text-left divide-y divide-gray-200 dark:divide-gray-700">
          <thead class="bg-gray-50 dark:bg-gray-900 text-gray-500 dark:text-gray-400">
            <tr>
              <th class="px-4 py-3 font-medium">Name</th>
              <th class="px-4 py-3 font-medium">Applications</th>
              <th class="px-4 py-3 font-medium">Active keys</th>
              <th v-if="isAdmin" class="px-4 py-3 font-medium">Members</th>
              <th v-if="isAdmin" class="px-4 py-3">
                <span class="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody class="bg-white dark:bg-gray-800 divide-y divide-gray-200 dark:divide-gray-700">
            <tr v-for="team in teams" :key="team.id">
              <td class="px-4 py-4">
                <RouterLink
                  :to="teamPath(team.id)"
                  class="font-medium text-indigo-600 dark:text-indigo-400 hover:underline"
                  >{{ team.name }}</RouterLink
                >
              </td>
              <td class="px-4 py-4">
                {{ teamStats[team.id]?.applications ?? '—' }}
              </td>
              <td class="px-4 py-4">{{ teamStats[team.id]?.keys ?? '—' }}</td>
              <td v-if="isAdmin" class="px-4 py-4">
                {{ teamStats[team.id]?.members ?? '—' }}
              </td>
              <td v-if="isAdmin" class="px-4 py-4">
                <template v-if="pendingDeleteTeam === team.id">
                  <p class="mb-2">
                    Delete {{ team.name }} and all its applications and keys?
                  </p>
                  <button
                    class="text-red-600 dark:text-red-400 mr-3"
                    :disabled="busy"
                    @click="deleteTeam(team)"
                  >
                    Delete
                  </button>
                  <button
                    class="text-gray-600 dark:text-gray-300"
                    :disabled="busy"
                    @click="pendingDeleteTeam = null"
                  >
                    Cancel
                  </button>
                </template>
                <button
                  v-else
                  class="text-red-600 dark:text-red-400"
                  :aria-label="`Delete ${team.name}`"
                  @click="pendingDeleteTeam = team.id"
                >
                  Delete
                </button>
              </td>
            </tr>
            <tr v-if="!teams.length">
              <td :colspan="isAdmin ? 5 : 3" class="px-4 py-8 text-gray-500 dark:text-gray-400">
                No teams yet.
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <template v-else-if="isNewTeam">
      <h1 class="text-2xl font-semibold text-gray-900 dark:text-gray-100 mb-6">Create team</h1>
      <form
        v-if="isAdmin"
        class="card p-5 max-w-lg space-y-4"
        @submit.prevent="createTeam"
      >
        <label class="block text-sm text-gray-700 dark:text-gray-200" for="team-name">Name</label>
        <input
          id="team-name"
          ref="nameInput"
          v-model="newTeamName"
          required
          autocomplete="off"
          class="input"
        />
        <p v-if="actionError" role="alert" class="text-sm text-red-600 dark:text-red-400">
          {{ actionError }}
        </p>
        <div class="flex items-center gap-3">
          <button
            class="btn btn-primary"
            :disabled="busy || !newTeamName.trim()"
          >
            Create team</button
          ><RouterLink to="/teams" class="text-sm text-gray-600 dark:text-gray-300"
            >Cancel</RouterLink
          >
        </div>
      </form>
      <p v-else role="alert" class="text-sm text-gray-600 dark:text-gray-300">
        An administrator can create teams.
      </p>
    </template>

    <template v-else-if="currentTeam">
      <template v-if="isNewApplication">
        <h1 class="text-2xl font-semibold text-gray-900 dark:text-gray-100 mb-6">
          Create application
        </h1>
        <form
          v-if="isAdmin"
          class="card p-5 max-w-lg space-y-4"
          @submit.prevent="createApplication"
        >
          <label class="block text-sm text-gray-700 dark:text-gray-200" for="application-name"
            >Name</label
          >
          <input
            id="application-name"
            ref="nameInput"
            v-model="newAppName"
            required
            :readonly="!!partialApplication"
            autocomplete="off"
            class="input"
          />
          <p v-if="actionError" role="alert" class="text-sm text-red-600 dark:text-red-400">
            {{ actionError }}
          </p>
          <p v-if="partialApplication" class="text-sm text-gray-600 dark:text-gray-300">
            The application exists. You can retry key creation or open its keys
            to check the result.
          </p>
          <div class="flex flex-wrap items-center gap-3">
            <button
              class="btn btn-primary"
              :disabled="busy || !newAppName.trim()"
            >
              {{
                partialApplication ? 'Retry key creation' : 'Create application'
              }}
            </button>
            <RouterLink
              :to="
                partialApplication
                  ? applicationPath(partialApplication.id)
                  : teamPath(currentTeam.id)
              "
              class="text-sm text-gray-600 dark:text-gray-300"
              >{{
                partialApplication ? 'Open application' : 'Cancel'
              }}</RouterLink
            >
          </div>
        </form>
        <p v-else role="alert" class="text-sm text-gray-600 dark:text-gray-300">
          An administrator can create applications.
        </p>
      </template>
      <template v-else>
        <div class="flex flex-wrap items-center justify-between gap-4">
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-gray-100">
            {{ currentTeam.name }}
          </h1>
          <label v-if="teams.length > 1" class="text-sm text-gray-600 dark:text-gray-300"
            >Switch team
            <select
              :value="currentTeam.id"
              aria-label="Switch team"
              class="ml-2 border border-gray-300 dark:border-gray-600 rounded-md px-3 py-2 bg-white dark:bg-gray-800"
              @change="router.push(teamPath(Number($event.target.value)))"
            >
              <option v-for="team in teams" :key="team.id" :value="team.id">
                {{ team.name }}
              </option>
            </select>
          </label>
        </div>
        <nav
          aria-label="Team sections"
          class="flex gap-6 border-b border-gray-200 dark:border-gray-700 mt-6 mb-6"
        >
          <RouterLink
            :to="teamPath(currentTeam.id)"
            class="pb-3 text-sm"
            :class="
              isMembers
                ? 'text-gray-600 dark:text-gray-300'
                : 'border-b-2 border-indigo-600 dark:border-indigo-400 text-indigo-600 dark:text-indigo-400'
            "
            :aria-current="!isMembers ? 'page' : undefined"
            >Applications</RouterLink
          >
          <RouterLink
            v-if="isAdmin"
            :to="`${teamPath(currentTeam.id)}/members`"
            class="pb-3 text-sm"
            :class="
              isMembers
                ? 'border-b-2 border-indigo-600 dark:border-indigo-400 text-indigo-600 dark:text-indigo-400'
                : 'text-gray-600 dark:text-gray-300'
            "
            :aria-current="isMembers ? 'page' : undefined"
            >Members</RouterLink
          >
        </nav>
        <TeamMembers
          v-if="isMembers && isAdmin"
          :key="currentTeam.id"
          :team="currentTeam"
        />
        <p v-else-if="isMembers" role="alert" class="text-sm text-gray-600 dark:text-gray-300">
          An administrator can manage team members.
        </p>
        <p v-else-if="loadingApps" role="status" class="text-sm text-gray-500 dark:text-gray-400">
          Loading applications…
        </p>
        <div v-else-if="appsError" role="alert" class="text-sm text-red-600 dark:text-red-400">
          {{ appsError }}
          <button
            class="underline ml-2"
            @click="loadApplications(currentTeam.id)"
          >
            Retry
          </button>
        </div>
        <p
          v-else-if="route.params.appId && !currentApplication"
          role="alert"
          class="text-sm text-gray-600 dark:text-gray-300"
        >
          Application not found in this team.
        </p>
        <div
          v-else-if="currentApplication"
          class="grid gap-6 md:grid-cols-[12rem_minmax(0,1fr)]"
        >
          <aside
            aria-label="Applications"
            class="border-b md:border-b-0 md:border-r border-gray-200 dark:border-gray-700 pb-4 md:pr-4"
          >
            <RouterLink
              v-for="app in applications"
              :key="app.id"
              :to="applicationPath(app.id)"
              class="block rounded-md px-3 py-3 text-sm mb-1"
              :class="
                app.id === currentApplication.id
                  ? 'bg-indigo-50 dark:bg-indigo-950 text-indigo-700 dark:text-indigo-300'
                  : 'text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-900'
              "
              :aria-current="
                app.id === currentApplication.id ? 'page' : undefined
              "
              >{{ app.name }}</RouterLink
            >
            <RouterLink
              v-if="isAdmin"
              :to="`${teamPath(currentTeam.id)}/applications/new`"
              class="inline-block mt-3 text-sm text-indigo-600 dark:text-indigo-400"
              >+ Create application</RouterLink
            >
          </aside>
          <ApplicationKeys
            :key="currentApplication.id"
            :application="currentApplication"
            @created="newKey = $event"
            @changed="refreshKeyCount"
            @selected-key="selectedKey = $event"
            @busy="keyBusy = $event"
          />
        </div>
        <template v-else>
          <div class="flex flex-wrap items-center justify-between gap-3 mb-4">
            <RouterLink
              v-if="isAdmin"
              :to="`${teamPath(currentTeam.id)}/applications/new`"
              class="btn btn-primary"
              >Create application</RouterLink
            >
          </div>
          <p v-if="actionError" role="alert" class="mb-4 text-sm text-red-600 dark:text-red-400">
            {{ actionError }}
          </p>
          <div class="overflow-x-auto">
            <table
              class="min-w-full text-sm text-left divide-y divide-gray-200 dark:divide-gray-700"
            >
              <thead class="bg-gray-50 dark:bg-gray-900 text-gray-500 dark:text-gray-400">
                <tr>
                  <th class="px-4 py-3 font-medium">Name</th>
                  <th class="px-4 py-3 font-medium">Active keys</th>
                  <th class="px-4 py-3 font-medium">Created</th>
                  <th v-if="isAdmin" class="px-4 py-3">
                    <span class="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody class="bg-white dark:bg-gray-800 divide-y divide-gray-200 dark:divide-gray-700">
                <tr v-for="app in applications" :key="app.id">
                  <td class="px-4 py-4">
                    <RouterLink
                      :to="applicationPath(app.id)"
                      class="text-indigo-600 dark:text-indigo-400 hover:underline"
                      >{{ app.name }}</RouterLink
                    >
                  </td>
                  <td class="px-4 py-4">{{ appKeyCounts[app.id] ?? '—' }}</td>
                  <td class="px-4 py-4">
                    {{ new Date(app.created_at).toLocaleDateString() }}
                  </td>
                  <td v-if="isAdmin" class="px-4 py-4">
                    <template v-if="pendingDeleteApp === app.id"
                      ><p class="mb-2">
                        Delete {{ app.name }} and all its keys?
                      </p>
                      <button
                        class="text-red-600 dark:text-red-400 mr-3"
                        :disabled="busy"
                        @click="deleteApplication(app)"
                      >
                        Delete</button
                      ><button
                        class="text-gray-600 dark:text-gray-300"
                        :disabled="busy"
                        @click="pendingDeleteApp = null"
                      >
                        Cancel
                      </button></template
                    >
                    <button
                      v-else
                      class="text-red-600 dark:text-red-400"
                      :aria-label="`Delete ${app.name}`"
                      @click="pendingDeleteApp = app.id"
                    >
                      Delete
                    </button>
                  </td>
                </tr>
                <tr v-if="!applications.length">
                  <td
                    :colspan="isAdmin ? 4 : 3"
                    class="px-4 py-8 text-gray-500 dark:text-gray-400"
                  >
                    No applications yet.
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </template>
      </template>
    </template>
    <p v-else role="alert" class="text-sm text-gray-600 dark:text-gray-300">Team not found.</p>
    <KeyCreatedDialog v-if="newKey" :value="newKey" @close="newKey = null" />
  </div>
</template>

<script setup>
import { ref, computed, watch, nextTick } from 'vue'
import {
  useRoute,
  useRouter,
  onBeforeRouteLeave,
  onBeforeRouteUpdate
} from 'vue-router'
import { api } from '../api/client.js'
import { useSession } from '../composables/useSession.js'
import TeamMembers from '../components/TeamMembers.vue'
import ApplicationKeys from '../components/ApplicationKeys.vue'
import KeyCreatedDialog from '../components/KeyCreatedDialog.vue'

const route = useRoute()
const router = useRouter()
const { currentUser } = useSession()
const isAdmin = computed(() => !!currentUser.value?.is_admin)
const teams = ref([])
const applications = ref([])
const teamStats = ref({})
const appKeyCounts = ref({})
const loadingTeams = ref(true)
const loadingApps = ref(false)
const teamsError = ref('')
const appsError = ref('')
const actionError = ref('')
const busy = ref(false)
const keyBusy = ref(false)
const newTeamName = ref('')
const newAppName = ref('')
const partialApplication = ref(null)
const newKey = ref(null)
const selectedKey = ref(null)
const pendingDeleteTeam = ref(null)
const pendingDeleteApp = ref(null)
const nameInput = ref(null)
const isOverview = computed(() => route.name === 'teams')
const isNewTeam = computed(() => route.name === 'team-new')
const isNewApplication = computed(() => route.name === 'team-application-new')
const isMembers = computed(() => route.name === 'team-members')
const currentTeam = computed(() =>
  teams.value.find((t) => t.id === Number(route.params.teamId))
)
const currentApplication = computed(() =>
  applications.value.find((a) => a.id === Number(route.params.appId))
)
const teamPath = (id) => `/teams/${id}`
const applicationPath = (id) =>
  `${teamPath(route.params.teamId)}/applications/${id}`
const breadcrumbs = computed(() => {
  if (isOverview.value) return []
  const crumbs = [{ label: 'Teams', to: '/teams' }]
  if (isNewTeam.value) return [...crumbs, { label: 'Create team' }]
  if (!currentTeam.value) return crumbs
  crumbs.push({
    label: currentTeam.value.name,
    to: teamPath(currentTeam.value.id)
  })
  if (isNewApplication.value) crumbs.push({ label: 'Create application' })
  else if (isMembers.value) crumbs.push({ label: 'Members' })
  else if (currentApplication.value) {
    crumbs.push({
      label: currentApplication.value.name,
      to: applicationPath(currentApplication.value.id)
    })
    if (route.name === 'team-key-new') crumbs.push({ label: 'Create key' })
    else if (route.params.keyId)
      crumbs.push({ label: selectedKey.value?.name ?? 'Key' })
  }
  delete crumbs[crumbs.length - 1].to
  return crumbs
})

let loadedTeams = false
let teamRequest
let contextVersion = 0
let applicationsTeamId = null
let appsRequestVersion = 0

async function loadTeams() {
  if (teamRequest) return teamRequest
  loadingTeams.value = true
  teamsError.value = ''
  teamRequest = (async () => {
    try {
      const result =
        (isAdmin.value ? await api.teams() : await api.myTeams()) ?? []
      teams.value = isAdmin.value
        ? result
        : result.map((m) => ({ id: m.team_id, name: m.team_name }))
      loadedTeams = true
    } catch (e) {
      teamsError.value = e.message
    } finally {
      loadingTeams.value = false
      teamRequest = null
    }
  })()
  return teamRequest
}

async function countKeys(apps) {
  const counts = await Promise.all(
    apps.map(async (a) => {
      try {
        return [
          a.id,
          ((await api.apiKeys(a.id)) ?? []).filter((k) => k.is_active).length
        ]
      } catch {
        return [a.id, null]
      }
    })
  )
  return Object.fromEntries(counts)
}

async function loadOverviewStats() {
  await Promise.all(
    teams.value.map(async (t) => {
      const [apps, members] = await Promise.allSettled([
        api.applications(t.id),
        isAdmin.value ? api.teamMembers(t.id) : Promise.resolve(null)
      ])
      let counts = null
      if (apps.status === 'fulfilled')
        counts = await countKeys(apps.value ?? [])
      teamStats.value[t.id] = {
        applications:
          apps.status === 'fulfilled' ? (apps.value ?? []).length : null,
        keys:
          counts && Object.values(counts).every((n) => n !== null)
            ? Object.values(counts).reduce((a, b) => a + b, 0)
            : null,
        members: members.status === 'fulfilled' ? members.value?.length : null
      }
    })
  )
}

async function loadApplications(teamId) {
  const version = ++appsRequestVersion
  loadingApps.value = true
  appsError.value = ''
  try {
    const result = (await api.applications(teamId)) ?? []
    if (
      version !== appsRequestVersion ||
      Number(route.params.teamId) !== teamId
    )
      return
    applications.value = result
    applicationsTeamId = teamId
    loadingApps.value = false
    const counts = await countKeys(result)
    if (version === appsRequestVersion)
      Object.assign(appKeyCounts.value, counts)
  } catch (e) {
    if (version === appsRequestVersion) appsError.value = e.message
  } finally {
    if (version === appsRequestVersion) loadingApps.value = false
  }
}

async function loadContext() {
  const version = ++contextVersion
  actionError.value = ''
  pendingDeleteTeam.value = null
  pendingDeleteApp.value = null
  selectedKey.value = null
  newTeamName.value = ''
  newAppName.value = ''
  partialApplication.value = null
  if (!loadedTeams) await loadTeams()
  if (version !== contextVersion) return
  if (isOverview.value) loadOverviewStats()
  else if (currentTeam.value && applicationsTeamId !== currentTeam.value.id) {
    applications.value = []
    await loadApplications(currentTeam.value.id)
  }
  await nextTick()
  nameInput.value?.focus()
}
watch(() => route.fullPath, loadContext, { immediate: true })

// Keep the one-time secret visible until the user closes its dialog.
onBeforeRouteLeave(() => !newKey.value && !busy.value && !keyBusy.value)
onBeforeRouteUpdate(() => !newKey.value && !busy.value && !keyBusy.value)

async function createTeam() {
  if (busy.value || !isAdmin.value || !newTeamName.value.trim()) return
  busy.value = true
  actionError.value = ''
  try {
    const result = await api.createTeam({ name: newTeamName.value.trim() })
    teams.value.push(result)
    busy.value = false
    await router.push(teamPath(result.id))
  } catch (e) {
    actionError.value = e.message
  } finally {
    busy.value = false
  }
}

async function createApplication() {
  if (busy.value || !isAdmin.value || !newAppName.value.trim()) return
  busy.value = true
  actionError.value = ''
  try {
    if (!partialApplication.value) {
      const result = await api.createApplication({
        team_id: currentTeam.value.id,
        name: newAppName.value.trim()
      })
      partialApplication.value = result
      applications.value.push(result)
    }
    const app = partialApplication.value
    const result = await api.createAPIKey(app.id, { name: 'Default' })
    appKeyCounts.value[app.id] = 1
    busy.value = false
    await router.push(applicationPath(app.id))
    newKey.value = result.key
  } catch (e) {
    actionError.value = partialApplication.value
      ? `Application created, but key creation failed: ${e.message}`
      : e.message
  } finally {
    busy.value = false
  }
}

async function deleteTeam(team) {
  if (busy.value || !isAdmin.value) return
  busy.value = true
  actionError.value = ''
  try {
    await api.deleteTeam(team.id)
    teams.value = teams.value.filter((t) => t.id !== team.id)
    pendingDeleteTeam.value = null
  } catch (e) {
    actionError.value = e.message
  } finally {
    busy.value = false
  }
}

async function deleteApplication(app) {
  if (busy.value || !isAdmin.value) return
  busy.value = true
  actionError.value = ''
  try {
    await api.deleteApplication(app.id)
    applications.value = applications.value.filter((a) => a.id !== app.id)
    pendingDeleteApp.value = null
  } catch (e) {
    actionError.value = e.message
  } finally {
    busy.value = false
  }
}

async function refreshKeyCount() {
  const app = currentApplication.value
  if (!app) return
  const counts = await countKeys([app])
  appKeyCounts.value[app.id] = counts[app.id]
}
</script>
