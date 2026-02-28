<template>
  <nav class="bg-white border-b border-gray-200">
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

        <!-- Nav links -->
        <div class="flex items-center gap-1">
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
        </div>

        <!-- API key indicator -->
        <div class="flex items-center gap-2">
          <span
            class="inline-flex items-center gap-1.5 text-xs font-medium px-2.5 py-1 rounded-full"
            :class="hasKey ? 'bg-green-50 text-green-700' : 'bg-yellow-50 text-yellow-700'"
          >
            <span
              class="w-1.5 h-1.5 rounded-full"
              :class="hasKey ? 'bg-green-500' : 'bg-yellow-500'"
            />
            {{ hasKey ? 'Key set' : 'No key' }}
          </span>
        </div>
      </div>
    </div>
  </nav>
</template>

<script setup>
import { computed } from 'vue'
import { useAuth } from '../composables/useAuth.js'

const { apiKey } = useAuth()
const hasKey = computed(() => !!apiKey.value)

const links = [
  { to: '/', label: 'Dashboard' },
  { to: '/models', label: 'Models' },
  { to: '/logs', label: 'Logs' },
  { to: '/config', label: 'Config' },
  { to: '/settings', label: 'Settings' },
]
</script>
