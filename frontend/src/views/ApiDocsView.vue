<template>
  <div ref="container" class="redoc-container"></div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount } from 'vue'

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
    script.src = 'https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js'
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
        colors: { primary: { main: '#4F46E5' } },
        typography: { fontSize: '14px', fontFamily: 'ui-sans-serif, system-ui, sans-serif' },
      },
      hideDownloadButton: false,
      expandResponses: '200',
    },
    container.value,
  )
}

onBeforeUnmount(() => {
  if (container.value) {
    container.value.innerHTML = ''
  }
})
</script>

<style scoped>
.redoc-container {
  min-height: calc(100vh - 4rem); /* subtract navbar height */
}
</style>
