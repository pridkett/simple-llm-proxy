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
    teams: vi.fn(),
    applications: vi.fn(),
    createApplication: vi.fn(),
    deleteApplication: vi.fn(),
  },
}))

import ApplicationsView from '@/views/ApplicationsView.vue'
import { api } from '@/api/client.js'

function makeRouter() {
  return createRouter({
    history: createWebHashHistory(),
    routes: [{ path: '/applications', name: 'applications', component: ApplicationsView }],
  })
}

const sampleTeams = [
  { id: 1, name: 'Alpha Team', created_at: '2024-01-01T00:00:00Z' },
  { id: 2, name: 'Beta Team', created_at: '2024-02-01T00:00:00Z' },
]

const sampleApps = [
  { id: 1, team_id: 1, name: 'App One', created_at: '2024-01-15T00:00:00Z' },
  { id: 2, team_id: 1, name: 'App Two', created_at: '2024-02-15T00:00:00Z' },
]

describe('ApplicationsView', () => {
  beforeEach(() => {
    vi.mocked(api.teams).mockReset()
    vi.mocked(api.applications).mockReset()
    vi.mocked(api.createApplication).mockReset()
    vi.mocked(api.deleteApplication).mockReset()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('TestApplicationsViewLists: renders application names after selecting a team', async () => {
    vi.mocked(api.teams).mockResolvedValue(sampleTeams)
    vi.mocked(api.applications).mockResolvedValue(sampleApps)

    const router = makeRouter()
    await router.push('/applications')
    const wrapper = mount(ApplicationsView, { global: { plugins: [router] } })
    await flushPromises()

    // Select team 1 from the dropdown
    const select = wrapper.find('select')
    expect(select.exists()).toBe(true)
    await select.setValue('1')
    await flushPromises()

    expect(api.applications).toHaveBeenCalledWith(1)
    expect(wrapper.text()).toContain('App One')
    expect(wrapper.text()).toContain('App Two')
  })

  it('TestApplicationsViewCreate: calls api.createApplication with form data', async () => {
    vi.mocked(api.teams).mockResolvedValue(sampleTeams)
    vi.mocked(api.applications).mockResolvedValue(sampleApps)
    vi.mocked(api.createApplication).mockResolvedValue({ id: 3, team_id: 1, name: 'MyApp', created_at: '2024-03-01T00:00:00Z' })

    const router = makeRouter()
    await router.push('/applications')
    const wrapper = mount(ApplicationsView, { global: { plugins: [router] } })
    await flushPromises()

    // Select a team first
    const select = wrapper.find('select')
    await select.setValue('1')
    await flushPromises()

    // Find the app name input and fill it
    const nameInput = wrapper.find('input[type="text"]')
    expect(nameInput.exists()).toBe(true)
    await nameInput.setValue('MyApp')

    // Submit the form
    const form = wrapper.find('form')
    expect(form.exists()).toBe(true)
    await form.trigger('submit')
    await flushPromises()

    expect(api.createApplication).toHaveBeenCalledWith({ team_id: 1, name: 'MyApp' })
  })

  it('TestApplicationsViewDelete: calls api.deleteApplication with app id', async () => {
    vi.mocked(api.teams).mockResolvedValue(sampleTeams)
    vi.mocked(api.applications).mockResolvedValue(sampleApps)
    vi.mocked(api.deleteApplication).mockResolvedValue(null)

    const router = makeRouter()
    await router.push('/applications')
    const wrapper = mount(ApplicationsView, { global: { plugins: [router] } })
    await flushPromises()

    // Select a team
    const select = wrapper.find('select')
    await select.setValue('1')
    await flushPromises()

    // Find and click the delete button for app 1
    const deleteBtn = wrapper.find('[data-testid="delete-app-1"]')
    expect(deleteBtn.exists()).toBe(true)
    await deleteBtn.trigger('click')
    await flushPromises()

    // Confirm the deletion
    const confirmBtn = wrapper.find('[data-testid="confirm-delete-app-1"]')
    expect(confirmBtn.exists()).toBe(true)
    await confirmBtn.trigger('click')
    await flushPromises()

    expect(api.deleteApplication).toHaveBeenCalledWith(1)
  })
})
