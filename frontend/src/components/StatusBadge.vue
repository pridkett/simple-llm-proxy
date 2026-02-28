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
    // 'healthy' | 'cooldown' | 'unknown'
  },
})

const label = computed(() => {
  switch (props.status) {
    case 'healthy': return 'Healthy'
    case 'cooldown': return 'Cooldown'
    default: return 'Unknown'
  }
})

const classes = computed(() => {
  switch (props.status) {
    case 'healthy': return 'bg-green-50 text-green-700'
    case 'cooldown': return 'bg-red-50 text-red-700'
    default: return 'bg-gray-100 text-gray-600'
  }
})

const dotClass = computed(() => {
  switch (props.status) {
    case 'healthy': return 'bg-green-500'
    case 'cooldown': return 'bg-red-500'
    default: return 'bg-gray-400'
  }
})
</script>
