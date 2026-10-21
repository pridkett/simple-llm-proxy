<template>
  <section>
    <div v-if="loadingMembers" class="text-gray-500 dark:text-gray-400 text-sm">
      Loading members...
    </div>
    <div v-else>
      <!-- Members table -->
      <div class="overflow-x-auto">
        <table class="min-w-full divide-y divide-gray-200 dark:divide-gray-700 mb-6">
          <thead class="bg-gray-50 dark:bg-gray-900">
            <tr>
              <th
                class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase"
              >
                Name
              </th>
              <th
                class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase"
              >
                Email
              </th>
              <th
                class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase"
              >
                Role
              </th>
              <th
                v-if="isAdmin"
                class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase"
              >
                Actions
              </th>
            </tr>
          </thead>
          <tbody class="bg-white dark:bg-gray-800 divide-y divide-gray-200 dark:divide-gray-700">
            <tr v-if="members.length === 0">
              <td
                :colspan="isAdmin ? 4 : 3"
                class="px-4 py-8 text-center text-sm text-gray-400 italic"
              >
                No members yet<template v-if="isAdmin">
                  — add a first member to this team below</template
                >
              </td>
            </tr>
            <tr v-for="member in members" :key="member.user_id">
              <td class="px-4 py-3 text-sm font-medium text-gray-900 dark:text-gray-100">
                {{ member.user_name }}
              </td>
              <td class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">
                {{ member.user_email }}
              </td>
              <td class="px-4 py-3 text-sm">
                <select
                  v-if="isAdmin"
                  :value="member.role"
                  @change="handleRoleChange(member, $event.target.value)"
                  class="border border-gray-300 dark:border-gray-600 rounded px-2 py-1 text-sm"
                >
                  <option value="admin">admin</option>
                  <option value="member">member</option>
                  <option value="viewer">viewer</option>
                </select>
                <span v-else class="text-gray-600 dark:text-gray-300">{{ member.role }}</span>
              </td>
              <td v-if="isAdmin" class="px-4 py-3 text-sm">
                <template v-if="pendingRemoveMemberId === member.user_id">
                  <span class="text-xs text-gray-500 dark:text-gray-400 mr-1"
                    >Remove {{ member.user_name }}?</span
                  >
                  <button
                    @click="confirmRemoveMember(member)"
                    class="text-xs text-red-600 dark:text-red-400 hover:text-red-800 dark:hover:text-red-200 mr-1 font-medium"
                  >
                    Yes
                  </button>
                  <button
                    @click="pendingRemoveMemberId = null"
                    class="text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200"
                  >
                    No
                  </button>
                </template>
                <button
                  v-else
                  @click="startRemoveMember(member.user_id)"
                  class="text-xs text-red-500 dark:text-red-400 hover:text-red-700 dark:hover:text-red-300"
                >
                  Remove
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Inline error banner -->
      <div
        v-if="membersError"
        role="alert"
        class="mb-4 px-3 py-2 bg-red-50 dark:bg-red-950 border border-red-200 dark:border-red-800 rounded text-sm text-red-600 dark:text-red-400 flex justify-between items-center"
      >
        <span>{{ membersError }}</span>
        <button
          @click="membersError = null"
          class="ml-3 text-red-400 hover:text-red-600 dark:hover:text-red-400"
        >
          ✕
        </button>
      </div>

      <!-- Add member form (admin only) -->
      <div v-if="isAdmin" class="border-t border-gray-200 dark:border-gray-700 pt-4">
        <h3 class="text-sm font-medium text-gray-700 dark:text-gray-200 mb-3">Add Member</h3>
        <form
          @submit.prevent="handleAddMember"
          class="flex flex-wrap items-end gap-3"
        >
          <!-- User search combobox -->
          <div class="relative" ref="userSearchContainer">
            <label class="block text-xs text-gray-500 dark:text-gray-400 mb-1">User</label>
            <input
              v-model="addMemberSearch"
              type="text"
              placeholder="Search by name or email"
              autocomplete="off"
              data-1p-ignore
              data-lpignore="true"
              class="w-64 max-w-full border border-gray-300 dark:border-gray-600 rounded-md px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 dark:focus:ring-indigo-400"
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
              class="absolute z-10 mt-1 w-full bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md shadow-lg max-h-48 overflow-y-auto"
            >
              <li
                v-for="(user, idx) in filteredUsers"
                :key="user.id"
                class="px-3 py-2 text-sm cursor-pointer"
                :class="
                  idx === highlightedIndex
                    ? 'bg-indigo-50 dark:bg-indigo-950 text-indigo-700 dark:text-indigo-300'
                    : 'text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-900'
                "
                @mousedown.prevent="selectUser(user)"
                @mouseover="highlightedIndex = idx"
              >
                <span class="font-medium">{{ user.name }}</span>
                <span class="text-gray-400 ml-1">{{ user.email }}</span>
              </li>
            </ul>
            <!-- No results hint -->
            <p
              v-if="
                userDropdownOpen &&
                addMemberSearch &&
                filteredUsers.length === 0
              "
              class="absolute z-10 mt-1 w-full bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md shadow-sm px-3 py-2 text-xs text-gray-400"
            >
              No users found
            </p>
          </div>
          <div>
            <label class="block text-xs text-gray-500 dark:text-gray-400 mb-1">Role</label>
            <select
              v-model="addMemberRole"
              class="border border-gray-300 dark:border-gray-600 rounded-md px-3 py-2 text-sm"
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
          >
            Add
          </button>
        </form>
      </div>
    </div>
  </section>
</template>
<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { api } from '../api/client.js'
import { useSession } from '../composables/useSession.js'

const { currentUser } = useSession()
const isAdmin = computed(() => !!currentUser.value?.is_admin)

const props = defineProps({ team: { type: Object, required: true } })
const selectedTeam = computed(() => props.team)
const members = ref([])
const loadingMembers = ref(false)
const membersError = ref(null)

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
  highlightedIndex.value = Math.min(
    max,
    Math.max(0, highlightedIndex.value + delta)
  )
}

function selectHighlighted() {
  if (
    highlightedIndex.value >= 0 &&
    filteredUsers.value[highlightedIndex.value]
  ) {
    selectUser(filteredUsers.value[highlightedIndex.value])
  }
}

function handleClickOutside(e) {
  if (
    userSearchContainer.value &&
    !userSearchContainer.value.contains(e.target)
  ) {
    closeUserDropdown()
  }
}

async function loadMembers(teamId) {
  loadingMembers.value = true
  membersError.value = null
  try {
    members.value = (await api.teamMembers(teamId)) ?? []
  } catch (e) {
    membersError.value = e.message
  } finally {
    loadingMembers.value = false
  }
}

async function handleAddMember() {
  if (!addMemberUserId.value || !selectedTeam.value) return
  membersError.value = null
  try {
    await api.addTeamMember(selectedTeam.value.id, {
      user_id: addMemberUserId.value,
      role: addMemberRole.value
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
    await api.updateTeamMemberRole(selectedTeam.value.id, member.user_id, {
      role: newRole
    })
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
  const tasks = [loadMembers(props.team.id)]
  // Only admins need the full user list (for the add-member search combobox)
  if (isAdmin.value) {
    tasks.push(
      api
        .users()
        .then((u) => {
          allUsers.value = u || []
        })
        .catch((e) => {
          membersError.value = e.message
        })
    )
  }
  await Promise.all(tasks)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
})
</script>
