<template>
  <div>
    <label :for="id" class="block text-xs text-gray-500 dark:text-gray-400 mb-1">
      {{ label }} (optional)
    </label>
    <input
      :id="id"
      v-model="value"
      type="datetime-local"
      step="1"
      :aria-describedby="`${id}-help`"
      class="input w-full"
    />
    <p :id="`${id}-help`" class="mt-1 text-xs text-gray-500 dark:text-gray-400">
      Uses your browser’s time zone. Leave blank for a key that never expires.
    </p>
  </div>
</template>

<script setup>
defineProps({
  id: { type: String, required: true },
  label: { type: String, default: 'Expires' }
})
const value = defineModel({ type: String, required: true })
</script>
