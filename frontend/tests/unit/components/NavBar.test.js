import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createRouter, createWebHashHistory } from 'vue-router'
import { ref } from 'vue'

// Mock useSession so we can control auth state per test
const mockIsAuthenticated = ref(false)
const mockCurrentUser = ref(null)
const mockClearSession = vi.fn()

vi.mock('@/composables/useSession.js', () => ({
  useSession: () => ({
    isAuthenticated: mockIsAuthenticated,
    currentUser: mockCurrentUser,
    loading: { value: false },
    fetchCurrentUser: vi.fn().mockResolvedValue(false),
    clearSession: mockClearSession,
  }),
}))

// Mock api client to avoid real fetch calls
vi.mock('@/api/client.js', () => ({
  api: {
    logout: vi.fn().mockResolvedValue(null),
    spend: vi.fn().mockResolvedValue({ rows: [], alerts: [], from: '', to: '' }),
  },
}))

import NavBar from '@/components/NavBar.vue'

function makeRouter(currentPath = '/') {
  const router = createRouter({
    history: createWebHashHistory(),
    routes: [
      { path: '/login', name: 'login', component: { template: '<div/>' }, meta: { requiresAuth: false } },
      { path: '/', name: 'dashboard', component: { template: '<div/>' } },
      { path: '/models', name: 'models', component: { template: '<div/>' } },
      { path: '/chat', name: 'chat', component: { template: '<div/>' } },
      { path: '/logs', name: 'logs', component: { template: '<div/>' } },
      { path: '/config', name: 'config', component: { template: '<div/>' } },
      { path: '/api-docs', name: 'api-docs', component: { template: '<div/>' } },
      { path: '/settings', name: 'settings', component: { template: '<div/>' } },
      { path: '/users', name: 'users', component: { template: '<div/>' } },
      { path: '/teams', name: 'teams', component: { template: '<div/>' } },
      { path: '/applications', name: 'applications', component: { template: '<div/>' } },
    ],
  })
  return router
}

describe('NavBar', () => {
  beforeEach(() => {
    mockIsAuthenticated.value = false
    mockCurrentUser.value = null
    mockClearSession.mockClear()
  })

  it('renders all navigation links when authenticated', async () => {
    mockIsAuthenticated.value = true
    mockCurrentUser.value = { id: 'u1', email: 'test@example.com', name: 'Test', is_admin: true }
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(NavBar, { global: { plugins: [router] } })
    const text = wrapper.text()
    expect(text).toContain('Dashboard')
    expect(text).toContain('Models')
    expect(text).toContain('Chat')
    expect(text).toContain('Logs')
    expect(text).toContain('Config')
    expect(text).toContain('API Docs')
    expect(text).toContain('Settings')
  })

  it('hides navigation links when not authenticated', async () => {
    mockIsAuthenticated.value = false
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(NavBar, { global: { plugins: [router] } })
    const text = wrapper.text()
    expect(text).not.toContain('Dashboard')
    expect(text).not.toContain('Logout')
  })

  it('shows user email when authenticated', async () => {
    mockIsAuthenticated.value = true
    mockCurrentUser.value = { id: 'u1', email: 'alice@example.com', name: 'Alice', is_admin: true }
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(NavBar, { global: { plugins: [router] } })
    expect(wrapper.text()).toContain('alice@example.com')
  })

  it('shows logout button when authenticated', async () => {
    mockIsAuthenticated.value = true
    mockCurrentUser.value = { id: 'u1', email: 'alice@example.com', name: 'Alice', is_admin: false }
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(NavBar, { global: { plugins: [router] } })
    expect(wrapper.text()).toContain('Logout')
  })

  it('hides nav entirely when on login route', async () => {
    mockIsAuthenticated.value = false
    const router = makeRouter()
    await router.push('/login')
    const wrapper = mount(NavBar, { global: { plugins: [router] } })
    // Nav element should not be visible on login route
    expect(wrapper.find('nav').exists()).toBe(false)
  })
})
