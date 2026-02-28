import { ref, readonly } from 'vue'

const STORAGE_KEY = 'proxy_api_key'

const apiKey = ref(localStorage.getItem(STORAGE_KEY) || '')

export function useAuth() {
  function setApiKey(key) {
    apiKey.value = key.trim()
    if (apiKey.value) {
      localStorage.setItem(STORAGE_KEY, apiKey.value)
    } else {
      localStorage.removeItem(STORAGE_KEY)
    }
  }

  function clearApiKey() {
    apiKey.value = ''
    localStorage.removeItem(STORAGE_KEY)
  }

  return {
    apiKey: readonly(apiKey),
    setApiKey,
    clearApiKey,
    hasApiKey: () => !!apiKey.value,
  }
}
