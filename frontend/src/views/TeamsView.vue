<template>
  <div class="p-6">
    <h1 class="text-2xl font-bold text-gray-900 mb-6">Teams</h1>

    <div class="flex gap-6">
      <!-- Left panel: team list -->
      <div class="w-64 flex-shrink-0">
        <!-- Create team form -->
        <form @submit.prevent="handleCreateTeam" class="mb-4 flex gap-2">
          <input
            v-model="newTeamName"
            type="text"
            placeholder="Team name"
            class="flex-1 border border-gray-300 rounded-md px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
          />
          <button
            type="submit"
            class="px-3 py-2 bg-indigo-600 text-white text-sm font-medium rounded-md hover:bg-indigo-700 transition-colors"
          >
            Add
          </button>
        </form>

        <div v-if="loadingTeams" class="text-gray-500 text-sm">Loading...</div>
        <div v-else-if="teamsError" class="text-red-600 text-sm">{{ teamsError }}</div>
        <ul v-else class="space-y-1">
          <li
            v-for="team in teams"
            :key="team.id"
            :data-testid="`team-item-${team.id}`"
            class="flex items-center justify-between px-3 py-2 rounded-md cursor-pointer text-sm"
            :class="selectedTeam?.id === team.id ? 'bg-indigo-50 text-indigo-700' : 'text-gray-700 hover:bg-gray-50'"
            @click="selectTeam(team)"
          >
            <span>{{ team.name }}</span>
            <div>
              <!-- Confirmation inline delete UI -->
              <template v-if="pendingDeleteTeamId === team.id">
                <span class="text-xs text-gray-500 mr-1">Delete?</span>
                <button
                  :data-testid="`confirm-delete-${team.id}`"
                  @click.stop="confirmDeleteTeam(team.id)"
                  class="text-xs text-red-600 hover:text-red-800 mr-1 font-medium"
                >Yes</button>
                <button
                  @click.stop="cancelDeleteTeam"
                  class="text-xs text-gray-500 hover:text-gray-700"
                >No</button>
              </template>
              <button
                v-else
                :data-testid="`delete-team-${team.id}`"
                @click.stop="startDeleteTeam(team.id)"
                class="text-xs text-red-500 hover:text-red-700 ml-2"
              >Delete</button>
            </div>
          </li>
        </ul>
      </div>

      <!-- Right panel: member detail -->
      <div v-if="selectedTeam" class="flex-1">
        <h2 class="text-lg font-semibold text-gray-900 mb-4">{{ selectedTeam.name }} — Members</h2>

        <div v-if="loadingMembers" class="text-gray-500 text-sm">Loading members...</div>
        <div v-else>
          <!-- Members table -->
          <table class="min-w-full divide-y divide-gray-200 mb-6">
            <thead class="bg-gray-50">
              <tr>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Name</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Email</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Role</th>
                <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Actions</th>
              </tr>
            </thead>
            <tbody class="bg-white divide-y divide-gray-200">
              <tr v-if="members.length === 0">
                <td colspan="4" class="px-4 py-8 text-center text-sm text-gray-400 italic">
                  No members yet — add a first member to this team below
                </td>
              </tr>
              <tr v-for="member in members" :key="member.user_id">
                <td class="px-4 py-3 text-sm font-medium text-gray-900">{{ member.user_name }}</td>
                <td class="px-4 py-3 text-sm text-gray-500">{{ member.user_email }}</td>
                <td class="px-4 py-3 text-sm">
                  <select
                    :value="member.role"
                    @change="handleRoleChange(member, $event.target.value)"
                    class="border border-gray-300 rounded px-2 py-1 text-sm"
                  >
                    <option value="admin">admin</option>
                    <option value="member">member</option>
                    <option value="viewer">viewer</option>
                  </select>
                </td>
                <td class="px-4 py-3 text-sm">
                  <template v-if="pendingRemoveMemberId === member.user_id">
                    <span class="text-xs text-gray-500 mr-1">Remove {{ member.user_name }}?</span>
                    <button
                      @click="confirmRemoveMember(member)"
                      class="text-xs text-red-600 hover:text-red-800 mr-1 font-medium"
                    >Yes</button>
                    <button
                      @click="pendingRemoveMemberId = null"
                      class="text-xs text-gray-500 hover:text-gray-700"
                    >No</button>
                  </template>
                  <button
                    v-else
                    @click="startRemoveMember(member.user_id)"
                    class="text-xs text-red-500 hover:text-red-700"
                  >Remove</button>
                </td>
              </tr>
            </tbody>
          </table>

          <!-- Inline error banner -->
          <div v-if="membersError" class="mb-4 px-3 py-2 bg-red-50 border border-red-200 rounded text-sm text-red-600 flex justify-between items-center">
            <span>{{ membersError }}</span>
            <button @click="membersError = null" class="ml-3 text-red-400 hover:text-red-600">✕</button>
          </div>

          <!-- Add member form -->
          <div class="border-t border-gray-200 pt-4">
            <h3 class="text-sm font-medium text-gray-700 mb-3">Add Member</h3>
            <form @submit.prevent="handleAddMember" class="flex items-end gap-3">
              <!-- User search combobox -->
              <div class="relative" ref="userSearchContainer">
                <label class="block text-xs text-gray-500 mb-1">User</label>
                <input
                  v-model="addMemberSearch"
                  type="text"
                  placeholder="Search by name or email"
                  autocomplete="off"
                  data-1p-ignore
                  data-lpignore="true"
                  class="w-64 border border-gray-300 rounded-md px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  @input="onUserSearchInput"
                  @focus="onUserSearchFocus"
                  @keydown.escape="closeUserDropdown"
                  @keydown.down.prevent="moveSelection(1)"
                  @keydown.up.prevent="moveSelection(-1)"
                  @keydown.enter.prevent="selectHighlighted"
                />
                <!-- Dropdown -->
                <ul
                  v-if="userDropdownOpen && filteredUsers.length > 0"
                  class="absolute z-10 mt-1 w-full bg-white border border-gray-200 rounded-md shadow-lg max-h-48 overflow-y-auto"
                >
                  <li
                    v-for="(user, idx) in filteredUsers"
                    :key="user.id"
                    class="px-3 py-2 text-sm cursor-pointer"
                    :class="idx === highlightedIndex ? 'bg-indigo-50 text-indigo-700' : 'text-gray-700 hover:bg-gray-50'"
                    @mousedown.prevent="selectUser(user)"
                    @mouseover="highlightedIndex = idx"
                  >
                    <span class="font-medium">{{ user.name }}</span>
                    <span class="text-gray-400 ml-1">{{ user.email }}</span>
                  </li>
                </ul>
                <!-- No results hint -->
                <p v-if="userDropdownOpen && addMemberSearch && filteredUsers.length === 0" class="absolute z-10 mt-1 w-full bg-white border border-gray-200 rounded-md shadow-sm px-3 py-2 text-xs text-gray-400">
                  No users found
                </p>
              </div>
              <div>
                <label class="block text-xs text-gray-500 mb-1">Role</label>
                <select
                  v-model="addMemberRole"
                  class="border border-gray-300 rounded-md px-3 py-2 text-sm"
                >
                  <option value="admin">admin</option>
                  <option value="member">member</option>
                  <option value="viewer">viewer</option>
                </select>
              </div>
              <button
                type="submit"
                :disabled="!addMemberUserId"
                class="px-4 py-2 bg-indigo-600 text-white text-sm font-medium rounded-md hover:bg-indigo-700 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
              >Add</button>
            </form>
          </div>
        </div>
      </div>

      <div v-else class="flex-1 flex items-center justify-center text-gray-400 text-sm">
        Select a team to view members
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { api } from '../api/client.js'

