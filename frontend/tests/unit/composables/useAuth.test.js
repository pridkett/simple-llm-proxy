import { describe, it, expect, beforeEach } from 'vitest'

// We need to re-import the composable after clearing localStorage because
// the module-level ref is initialised from localStorage at import time.
// We use vi.resetModules() to get a fresh module per test.
import { vi } from 'vitest'

describe('useAuth', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.resetModules()
  })

  it('returns empty apiKey when localStorage is empty', async () => {
    const { useAuth } = await import('@/composables/useAuth.js')
    const { apiKey } = useAuth()
    expect(apiKey.value).toBe('')
  })

  it('reads existing key from localStorage on first load', async () => {
    localStorage.setItem('proxy_api_key', 'existing-key')
    const { useAuth } = await import('@/composables/useAuth.js')
    const { apiKey } = useAuth()
    expect(apiKey.value).toBe('existing-key')
  })

  it('setApiKey persists the key to localStorage', async () => {
    const { useAuth } = await import('@/composables/useAuth.js')
    const { apiKey, setApiKey } = useAuth()
    setApiKey('my-secret-key')
    expect(apiKey.value).toBe('my-secret-key')
    expect(localStorage.getItem('proxy_api_key')).toBe('my-secret-key')
  })

  it('setApiKey trims whitespace', async () => {
    const { useAuth } = await import('@/composables/useAuth.js')
    const { setApiKey, apiKey } = useAuth()
    setApiKey('  trimmed  ')
    expect(apiKey.value).toBe('trimmed')
  })

  it('clearApiKey removes the key from localStorage', async () => {
    localStorage.setItem('proxy_api_key', 'some-key')
    const { useAuth } = await import('@/composables/useAuth.js')
    const { clearApiKey, apiKey } = useAuth()
    clearApiKey()
    expect(apiKey.value).toBe('')
    expect(localStorage.getItem('proxy_api_key')).toBeNull()
  })

  it('setApiKey with empty string removes the key', async () => {
    const { useAuth } = await import('@/composables/useAuth.js')
    const { setApiKey } = useAuth()
    setApiKey('')
    expect(localStorage.getItem('proxy_api_key')).toBeNull()
  })

  it('hasApiKey returns false when no key', async () => {
    const { useAuth } = await import('@/composables/useAuth.js')
    const { hasApiKey } = useAuth()
    expect(hasApiKey()).toBe(false)
  })

  it('hasApiKey returns true after setting key', async () => {
    const { useAuth } = await import('@/composables/useAuth.js')
    const { setApiKey, hasApiKey } = useAuth()
    setApiKey('key')
    expect(hasApiKey()).toBe(true)
  })
})
