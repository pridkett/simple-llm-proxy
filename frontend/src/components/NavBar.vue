<template>
  <header v-if="route.name !== 'login'" class="bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700">
    <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
      <div class="flex h-16 items-center justify-between gap-4">
        <router-link
          to="/"
          class="flex shrink-0 items-center gap-3 rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:focus-visible:ring-indigo-400 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-gray-900"
          aria-label="LLM Proxy dashboard"
        >
          <div class="w-8 h-8 shrink-0 bg-indigo-600 rounded-md flex items-center justify-center">
            <svg aria-hidden="true" class="w-5 h-5 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                d="M13 10V3L4 14h7v7l9-11h-7z" />
            </svg>
          </div>
          <span class="whitespace-nowrap text-lg font-semibold text-gray-900 dark:text-gray-100">LLM Proxy</span>
        </router-link>

        <div v-if="isAuthenticated" class="flex shrink-0 items-center gap-3">
          <button
            ref="navigationTrigger"
            type="button"
            aria-controls="primary-navigation"
            :aria-expanded="navigationOpen"
            :aria-label="navigationOpen ? 'Close navigation' : 'Open navigation'"
            class="flex h-11 w-11 items-center justify-center rounded-md text-gray-600 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-900 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:focus-visible:ring-indigo-400 md:hidden"
            @click="toggleNavigation"
          >
            <svg aria-hidden="true" class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path v-if="navigationOpen" stroke-linecap="round" stroke-width="2" d="M6 6l12 12M6 18L18 6" />
              <path v-else stroke-linecap="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
            </svg>
          </button>

          <div ref="accountControl" class="relative" @focusout="handleAccountFocusOut">
            <button
              ref="accountTrigger"
              type="button"
              aria-label="Account options"
              aria-controls="account-options"
              :aria-expanded="accountOpen"
              class="flex h-11 w-11 items-center justify-center rounded-full bg-indigo-100 dark:bg-indigo-900 text-sm font-semibold text-indigo-700 dark:text-indigo-300 ring-1 ring-inset ring-indigo-200 dark:ring-indigo-800 hover:bg-indigo-200 dark:hover:bg-indigo-800 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:focus-visible:ring-indigo-400 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-gray-900"
              @click="toggleAccount"
            >
              <span aria-hidden="true">{{ initials }}</span>
            </button>

            <!-- Disclosure with ordinary links/buttons, so Tab follows native browser behavior. -->
            <div
              v-if="accountOpen"
              id="account-options"
              class="absolute right-0 z-50 mt-2 w-72 max-w-[calc(100vw-2rem)] rounded-lg border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 py-1 shadow-lg"
            >
              <div class="border-b border-gray-100 dark:border-gray-700 px-4 py-3 text-sm [overflow-wrap:anywhere]">
                <p v-if="currentUser?.name" class="font-medium text-gray-900 dark:text-gray-100">{{ currentUser.name }}</p>
                <p class="text-gray-500 dark:text-gray-400">{{ currentUser?.email }}</p>
              </div>
              <router-link
                ref="settingsLink"
                to="/settings"
                class="block px-4 py-3 text-sm text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-900 focus-visible:outline-none focus-visible:bg-indigo-50 dark:focus-visible:bg-indigo-950 focus-visible:text-indigo-700 dark:focus-visible:text-indigo-300"
                @click="closeAccount(true)"
              >
                Proxy settings
              </router-link>
              <button
                type="button"
                class="block w-full px-4 py-3 text-left text-sm text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-900 focus-visible:outline-none focus-visible:bg-indigo-50 dark:focus-visible:bg-indigo-950 focus-visible:text-indigo-700 dark:focus-visible:text-indigo-300"
                @click="handleLogout"
              >
                Logout
              </button>
            </div>
          </div>
        </div>
      </div>

      <nav
        v-if="isAuthenticated"
        id="primary-navigation"
        aria-label="Main navigation"
        class="flex-wrap gap-1 border-t border-gray-100 dark:border-gray-700 py-2 md:flex"
        :class="navigationOpen ? 'flex' : 'hidden'"
      >
        <router-link
          v-for="link in visibleLinks"
          :key="link.to"
          :to="link.to"
          class="relative whitespace-nowrap rounded-md px-3 py-2 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:focus-visible:ring-indigo-400"
          :class="(route.path === link.to || (link.to === '/teams' && route.path.startsWith('/teams/')))
            ? 'bg-indigo-50 dark:bg-indigo-950 text-indigo-700 dark:text-indigo-300'
            : 'text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-50 dark:hover:bg-gray-900'"
          @click="closeNavigation(true)"
          :aria-current="(route.path === link.to || (link.to === '/teams' && route.path.startsWith('/teams/'))) ? 'page' : undefined"
        >
          {{ link.label }}
          <span
            v-if="link.to === '/cost' && alertCount > 0"
            class="absolute -top-1 -right-1 flex h-4 w-4 items-center justify-center rounded-full bg-red-500 text-white text-[10px] font-semibold"
          >
            {{ alertCount > 9 ? '9+' : alertCount }}
          </span>
        </router-link>
      </nav>
    </div>
  </header>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useSession } from '../composables/useSession.js'
