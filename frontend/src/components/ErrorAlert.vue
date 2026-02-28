<template>
  <div class="rounded-md bg-red-50 border border-red-200 p-4">
    <div class="flex items-start gap-3">
      <svg class="w-5 h-5 text-red-500 mt-0.5 shrink-0" fill="currentColor" viewBox="0 0 20 20">
        <path fill-rule="evenodd"
          d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z"
          clip-rule="evenodd" />
      </svg>
      <div>
        <h3 class="text-sm font-medium text-red-800">{{ title }}</h3>
        <p v-if="message" class="mt-1 text-sm text-red-700">{{ message }}</p>
      </div>
    </div>
  </div>
</template>

<script setup>
defineProps({
  title: {
    type: String,
    default: 'An error occurred',
  },
  message: {
    type: String,
    default: '',
  },
})
</script>
