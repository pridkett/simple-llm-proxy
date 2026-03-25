<template>
  <div class="p-6">
    <h1 class="text-2xl font-bold text-gray-900 mb-6">Users</h1>
    <div v-if="loading" class="text-gray-500">Loading...</div>
    <div v-else-if="error" class="text-red-600">{{ error }}</div>
    <table v-else class="min-w-full divide-y divide-gray-200">
      <thead class="bg-gray-50">
        <tr>
          <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Name</th>
          <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Email</th>
          <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Role</th>
          <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Last Seen</th>
        </tr>
      </thead>
      <tbody class="bg-white divide-y divide-gray-200">
        <tr v-for="user in users" :key="user.id">
          <td class="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-900">{{ user.name }}</td>
          <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{{ user.email }}</td>
          <td class="px-6 py-4 whitespace-nowrap">
            <span
              v-if="user.is_admin"
              class="px-2 inline-flex text-xs leading-5 font-semibold rounded-full bg-green-100 text-green-800"
            >Admin</span>
            <span
              v-else
              class="px-2 inline-flex text-xs leading-5 font-semibold rounded-full bg-gray-100 text-gray-800"
            >User</span>
          </td>
          <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
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
