<template>
  <div ref="container" class="redoc-container"></div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount, watch } from 'vue'
import { useColorScheme } from '../composables/useColorScheme.js'

const { isDark } = useColorScheme()

const container = ref(null)
const SCRIPT_ID = 'redoc-standalone-script'

onMounted(() => {
  if (window.Redoc) {
    initRedoc()
    return
  }

  if (!document.getElementById(SCRIPT_ID)) {
    const script = document.createElement('script')
    script.id = SCRIPT_ID
    // Pin the major version matching Redoc.init and the theme API used here.
    script.src = 'https://cdn.redoc.ly/redoc/v2.5.4/bundles/redoc.standalone.js'
    script.onload = initRedoc
    document.head.appendChild(script)
  } else {
    // Script tag exists but Redoc not ready yet — wait for load
    document.getElementById(SCRIPT_ID).addEventListener('load', initRedoc)
  }
})

function initRedoc() {
  if (!container.value || !window.Redoc) return
  window.Redoc.init(
    '/openapi.json',
    {
      theme: {
        colors: {
          primary: { main: isDark.value ? '#A5B4FC' : '#4F46E5' },
          ...(isDark.value ? {
            text: { primary: '#F3F4F6', secondary: '#D1D5DB' },
            gray: { 50: '#111827', 100: '#1F2937' },
            border: { dark: '#4B5563', light: '#374151' },
            responses: {
              success: { color: '#86EFAC', backgroundColor: '#052E16', tabTextColor: '#86EFAC' },
              error: { color: '#FCA5A5', backgroundColor: '#450A0A', tabTextColor: '#FCA5A5' },
              redirect: { color: '#93C5FD', backgroundColor: '#172554', tabTextColor: '#93C5FD' },
              info: { color: '#C4B5FD', backgroundColor: '#2E1065', tabTextColor: '#C4B5FD' },
            },
          } : {}),
        },
        typography: {
          fontSize: '14px', fontFamily: 'ui-sans-serif, system-ui, sans-serif',
          ...(isDark.value ? { code: { color: '#FCA5A5', backgroundColor: '#374151' } } : {}),
        },
        ...(isDark.value ? {
          sidebar: { backgroundColor: '#1F2937', textColor: '#D1D5DB', activeTextColor: '#A5B4FC' },
          rightPanel: { backgroundColor: '#111827', textColor: '#F3F4F6' },
          codeBlock: { backgroundColor: '#111827' },
          schema: { nestedBackground: '#1F2937', linesColor: '#374151', typeNameColor: '#A5B4FC', typeTitleColor: '#D1D5DB' },
        } : {}),
      },
      hideDownloadButton: false,
      expandResponses: '200',
    },
    container.value,
  )
}

watch(isDark, initRedoc)

onBeforeUnmount(() => {
  document.getElementById(SCRIPT_ID)?.removeEventListener('load', initRedoc)
  if (container.value) {
    container.value.innerHTML = ''
  }
})
</script>

<style scoped>
.redoc-container {
  min-height: calc(100vh - 4rem); /* subtract navbar height */
}

/* Redoc 2 hard-codes light colors for schema headings and response tabs. */
@media (prefers-color-scheme: dark) {
  .redoc-container :deep(h5) {
    color: #d1d5db;
    border-color: #374151;
  }
  .redoc-container :deep(h5 span) {
    color: inherit;
  }
  .redoc-container :deep([role="tab"]) {
    background-color: #1f2937;
  }
  .redoc-container :deep([role="tab"][aria-selected="true"]) {
    background-color: #374151;
    color: #f3f4f6;
  }
}
</style>