import { api } from '../api/client.js'

const route = useRoute()
const router = useRouter()
const { isAuthenticated, currentUser, clearSession } = useSession()

const accountControl = ref(null)
const accountTrigger = ref(null)
const settingsLink = ref(null)
const navigationTrigger = ref(null)
const accountOpen = ref(false)
const navigationOpen = ref(false)
const alertCount = ref(0)

const initials = computed(() => {
  const name = currentUser.value?.name?.trim()
  const emailName = currentUser.value?.email?.split('@')[0] ?? ''
  const parts = (name || emailName).split(/[\s._+\-]+/u).filter(Boolean)
  if (!parts.length) return '?'
  return (Array.from(parts[0])[0] + (parts.length > 1 ? Array.from(parts.at(-1))[0] : '')).toUpperCase()
})

const links = [
  { to: '/', label: 'Dashboard' },
  { to: '/models', label: 'Models' },
  { to: '/chat', label: 'Chat' },
  { to: '/logs', label: 'Logs' },
  { to: '/config', label: 'Config' },
  { to: '/api-docs', label: 'API Docs' },
]

const adminLinks = [
  { to: '/users', label: 'Users' },
  { to: '/teams', label: 'Teams' },
  { to: '/cost', label: 'Cost' },
  { to: '/pools', label: 'Pools' },
  { to: '/webhooks', label: 'Webhooks' },
  { to: '/events', label: 'Events' },
]

const visibleLinks = computed(() => currentUser.value?.is_admin ? [...links, ...adminLinks] : links)

function closeAccount(restoreFocus = false) {
  accountOpen.value = false
  if (restoreFocus) accountTrigger.value?.focus()
}

function closeNavigation(restoreFocus = false) {
  if (navigationOpen.value && restoreFocus) navigationTrigger.value?.focus()
  navigationOpen.value = false
}

async function toggleAccount() {
  if (accountOpen.value) {
    closeAccount()
    return
  }
  closeNavigation()
  accountOpen.value = true
  await nextTick()
  settingsLink.value?.$el.focus()
}

function toggleNavigation() {
  closeAccount()
  navigationOpen.value = !navigationOpen.value
}

function handleAccountFocusOut(event) {
  if (!accountControl.value?.contains(event.relatedTarget)) closeAccount()
}

function handlePointerDown(event) {
  if (!accountControl.value?.contains(event.target)) closeAccount()
}

function handleKeyDown(event) {
  if (event.key !== 'Escape') return
  if (accountOpen.value) {
    closeAccount(true)
  } else if (navigationOpen.value) {
    closeNavigation(true)
  }
}

async function fetchAlertCount() {
  if (!isAuthenticated.value || !currentUser.value?.is_admin) {
    alertCount.value = 0
    return
  }
  try {
    const data = await api.spend()
    if (data && data.alerts) {
      alertCount.value = data.alerts.length
    }
  } catch {
    // Badge silently stays at 0 on error — non-critical
  }
}

onMounted(() => {
  fetchAlertCount()
  document.addEventListener('pointerdown', handlePointerDown)
  document.addEventListener('keydown', handleKeyDown)
})

// Refresh the badge on navigation, and dismiss disclosures on history navigation too.
const removeAfterEach = router.afterEach(() => {
  closeAccount()
  closeNavigation()
  fetchAlertCount()
})

onBeforeUnmount(() => {
  removeAfterEach()
  document.removeEventListener('pointerdown', handlePointerDown)
  document.removeEventListener('keydown', handleKeyDown)
})

async function handleLogout() {
  closeAccount(true)
  try {
    await api.logout()
  } catch {
    // Even if the server request fails, clear local session state
  }
  clearSession()
  router.push('/login')
}
</script>
