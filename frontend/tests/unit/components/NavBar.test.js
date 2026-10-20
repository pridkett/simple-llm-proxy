import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { ref } from 'vue'

const mockIsAuthenticated = ref(false)
const mockCurrentUser = ref(null)
const mockClearSession = vi.fn()

vi.mock('@/composables/useSession.js', () => ({
  useSession: () => ({
    isAuthenticated: mockIsAuthenticated,
    currentUser: mockCurrentUser,
    clearSession: mockClearSession,
  }),
}))

vi.mock('@/api/client.js', () => ({
  api: {
    logout: vi.fn(),
    spend: vi.fn(),
  },
}))

import NavBar from '@/components/NavBar.vue'
import { api } from '@/api/client.js'

enableAutoUnmount(afterEach)

async function render({ user = { name: 'Alice Example', email: 'alice@example.com', is_admin: true }, path = '/' } = {}) {
  mockIsAuthenticated.value = Boolean(user)
  mockCurrentUser.value = user
  const paths = ['/', '/login', '/models', '/chat', '/logs', '/config', '/api-docs', '/settings', '/users', '/teams', '/teams/1/applications/1', '/applications', '/keys', '/cost', '/pools', '/webhooks', '/events']
  const router = createRouter({
    history: createMemoryHistory(),
    routes: paths.map(path => ({ path, name: path === '/login' ? 'login' : path, component: { template: '<div />' } })),
  })
  await router.push(path)
  const wrapper = mount(NavBar, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
  return { wrapper, router }
}

const accountButton = wrapper => wrapper.get('button[aria-controls="account-options"]')
const navigationButton = wrapper => wrapper.get('button[aria-controls="primary-navigation"]')

async function openAccount(wrapper) {
  await accountButton(wrapper).trigger('click')
  await flushPromises()
  return wrapper.get('#account-options')
}

describe('NavBar', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.logout).mockResolvedValue(null)
    vi.mocked(api.spend).mockResolvedValue({ alerts: [] })
  })

  it('renders all admin links and marks the active route', async () => {
    const { wrapper } = await render({ path: '/models' })
    const navigation = wrapper.get('nav[aria-label="Main navigation"]')
    expect(navigation.findAll('a').map(link => link.text())).toEqual([
      'Dashboard', 'Models', 'Chat', 'Logs', 'Config', 'API Docs',
      'Users', 'Teams', 'Cost', 'Pools', 'Webhooks', 'Events',
    ])
    expect(navigation.get('a[href="/models"]').attributes('aria-current')).toBe('page')
  })

  it('hides admin links and avoids admin spend requests for regular users', async () => {
    const { wrapper } = await render({ user: { name: 'Alice', email: 'alice@example.com', is_admin: false } })
    expect(wrapper.get('nav').findAll('a').map(link => link.text())).toEqual([
      'Dashboard', 'Models', 'Chat', 'Logs', 'Config', 'API Docs',
    ])
    expect(api.spend).not.toHaveBeenCalled()
    expect((await openAccount(wrapper)).text()).toContain('Logout')
  })

  it('keeps Teams active inside an application workspace', async () => {
    const { wrapper } = await render({ path: '/teams/1/applications/1' })
    expect(wrapper.get('nav a[href="/teams"]').attributes('aria-current')).toBe('page')
    expect(wrapper.get('nav').text()).not.toContain('Applications')
    expect(wrapper.get('nav').text()).not.toContain('Keys')
  })

  it('hides navigation and account controls when not authenticated', async () => {
    const { wrapper } = await render({ user: null })
    expect(wrapper.find('nav').exists()).toBe(false)
    expect(wrapper.find('button').exists()).toBe(false)
  })

  it('hides the entire header on the login route', async () => {
    const { wrapper } = await render({ user: null, path: '/login' })
    expect(wrapper.find('header').exists()).toBe(false)
  })

  it.each([
    [{ name: '  Alice   van Example ', email: 'alice@example.com' }, 'AE'],
    [{ name: 'Alice', email: 'alice@example.com' }, 'A'],
    [{ name: ' ', email: 'alice.example@example.com' }, 'AE'],
    [{ email: 'alice@example.com' }, 'A'],
    [{}, '?'],
  ])('uses initials with an email or unknown-user fallback: %j', async (user, expected) => {
    const { wrapper } = await render({ user })
    expect(accountButton(wrapper).text()).toBe(expected)
  })

  it('shows identity and actions only inside the opened disclosure', async () => {
    const email = `${'long.email.'.repeat(12)}example@example.com`
    const { wrapper } = await render({ user: { name: 'Alice Example', email } })
    expect(accountButton(wrapper).attributes('aria-expanded')).toBe('false')
    expect(wrapper.text()).not.toContain(email)
    expect(wrapper.text()).not.toContain('Logout')
    const panel = await openAccount(wrapper)
    expect(accountButton(wrapper).attributes('aria-expanded')).toBe('true')
    expect(panel.text()).toContain(email)
    expect(panel.text()).toContain('Alice Example')
    expect(panel.get('a').text()).toBe('Proxy settings')
    expect(document.activeElement).toBe(panel.get('a').element)
    await accountButton(wrapper).trigger('click')
    expect(wrapper.find('#account-options').exists()).toBe(false)
  })

  it('closes on Escape and returns focus to the account trigger', async () => {
    const { wrapper } = await render()
    await openAccount(wrapper)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(wrapper.find('#account-options').exists()).toBe(false)
    expect(document.activeElement).toBe(accountButton(wrapper).element)
  })

  it('keeps the account open for inside clicks and closes for outside clicks', async () => {
    const { wrapper } = await render()
    const panel = await openAccount(wrapper)
    await panel.trigger('pointerdown')
    expect(wrapper.find('#account-options').exists()).toBe(true)
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(wrapper.find('#account-options').exists()).toBe(false)
  })

  it('dismisses the account when focus moves outside without stealing focus', async () => {
    const { wrapper } = await render()
    await openAccount(wrapper)
    const link = wrapper.get('nav a').element
    link.focus()
    await flushPromises()
    expect(wrapper.find('#account-options').exists()).toBe(false)
    expect(document.activeElement).toBe(link)
  })

  it('opens real proxy settings and closes the disclosure', async () => {
    const { wrapper, router } = await render()
    const panel = await openAccount(wrapper)
    await panel.get('a').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/settings')
    expect(wrapper.find('#account-options').exists()).toBe(false)
  })

  it.each([false, true])('clears the session and returns to login when logout fails: %s', async fails => {
    if (fails) vi.mocked(api.logout).mockRejectedValueOnce(new Error('Network error'))
    const { wrapper, router } = await render()
    const panel = await openAccount(wrapper)
    await panel.get('button').trigger('click')
    await flushPromises()
    expect(api.logout).toHaveBeenCalledOnce()
    expect(mockClearSession).toHaveBeenCalledOnce()
    expect(router.currentRoute.value.path).toBe('/login')
    expect(wrapper.find('header').exists()).toBe(false)
  })

  it('toggles mobile navigation and closes it on selection with focus restored', async () => {
    const { wrapper, router } = await render()
    expect(navigationButton(wrapper).attributes('aria-expanded')).toBe('false')
    await navigationButton(wrapper).trigger('click')
    expect(navigationButton(wrapper).attributes('aria-expanded')).toBe('true')
    await wrapper.get('nav a[href="/models"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/models')
    expect(navigationButton(wrapper).attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(navigationButton(wrapper).element)
  })

  it('closes mobile navigation on Escape and on opening the account', async () => {
    const { wrapper } = await render()
    await navigationButton(wrapper).trigger('click')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(navigationButton(wrapper).attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(navigationButton(wrapper).element)
    await navigationButton(wrapper).trigger('click')
    await openAccount(wrapper)
    expect(navigationButton(wrapper).attributes('aria-expanded')).toBe('false')
    await navigationButton(wrapper).trigger('click')
    expect(wrapper.find('#account-options').exists()).toBe(false)
  })

  it('dismisses disclosures on external route changes', async () => {
    const { wrapper, router } = await render()
    await openAccount(wrapper)
    await router.push('/models')
    await flushPromises()
    expect(wrapper.find('#account-options').exists()).toBe(false)
    await navigationButton(wrapper).trigger('click')
    await router.push('/logs')
    await flushPromises()
    expect(navigationButton(wrapper).attributes('aria-expanded')).toBe('false')
  })

  it('removes global listeners and route hooks on unmount', async () => {
    const { wrapper, router } = await render()
    const removeListener = vi.spyOn(document, 'removeEventListener')
    wrapper.unmount()
    expect(removeListener).toHaveBeenCalledWith('pointerdown', expect.any(Function))
    expect(removeListener).toHaveBeenCalledWith('keydown', expect.any(Function))
    const calls = vi.mocked(api.spend).mock.calls.length
    await router.push('/models')
    expect(api.spend).toHaveBeenCalledTimes(calls)
    removeListener.mockRestore()
  })
})
