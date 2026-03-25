<template>
  <div class="min-h-screen bg-gray-50 flex flex-col">
    <NavBar />
    <main class="flex-1">
      <router-view />
    </main>
    <footer class="py-6 text-center text-xs text-gray-400 border-t border-gray-200">
      simple-llm-proxy
    </footer>
  </div>
</template>

<script setup>
import NavBar from './components/NavBar.vue'
</script>