const teams = ref([])
const loadingTeams = ref(true)
const teamsError = ref(null)

const selectedTeam = ref(null)
const members = ref([])
const loadingMembers = ref(false)
const membersError = ref(null)

const newTeamName = ref('')
const pendingDeleteTeamId = ref(null)

// User search combobox state
const allUsers = ref([])
const addMemberSearch = ref('')
const addMemberUserId = ref('')
const addMemberRole = ref('member')
const userDropdownOpen = ref(false)
const highlightedIndex = ref(-1)
const userSearchContainer = ref(null)
const pendingRemoveMemberId = ref(null)

const filteredUsers = computed(() => {
  const q = addMemberSearch.value.toLowerCase().trim()
  if (!q) return allUsers.value
  return allUsers.value.filter(
    (u) => u.name.toLowerCase().includes(q) || u.email.toLowerCase().includes(q)
  )
})

function onUserSearchInput() {
  // Clear the selected ID whenever the user edits the search text
  addMemberUserId.value = ''
  userDropdownOpen.value = true
  highlightedIndex.value = -1
}

function onUserSearchFocus() {
  userDropdownOpen.value = true
  highlightedIndex.value = -1
}

function closeUserDropdown() {
  userDropdownOpen.value = false
  highlightedIndex.value = -1
}

