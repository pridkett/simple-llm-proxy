import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createRouter, createWebHashHistory } from 'vue-router'

// Mock useSession to avoid fetch calls during LoginView mount
vi.mock('@/composables/useSession.js', () => ({
  useSession: () => ({
    isAuthenticated: { value: false },
    currentUser: { value: null },
    loading: { value: false },
    fetchCurrentUser: vi.fn().mockResolvedValue(false),
    clearSession: vi.fn(),
  }),
}))

import LoginView from '@/views/LoginView.vue'

// Create the router once at module level so createWebHashHistory() runs with real window.location
const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/login', name: 'login', component: LoginView, meta: { requiresAuth: false } },
    { path: '/', name: 'dashboard', component: { template: '<div/>' } },
  ],
})

describe('LoginView', () => {
  let hrefSpy

  beforeEach(async () => {
    // Use Object.defineProperty to intercept href assignment
    Object.defineProperty(window, 'location', {
      writable: true,
      value: {
        ...window.location,
        href: window.location.href,
      },
    })
    await router.push('/login')
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('Test 1: renders a Sign in button', async () => {
    const wrapper = mount(LoginView, { global: { plugins: [router] } })
    const button = wrapper.find('button')
    expect(button.exists()).toBe(true)
    expect(button.text()).toMatch(/sign in/i)
  })

  it('Test 2: clicking Sign in button sets window.location.href to /auth/login', async () => {
    // Spy on location.href setter
    let capturedHref = ''
    const locationMock = { href: '' }
    Object.defineProperty(locationMock, 'href', {
      get: () => capturedHref,
      set: (val) => { capturedHref = val },
    })
    vi.stubGlobal('location', locationMock)

    const wrapper = mount(LoginView, { global: { plugins: [router] } })
    const button = wrapper.find('button')
    await button.trigger('click')
    expect(capturedHref).toBe('/auth/login')
  })
})
