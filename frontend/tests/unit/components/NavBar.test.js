import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createRouter, createWebHashHistory } from 'vue-router'
import { ref } from 'vue'

// We mock useAuth so we can control the apiKey ref per test
const mockApiKey = ref('')
vi.mock('@/composables/useAuth.js', () => ({
  useAuth: () => ({
    apiKey: mockApiKey,
    setApiKey: (k) => { mockApiKey.value = k },
    clearApiKey: () => { mockApiKey.value = '' },
    hasApiKey: () => !!mockApiKey.value,
  }),
}))

import NavBar from '@/components/NavBar.vue'

function makeRouter() {
  return createRouter({
    history: createWebHashHistory(),
    routes: [
      { path: '/', component: { template: '<div/>' } },
      { path: '/models', component: { template: '<div/>' } },
      { path: '/logs', component: { template: '<div/>' } },
      { path: '/config', component: { template: '<div/>' } },
      { path: '/settings', component: { template: '<div/>' } },
    ],
  })
}

describe('NavBar', () => {
  beforeEach(() => {
    mockApiKey.value = ''
  })

  it('renders all navigation links', async () => {
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(NavBar, { global: { plugins: [router] } })
    const text = wrapper.text()
    expect(text).toContain('Dashboard')
    expect(text).toContain('Models')
    expect(text).toContain('Logs')
    expect(text).toContain('Config')
    expect(text).toContain('Settings')
  })

  it('shows "No key" when no API key is set', async () => {
    mockApiKey.value = ''
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(NavBar, { global: { plugins: [router] } })
    expect(wrapper.text()).toContain('No key')
  })

  it('shows "Key set" when API key is present', async () => {
    mockApiKey.value = 'test-key-123'
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(NavBar, { global: { plugins: [router] } })
    expect(wrapper.text()).toContain('Key set')
  })
})