function selectUser(user) {
  addMemberUserId.value = user.id
  addMemberSearch.value = `${user.name} (${user.email})`
  closeUserDropdown()
}

function moveSelection(delta) {
  if (!userDropdownOpen.value || filteredUsers.value.length === 0) return
  const max = filteredUsers.value.length - 1
  highlightedIndex.value = Math.min(max, Math.max(0, highlightedIndex.value + delta))
}

function selectHighlighted() {
  if (highlightedIndex.value >= 0 && filteredUsers.value[highlightedIndex.value]) {
    selectUser(filteredUsers.value[highlightedIndex.value])
  }
}

function handleClickOutside(e) {
  if (userSearchContainer.value && !userSearchContainer.value.contains(e.target)) {
    closeUserDropdown()
  }
}

async function loadTeams() {
  loadingTeams.value = true
  teamsError.value = null
  try {
    teams.value = await api.teams()
  } catch (e) {
    teamsError.value = e.message
  } finally {
    loadingTeams.value = false
  }
}

async function loadMembers(teamId) {
  loadingMembers.value = true
  membersError.value = null
  try {
    members.value = await api.teamMembers(teamId) ?? []
  } catch (e) {
    membersError.value = e.message
  } finally {
    loadingMembers.value = false
  }
}

async function selectTeam(team) {
  selectedTeam.value = team
  pendingDeleteTeamId.value = null
  pendingRemoveMemberId.value = null
  await loadMembers(team.id)
}

async function handleCreateTeam() {
  if (!newTeamName.value.trim()) return
  try {
    await api.createTeam({ name: newTeamName.value.trim() })
    newTeamName.value = ''
    await loadTeams()
  } catch (e) {
    teamsError.value = e.message
  }
}

function startDeleteTeam(teamId) {
  pendingDeleteTeamId.value = teamId
}

function cancelDeleteTeam() {
  pendingDeleteTeamId.value = null
}

async function confirmDeleteTeam(teamId) {
  try {
    await api.deleteTeam(teamId)
    pendingDeleteTeamId.value = null
    if (selectedTeam.value?.id === teamId) {
      selectedTeam.value = null
      members.value = []
    }
    await loadTeams()
  } catch (e) {
    teamsError.value = e.message
  }
}

async function handleAddMember() {
  if (!addMemberUserId.value || !selectedTeam.value) return
  membersError.value = null
  try {
    await api.addTeamMember(selectedTeam.value.id, {
      user_id: addMemberUserId.value,
      role: addMemberRole.value,
    })
    addMemberUserId.value = ''
    addMemberSearch.value = ''
    addMemberRole.value = 'member'
    await loadMembers(selectedTeam.value.id)
  } catch (e) {
    membersError.value = e.message
  }
}

async function handleRoleChange(member, newRole) {
  try {
    await api.updateTeamMemberRole(selectedTeam.value.id, member.user_id, { role: newRole })
    await loadMembers(selectedTeam.value.id)
  } catch (e) {
    membersError.value = e.message
  }
}

function startRemoveMember(userId) {
  pendingRemoveMemberId.value = userId
}

async function confirmRemoveMember(member) {
  try {
    await api.removeTeamMember(selectedTeam.value.id, member.user_id)
    pendingRemoveMemberId.value = null
    await loadMembers(selectedTeam.value.id)
  } catch (e) {
    membersError.value = e.message
  }
}

onMounted(async () => {
  document.addEventListener('click', handleClickOutside)
  await Promise.all([loadTeams(), api.users().then((u) => { allUsers.value = u || [] })])
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
})
</script>
