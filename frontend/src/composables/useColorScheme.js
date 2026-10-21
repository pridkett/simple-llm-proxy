import { ref, onMounted, onBeforeUnmount, readonly } from 'vue'

// Most of the UI follows CSS media queries. Renderers with their own themes
// (charts and API documentation) need the same preference as reactive state.
export function useColorScheme() {
  const preference = window.matchMedia?.('(prefers-color-scheme: dark)')
  const isDark = ref(preference?.matches ?? false)
  const update = () => { isDark.value = preference.matches }

  onMounted(() => {
    if (!preference) return
    update()
    preference.addEventListener('change', update)
  })
  onBeforeUnmount(() => preference?.removeEventListener('change', update))

  return { isDark: readonly(isDark) }
}
