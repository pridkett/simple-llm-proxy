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
    myTeams: vi.fn(),
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

// myTeams returns TeamMember-shaped objects with team_id / team_name
const sampleMemberships = [
  { team_id: 1, team_name: 'Alpha Team', user_id: 'u1', role: 'admin' },
  { team_id: 2, team_name: 'Beta Team', user_id: 'u1', role: 'member' },
]

const sampleApps = [
  { id: 1, team_id: 1, name: 'App One', created_at: '2024-01-15T00:00:00Z' },
  { id: 2, team_id: 1, name: 'App Two', created_at: '2024-02-15T00:00:00Z' },
]

describe('ApplicationsView', () => {
  beforeEach(() => {
    // Test user is admin, so the component calls api.teams() not api.myTeams()
    vi.mocked(api.teams).mockResolvedValue(
      sampleMemberships.map((m) => ({ id: m.team_id, name: m.team_name }))
    )
    vi.mocked(api.myTeams).mockResolvedValue(sampleMemberships)
    vi.mocked(api.applications).mockReset()
    vi.mocked(api.createApplication).mockReset()
    vi.mocked(api.deleteApplication).mockReset()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('TestApplicationsViewTeamList: renders team names in left panel', async () => {
    const router = makeRouter()
    await router.push('/applications')
    const wrapper = mount(ApplicationsView, { global: { plugins: [router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('Alpha Team')
    expect(wrapper.text()).toContain('Beta Team')
    // Admin user hits api.teams(); non-admins would hit api.myTeams()
    expect(api.teams).toHaveBeenCalledTimes(1)
  })

  it('TestApplicationsViewLists: renders application names after selecting a team', async () => {
    vi.mocked(api.applications).mockResolvedValue(sampleApps)

    const router = makeRouter()
    await router.push('/applications')
    const wrapper = mount(ApplicationsView, { global: { plugins: [router] } })
    await flushPromises()

    // Click the first team in the left panel
    const teamItems = wrapper.findAll('li')
    const alphaItem = teamItems.find((li) => li.text().includes('Alpha Team'))
    expect(alphaItem).toBeDefined()
    await alphaItem.trigger('click')
    await flushPromises()

    expect(api.applications).toHaveBeenCalledWith(1)
    expect(wrapper.text()).toContain('App One')
    expect(wrapper.text()).toContain('App Two')
  })

  it('TestApplicationsViewCreate: calls api.createApplication with form data', async () => {
    vi.mocked(api.applications).mockResolvedValue(sampleApps)
    vi.mocked(api.createApplication).mockResolvedValue(null)

    const router = makeRouter()
    await router.push('/applications')
    const wrapper = mount(ApplicationsView, { global: { plugins: [router] } })
    await flushPromises()

    // Select a team first
    const teamItems = wrapper.findAll('li')
    const alphaItem = teamItems.find((li) => li.text().includes('Alpha Team'))
    await alphaItem.trigger('click')
    await flushPromises()

    // Find the app name input (the second text input — first is the team filter)
    const inputs = wrapper.findAll('input[type="text"]')
    const nameInput = inputs[inputs.length - 1]
    expect(nameInput.exists()).toBe(true)
    await nameInput.setValue('MyApp')

    // Submit the form
    const forms = wrapper.findAll('form')
    await forms[forms.length - 1].trigger('submit')
    await flushPromises()

    expect(api.createApplication).toHaveBeenCalledWith({ team_id: 1, name: 'MyApp' })
  })

  it('TestApplicationsViewDelete: calls api.deleteApplication with app id', async () => {
    vi.mocked(api.applications).mockResolvedValue(sampleApps)
    vi.mocked(api.deleteApplication).mockResolvedValue(null)

    const router = makeRouter()
    await router.push('/applications')
    const wrapper = mount(ApplicationsView, { global: { plugins: [router] } })
    await flushPromises()

    // Select a team
    const teamItems = wrapper.findAll('li')
    const alphaItem = teamItems.find((li) => li.text().includes('Alpha Team'))
    await alphaItem.trigger('click')
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
