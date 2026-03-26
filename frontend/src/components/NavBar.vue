<template>
  <nav v-if="route.name !== 'login'" class="bg-white border-b border-gray-200">
    <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
      <div class="flex h-16 items-center justify-between">
        <!-- Logo / title -->
        <div class="flex items-center gap-3">
          <div class="w-8 h-8 bg-indigo-600 rounded-md flex items-center justify-center">
            <svg class="w-5 h-5 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                d="M13 10V3L4 14h7v7l9-11h-7z" />
            </svg>
          </div>
          <span class="text-lg font-semibold text-gray-900">LLM Proxy</span>
        </div>

        <!-- Nav links (only when authenticated) -->
        <div v-if="isAuthenticated" class="flex items-center gap-1">
          <router-link
            v-for="link in links"
            :key="link.to"
            :to="link.to"
            class="px-3 py-2 rounded-md text-sm font-medium transition-colors"
            :class="$route.path === link.to
              ? 'bg-indigo-50 text-indigo-700'
              : 'text-gray-600 hover:text-gray-900 hover:bg-gray-50'"
          >
            {{ link.label }}
          </router-link>
          <!-- Admin-only links -->
          <template v-if="currentUser?.is_admin">
            <router-link
              v-for="link in adminLinks"
              :key="link.to"
              :to="link.to"
              class="px-3 py-2 rounded-md text-sm font-medium transition-colors"
              :class="$route.path === link.to
                ? 'bg-indigo-50 text-indigo-700'
                : 'text-gray-600 hover:text-gray-900 hover:bg-gray-50'"
            >
              {{ link.label }}
            </router-link>
          </template>
        </div>

        <!-- User info + logout (only when authenticated) -->
        <div v-if="isAuthenticated" class="flex items-center gap-3">
          <span class="text-sm text-gray-600">{{ currentUser?.email }}</span>
          <button
            @click="handleLogout"
            class="text-sm font-medium text-gray-600 hover:text-gray-900 px-3 py-1.5 rounded-md border border-gray-200 hover:bg-gray-50 transition-colors"
          >
            Logout
          </button>
        </div>
      </div>
    </div>
  </nav>
</template>

<script setup>
import { useRoute, useRouter } from 'vue-router'
import { useSession } from '../composables/useSession.js'
import { api } from '../api/client.js'

const route = useRoute()
const router = useRouter()
const { isAuthenticated, currentUser, clearSession } = useSession()

const links = [
  { to: '/', label: 'Dashboard' },
  { to: '/models', label: 'Models' },
  { to: '/logs', label: 'Logs' },
  { to: '/config', label: 'Config' },
  { to: '/api-docs', label: 'API Docs' },
  { to: '/settings', label: 'Settings' },
]

const adminLinks = [
  { to: '/users', label: 'Users' },
  { to: '/teams', label: 'Teams' },
  { to: '/applications', label: 'Applications' },
  { to: '/keys', label: 'Keys' },
]

async function handleLogout() {
  try {
    await api.logout()
  } catch {
    // Even if the server request fails, clear local session state
  }
  clearSession()
  router.push('/login')
}
</script>
