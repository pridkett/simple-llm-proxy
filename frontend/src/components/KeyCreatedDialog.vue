<template>
  <dialog
    ref="dialog"
    aria-labelledby="key-created-title"
    class="w-[calc(100%-2rem)] max-w-lg rounded-lg border-0 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 p-6 shadow-xl backdrop:bg-black/50"
    @cancel.prevent
  >
    <h2 id="key-created-title" class="text-lg font-semibold text-gray-900 dark:text-gray-100">
      API key created
    </h2>
    <p class="mt-2 text-sm text-gray-600 dark:text-gray-300">
      Copy this key now. It will not be shown again.
    </p>
    <code
      class="my-4 block break-all rounded bg-gray-100 dark:bg-gray-700 p-3 text-sm text-gray-900 dark:text-gray-100 select-all"
      >{{ value }}</code
    >
    <p v-if="copyError" role="alert" class="mb-3 text-sm text-red-600 dark:text-red-400">
      {{ copyError }}
    </p>
    <div class="flex gap-3">
      <button type="button" class="btn btn-secondary" autofocus @click="copy">
        {{ copied ? 'Copied' : 'Copy key' }}
      </button>
      <button type="button" class="btn btn-primary" @click="$emit('close')">
        Done
      </button>
    </div>
  </dialog>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount } from 'vue'

const props = defineProps({ value: { type: String, required: true } })
defineEmits(['close'])
const dialog = ref(null)
const copied = ref(false)
const copyError = ref('')

async function copy() {
  try {
    await navigator.clipboard.writeText(props.value)
    copied.value = true
    copyError.value = ''
  } catch {
    copyError.value = 'Select the key above and copy it manually.'
  }
}

onMounted(() => {
  if (dialog.value.showModal) dialog.value.showModal()
  else dialog.value.setAttribute('open', '')
})
onBeforeUnmount(() => dialog.value?.close?.())
</script>
