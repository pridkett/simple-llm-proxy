import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createWebHashHistory } from 'vue-router'
import { ref } from 'vue'

// --- Controllable session mock ---
// We need to be able to switch between admin and non-admin users per test.
const mockCurrentUser = ref({ id: 'u1', email: 'admin@example.com', name: 'Admin', is_admin: true })

vi.mock('@/composables/useSession.js', () => ({
  useSession: () => ({
    isAuthenticated: ref(true),
    currentUser: mockCurrentUser,
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
    createTeam: vi.fn(),
    deleteTeam: vi.fn(),
    teamMembers: vi.fn(),
    addTeamMember: vi.fn(),
    removeTeamMember: vi.fn(),
    updateTeamMemberRole: vi.fn(),
    users: vi.fn().mockResolvedValue([]),
  },
}))

import TeamsView from '@/views/TeamsView.vue'
import { api } from '@/api/client.js'

function makeRouter() {
  return createRouter({
    history: createWebHashHistory(),
    routes: [{ path: '/teams', name: 'teams', component: TeamsView }],
  })
}

const sampleTeams = [
  { id: 1, name: 'Alpha Team', created_at: '2024-01-01T00:00:00Z' },
  { id: 2, name: 'Beta Team', created_at: '2024-02-01T00:00:00Z' },
]

const sampleMembers = [
  { team_id: 1, user_id: 'u1', role: 'admin', user_email: 'alice@example.com', user_name: 'Alice' },
  { team_id: 1, user_id: 'u2', role: 'member', user_email: 'bob@example.com', user_name: 'Bob' },
]

describe('TeamsView', () => {
  beforeEach(() => {
    // Default to admin user
    mockCurrentUser.value = { id: 'u1', email: 'admin@example.com', name: 'Admin', is_admin: true }
    vi.mocked(api.teams).mockReset()
    vi.mocked(api.myTeams).mockReset()
    vi.mocked(api.createTeam).mockReset()
    vi.mocked(api.deleteTeam).mockReset()
    vi.mocked(api.teamMembers).mockReset()
    vi.mocked(api.addTeamMember).mockReset()
    vi.mocked(api.removeTeamMember).mockReset()
    vi.mocked(api.updateTeamMemberRole).mockReset()
    vi.mocked(api.users).mockResolvedValue([])
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('TestTeamsViewListsTeams: renders team names', async () => {
    vi.mocked(api.teams).mockResolvedValue(sampleTeams)

    const router = makeRouter()
    await router.push('/teams')
    const wrapper = mount(TeamsView, { global: { plugins: [router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('Alpha Team')
    expect(wrapper.text()).toContain('Beta Team')
    expect(api.teams).toHaveBeenCalledTimes(1)
  })

  it('TestTeamsViewCreateTeam: calls api.createTeam with form data', async () => {
    vi.mocked(api.teams).mockResolvedValue(sampleTeams)
    vi.mocked(api.createTeam).mockResolvedValue({ id: 3, name: 'New Team', created_at: '2024-03-01T00:00:00Z' })

    const router = makeRouter()
    await router.push('/teams')
    const wrapper = mount(TeamsView, { global: { plugins: [router] } })
    await flushPromises()

    // Find the text input in the create team form
    const nameInput = wrapper.find('input[type="text"]')
    expect(nameInput.exists()).toBe(true)
    await nameInput.setValue('New Team')

    // Submit the form
    const form = wrapper.find('form')
    expect(form.exists()).toBe(true)
    await form.trigger('submit')
    await flushPromises()

    expect(api.createTeam).toHaveBeenCalledWith({ name: 'New Team' })
  })

  it('TestTeamsViewDeleteTeam: calls api.deleteTeam with team id', async () => {
    vi.mocked(api.teams).mockResolvedValue(sampleTeams)
    vi.mocked(api.deleteTeam).mockResolvedValue(null)

    const router = makeRouter()
    await router.push('/teams')
    const wrapper = mount(TeamsView, { global: { plugins: [router] } })
    await flushPromises()

    // Find the delete button for the first team (Alpha Team, id=1)
    const deleteBtn = wrapper.find('[data-testid="delete-team-1"]')
    expect(deleteBtn.exists()).toBe(true)
    await deleteBtn.trigger('click')
    await flushPromises()

    // Confirm the deletion
    const confirmBtn = wrapper.find('[data-testid="confirm-delete-1"]')
    expect(confirmBtn.exists()).toBe(true)
    await confirmBtn.trigger('click')
    await flushPromises()

    expect(api.deleteTeam).toHaveBeenCalledWith(1)
  })

  it('TestTeamsViewMembersPanel: clicking team shows member names and roles', async () => {
    vi.mocked(api.teams).mockResolvedValue(sampleTeams)
    vi.mocked(api.teamMembers).mockResolvedValue(sampleMembers)

    const router = makeRouter()
    await router.push('/teams')
    const wrapper = mount(TeamsView, { global: { plugins: [router] } })
    await flushPromises()

    // Click on the first team to open member panel
    const teamItem = wrapper.find('[data-testid="team-item-1"]')
    expect(teamItem.exists()).toBe(true)
    await teamItem.trigger('click')
    await flushPromises()

    expect(api.teamMembers).toHaveBeenCalledWith(1)
    expect(wrapper.text()).toContain('Alice')
    expect(wrapper.text()).toContain('Bob')
  })

  // --- Non-admin tests ---

  it('TestTeamsViewNonAdminUsesMyTeams: non-admin calls api.myTeams instead of api.teams', async () => {
    mockCurrentUser.value = { id: 'u2', email: 'user@example.com', name: 'User', is_admin: false }
    vi.mocked(api.myTeams).mockResolvedValue([sampleTeams[0]])

    const router = makeRouter()
    await router.push('/teams')
    const wrapper = mount(TeamsView, { global: { plugins: [router] } })
    await flushPromises()

    expect(api.myTeams).toHaveBeenCalledTimes(1)
    expect(api.teams).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Alpha Team')
  })

  it('TestTeamsViewNonAdminHidesCreateTeam: non-admin does not see create team form', async () => {
    mockCurrentUser.value = { id: 'u2', email: 'user@example.com', name: 'User', is_admin: false }
    vi.mocked(api.myTeams).mockResolvedValue(sampleTeams)

    const router = makeRouter()
    await router.push('/teams')
    const wrapper = mount(TeamsView, { global: { plugins: [router] } })
    await flushPromises()

    // The create form should not exist
    const form = wrapper.find('form')
    expect(form.exists()).toBe(false)
  })

  it('TestTeamsViewNonAdminHidesDeleteButtons: non-admin does not see delete buttons', async () => {
    mockCurrentUser.value = { id: 'u2', email: 'user@example.com', name: 'User', is_admin: false }
    vi.mocked(api.myTeams).mockResolvedValue(sampleTeams)

    const router = makeRouter()
    await router.push('/teams')
    const wrapper = mount(TeamsView, { global: { plugins: [router] } })
    await flushPromises()

    const deleteBtn = wrapper.find('[data-testid="delete-team-1"]')
    expect(deleteBtn.exists()).toBe(false)
  })

  it('TestTeamsViewNonAdminReadOnlyMembers: non-admin sees members read-only without actions column', async () => {
    mockCurrentUser.value = { id: 'u2', email: 'user@example.com', name: 'User', is_admin: false }
    vi.mocked(api.myTeams).mockResolvedValue(sampleTeams)
    vi.mocked(api.teamMembers).mockResolvedValue(sampleMembers)

    const router = makeRouter()
    await router.push('/teams')
    const wrapper = mount(TeamsView, { global: { plugins: [router] } })
    await flushPromises()

    // Click a team to open detail
    const teamItem = wrapper.find('[data-testid="team-item-1"]')
    await teamItem.trigger('click')
    await flushPromises()

    // Members should be visible
    expect(wrapper.text()).toContain('Alice')
    expect(wrapper.text()).toContain('Bob')

    // Actions column header should not exist
    const headers = wrapper.findAll('th')
    const headerTexts = headers.map((h) => h.text())
    expect(headerTexts).not.toContain('Actions')

    // Role should be displayed as plain text, not a select dropdown
    const selects = wrapper.findAll('table select')
    expect(selects.length).toBe(0)

    // Remove buttons should not exist
    expect(wrapper.text()).not.toContain('Remove')

    // Add member form should not exist
    expect(wrapper.text()).not.toContain('Add Member')
  })

  it('TestTeamsViewErrorDoesNotReplaceList: error on create does not hide team list', async () => {
    vi.mocked(api.teams).mockResolvedValue(sampleTeams)
    vi.mocked(api.createTeam).mockRejectedValue(new Error('Forbidden'))

    const router = makeRouter()
    await router.push('/teams')
    const wrapper = mount(TeamsView, { global: { plugins: [router] } })
    await flushPromises()

    // Both teams visible initially
    expect(wrapper.text()).toContain('Alpha Team')
    expect(wrapper.text()).toContain('Beta Team')

    // Try to create a team (will fail)
    const nameInput = wrapper.find('input[type="text"]')
    await nameInput.setValue('Bad Team')
    const form = wrapper.find('form')
    await form.trigger('submit')
    await flushPromises()

    // Error should be shown inline
    expect(wrapper.text()).toContain('Forbidden')

    // Teams should still be visible (not replaced by error)
    expect(wrapper.text()).toContain('Alpha Team')
    expect(wrapper.text()).toContain('Beta Team')
  })
})
