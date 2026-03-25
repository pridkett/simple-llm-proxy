import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createWebHashHistory } from 'vue-router'
import { ref } from 'vue'

// Mock useSession to control auth state
vi.mock('@/composables/useSession.js', () => ({
  useSession: () => ({
    isAuthenticated: ref(true),
    currentUser: ref({ id: 'u1', email: 'admin@example.com', name: 'Admin', is_admin: true }),
    loading: ref(false),
    fetchCurrentUser: vi.fn().mockResolvedValue(true),
    clearSession: vi.fn(),
  }),
}))

// Mock the api client — factory must not reference external variables (hoisting)
vi.mock('@/api/client.js', () => ({
  api: {
    users: vi.fn(),
  },
}))

import UsersView from '@/views/UsersView.vue'
import { api } from '@/api/client.js'

function makeRouter() {
  return createRouter({
    history: createWebHashHistory(),
    routes: [{ path: '/users', name: 'users', component: UsersView }],
  })
}

describe('UsersView', () => {
  beforeEach(() => {
    vi.mocked(api.users).mockReset()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('TestUsersViewRenders: renders user rows and admin badge', async () => {
    vi.mocked(api.users).mockResolvedValue([
      { id: 'u1', email: 'alice@example.com', name: 'Alice', is_admin: true, created_at: '2024-01-01T00:00:00Z', last_seen: '2024-06-01T00:00:00Z' },
      { id: 'u2', email: 'bob@example.com', name: 'Bob', is_admin: false, created_at: '2024-02-01T00:00:00Z', last_seen: '2024-06-02T00:00:00Z' },
    ])

    const router = makeRouter()
    await router.push('/users')
    const wrapper = mount(UsersView, { global: { plugins: [router] } })
    await flushPromises()

    // Both users rendered
    expect(wrapper.text()).toContain('Alice')
    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.text()).toContain('Bob')
    expect(wrapper.text()).toContain('bob@example.com')

    // Admin badge shown for Alice (is_admin=true)
    expect(wrapper.text()).toContain('Admin')
  })
})
