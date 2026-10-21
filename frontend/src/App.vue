<template>
  <div class="min-h-screen bg-gray-50 dark:bg-gray-900 flex flex-col">
    <NavBar />
    <main class="flex-1">
      <router-view />
    </main>
  </div>
</template>

<script setup>
import { onMounted } from 'vue'
import NavBar from './components/NavBar.vue'
import { useSession } from './composables/useSession.js'

const { fetchCurrentUser } = useSession()

onMounted(async () => {
  await fetchCurrentUser()
  // Returns false (not throws) on 401 — unauthenticated state is handled by router guard
})
</script>
