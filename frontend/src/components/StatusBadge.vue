<template>
  <span
    class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium"
    :class="classes"
  >
    <span class="w-1.5 h-1.5 rounded-full" :class="dotClass" />
    {{ label }}
  </span>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  status: {
    type: String,
    required: true,
    // 'healthy' | 'cooldown' | 'backoff' | 'unknown' | 'ok' | 'warning' | 'over'
  },
})

const label = computed(() => {
  switch (props.status) {
    case 'healthy': return 'Healthy'
    case 'cooldown': return 'Cooldown'
    case 'backoff': return 'Backoff'
    case 'ok': return 'OK'
    case 'warning': return 'Warning'
    case 'over': return 'Over Budget'
    default: return 'Unknown'
  }
})

const classes = computed(() => {
  switch (props.status) {
    case 'healthy': return 'bg-green-50 dark:bg-green-950 text-green-700 dark:text-green-300'
    case 'cooldown': return 'bg-red-50 dark:bg-red-950 text-red-700 dark:text-red-300'
    case 'backoff': return 'bg-amber-50 dark:bg-amber-950 text-amber-700 dark:text-amber-300'
    case 'ok': return 'bg-green-50 dark:bg-green-950 text-green-700 dark:text-green-300'
    case 'warning': return 'bg-amber-50 dark:bg-amber-950 text-amber-700 dark:text-amber-300'
    case 'over': return 'bg-red-50 dark:bg-red-950 text-red-700 dark:text-red-300'
    default: return 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300'
  }
})

const dotClass = computed(() => {
  switch (props.status) {
    case 'healthy': return 'bg-green-500'
    case 'cooldown': return 'bg-red-500'
    case 'backoff': return 'bg-amber-500'
    case 'ok': return 'bg-green-500'
    case 'warning': return 'bg-amber-500'
    case 'over': return 'bg-red-500'
    default: return 'bg-gray-400 dark:bg-gray-500'
  }
})
</script>
