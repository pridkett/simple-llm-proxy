<template>
  <div class="p-6">
    <h1 class="text-2xl font-bold text-gray-900 dark:text-gray-100 mb-6">Users</h1>
    <div v-if="loading" class="text-gray-500 dark:text-gray-400">Loading...</div>
    <div v-else-if="error" class="text-red-600 dark:text-red-400">{{ error }}</div>
    <table v-else class="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
      <thead class="bg-gray-50 dark:bg-gray-900">
        <tr>
          <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Name</th>
          <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Email</th>
          <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Role</th>
          <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Last Seen</th>
        </tr>
      </thead>
      <tbody class="bg-white dark:bg-gray-800 divide-y divide-gray-200 dark:divide-gray-700">
        <tr v-for="user in users" :key="user.id">
          <td class="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-900 dark:text-gray-100">{{ user.name }}</td>
          <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500 dark:text-gray-400">{{ user.email }}</td>
          <td class="px-6 py-4 whitespace-nowrap">
            <span
              v-if="user.is_admin"
              class="px-2 inline-flex text-xs leading-5 font-semibold rounded-full bg-green-100 dark:bg-green-900 text-green-800 dark:text-green-200"
            >Admin</span>
            <span
              v-else
              class="px-2 inline-flex text-xs leading-5 font-semibold rounded-full bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200"
            >User</span>
          </td>
          <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500 dark:text-gray-400">
            {{ user.last_seen ? new Date(user.last_seen).toLocaleDateString() : '—' }}
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api/client.js'

const users = ref([])
const loading = ref(true)
const error = ref(null)

onMounted(async () => {
  try {
    users.value = await api.users()
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
})
</script>
