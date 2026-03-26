import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createWebHashHistory } from 'vue-router'
import { ref } from 'vue'

// NOTE: vi.mock factories are hoisted to the top of the file by Vitest.
// External variables (declared with let/const outside the factory) are NOT
// available when the factory runs. Use vi.fn() inside the factory directly,
// then use vi.mocked() per-test to configure behavior.

// Mock useSession so NavBar renders with a logged-in admin user by default
vi.mock('@/composables/useSession.js', () => ({
  useSession: () => ({
    isAuthenticated: ref(true),
    currentUser: ref({ id: 'u1', email: 'admin@example.com', name: 'Admin', is_admin: true }),
    loading: { value: false },
    fetchCurrentUser: vi.fn().mockResolvedValue(true),
    clearSession: vi.fn(),
  }),
}))

// Mock api client — spend() is the key method under test for badge behavior
vi.mock('@/api/client.js', () => ({
  api: {
    logout: vi.fn().mockResolvedValue(null),
    spend: vi.fn().mockResolvedValue({ rows: [], alerts: [], from: '', to: '' }),
  },
}))

import NavBar from '@/components/NavBar.vue'
import { api } from '@/api/client.js'

function makeRouter() {
  return createRouter({
    history: createWebHashHistory(),
    routes: [
      { path: '/', name: 'dashboard', component: { template: '<div />' } },
      { path: '/cost', name: 'cost', component: { template: '<div />' } },
      { path: '/somewhere', name: 'somewhere', component: { template: '<div />' } },
      { path: '/login', name: 'login', component: { template: '<div />' }, meta: { requiresAuth: false } },
    ],
  })
}

// Tests NavBar's Cost link badge behavior.
// There is no standalone NavBadge component — this tests the badge as part of NavBar.
describe('NavBar Cost badge', () => {
  beforeEach(() => {
    vi.mocked(api.spend).mockReset()
  })

  it('renders numeric badge when alertCount > 0', async () => {
    vi.mocked(api.spend).mockResolvedValueOnce({ rows: [], alerts: [{ key_id: 1 }], from: '', to: '' })
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(NavBar, { global: { plugins: [router] } })
    await flushPromises()
    const badge = wrapper.find('.bg-red-500.rounded-full')
    expect(badge.exists()).toBe(true)
    expect(badge.text()).toBe('1')
  })

  it('hides badge when alertCount is 0', async () => {
    vi.mocked(api.spend).mockResolvedValueOnce({ rows: [], alerts: [], from: '', to: '' })
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(NavBar, { global: { plugins: [router] } })
    await flushPromises()
    const badge = wrapper.find('.bg-red-500.rounded-full')
    expect(badge.exists()).toBe(false)
  })

  it('shows 9+ when alertCount >= 10', async () => {
    const manyAlerts = Array.from({ length: 10 }, (_, i) => ({ key_id: i + 1 }))
    vi.mocked(api.spend).mockResolvedValueOnce({ rows: [], alerts: manyAlerts, from: '', to: '' })
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(NavBar, { global: { plugins: [router] } })
    await flushPromises()
    const badge = wrapper.find('.bg-red-500.rounded-full')
    expect(badge.exists()).toBe(true)
    expect(badge.text()).toBe('9+')
  })

  it('refreshes alert count on route navigation', async () => {
    // First load: 0 alerts (badge hidden on mount)
    vi.mocked(api.spend).mockResolvedValueOnce({ rows: [], alerts: [], from: '', to: '' })
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(NavBar, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.find('.bg-red-500.rounded-full').exists()).toBe(false)

    // Navigation triggers re-fetch via afterEach: now 1 alert
    vi.mocked(api.spend).mockResolvedValueOnce({ rows: [], alerts: [{ key_id: 1 }], from: '', to: '' })
    await router.push('/somewhere')
    await flushPromises()
    expect(wrapper.find('.bg-red-500.rounded-full').exists()).toBe(true)
  })
})
