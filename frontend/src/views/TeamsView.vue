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
        <div v-else-if="membersError" class="text-red-600 text-sm">{{ membersError }}</div>
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

          <!-- Add member form -->
          <div class="border-t border-gray-200 pt-4">
            <h3 class="text-sm font-medium text-gray-700 mb-3">Add Member</h3>
            <form @submit.prevent="handleAddMember" class="flex items-end gap-3">
              <div>
                <label class="block text-xs text-gray-500 mb-1">User ID</label>
                <input
                  v-model="addMemberUserId"
                  type="text"
                  placeholder="user-id"
                  class="border border-gray-300 rounded-md px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
                />
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
                class="px-4 py-2 bg-indigo-600 text-white text-sm font-medium rounded-md hover:bg-indigo-700 transition-colors"
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
import { ref, onMounted } from 'vue'
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

const addMemberUserId = ref('')
const addMemberRole = ref('member')
const pendingRemoveMemberId = ref(null)

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
    members.value = await api.teamMembers(teamId)
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
  if (!addMemberUserId.value.trim() || !selectedTeam.value) return
  try {
    await api.addTeamMember(selectedTeam.value.id, {
      user_id: addMemberUserId.value.trim(),
      role: addMemberRole.value,
    })
    addMemberUserId.value = ''
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

onMounted(loadTeams)
</script>
