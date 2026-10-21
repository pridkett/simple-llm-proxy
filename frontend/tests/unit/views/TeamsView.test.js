import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { createRouter, createMemoryHistory, RouterView } from 'vue-router'
import { ref } from 'vue'

const user = ref({ id: 'u1', is_admin: true })
vi.mock('@/composables/useSession.js', () => ({
  useSession: () => ({ currentUser: user })
}))
vi.mock('@/api/client.js', () => ({
  api: Object.fromEntries(
    [
      'teams',
      'myTeams',
      'createTeam',
      'deleteTeam',
      'teamMembers',
      'addTeamMember',
      'removeTeamMember',
      'updateTeamMemberRole',
      'users',
      'applications',
      'createApplication',
      'deleteApplication',
      'apiKeys',
      'createAPIKey',
      'updateAPIKey',
      'revokeAPIKey',
      'models',
      'spend'
    ].map((name) => [name, vi.fn()])
  )
}))
import { api } from '@/api/client.js'
import { teamRoutes } from '@/router/teamRoutes.js'

enableAutoUnmount(afterEach)
const clone = (data) => JSON.parse(JSON.stringify(data))
let teams, apps, keys
const secret = 'sk-app-' + 'a'.repeat(48)

async function render(path = '/teams') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: teamRoutes
  })
  await router.push(path)
  const wrapper = mount(RouterView, {
    attachTo: document.body,
    global: { plugins: [router] }
  })
  await flushPromises()
  return { router, wrapper }
}
function button(wrapper, label) {
  return wrapper.findAll('button').find((b) => b.text() === label)
}

beforeEach(() => {
  vi.resetAllMocks()
  user.value = { id: 'u1', is_admin: true }
  teams = [
    { id: 1, name: 'Platform' },
    { id: 2, name: 'Research' }
  ]
  apps = [
    { id: 1, team_id: 1, name: 'Support assistant', created_at: '2026-10-01' },
    { id: 2, team_id: 2, name: 'Evaluations', created_at: '2026-10-01' }
  ]
  keys = {
    1: [
      {
        id: 1,
        application_id: 1,
        name: 'Production',
        key_prefix: 'abcd1234',
        is_active: true,
        max_budget: 20,
        allowed_models: []
      }
    ],
    2: []
  }
  api.teams.mockImplementation(async () => clone(teams))
  api.myTeams.mockResolvedValue([
    { team_id: 1, team_name: 'Platform', role: 'viewer' }
  ])
  api.applications.mockImplementation(async (teamId) =>
    clone(apps.filter((a) => a.team_id === teamId))
  )
  api.apiKeys.mockImplementation(async (appId) => clone(keys[appId] ?? []))
  api.createTeam.mockImplementation(async (body) => {
    const t = { id: 3, ...body }
    teams.push(t)
    return clone(t)
  })
  api.createApplication.mockImplementation(async (body) => {
    const a = { id: 3, ...body, created_at: '2026-10-20' }
    apps.push(a)
    return clone(a)
  })
  api.createAPIKey.mockImplementation(async (appId, body) => {
    const k = {
      id: 10,
      application_id: appId,
      ...body,
      key_prefix: 'aaaaaaaa',
      is_active: true,
      allowed_models: []
    }
    keys[appId] = [...(keys[appId] ?? []), k]
    return { ...k, key: secret }
  })
  api.updateAPIKey.mockImplementation(async (id, body) => {
    Object.assign(
      keys[1].find((k) => k.id === id),
      body
    )
  })
  api.revokeAPIKey.mockImplementation(async (id) => {
    keys[1].find((k) => k.id === id).is_active = false
  })
  api.teamMembers.mockResolvedValue([
    {
      user_id: 'u1',
      user_name: 'Alex',
      user_email: 'alex@example.com',
      role: 'admin'
    }
  ])
  api.users.mockResolvedValue([
    { id: 'u2', name: 'Sam', email: 'sam@example.com' }
  ])
  api.models.mockResolvedValue({ data: [] })
  api.spend.mockResolvedValue({ rows: [] })
})

describe('Teams workspace', () => {
  it('opens the overview without a root breadcrumb or redundant row actions', async () => {
    const { wrapper } = await render()
    expect(wrapper.get('h1').text()).toBe('Teams')
    expect(wrapper.find('nav[aria-label="Breadcrumb"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Open workspace')
    expect(wrapper.get('a[href="/teams/new"]').text()).toBe('Create team')
    expect(wrapper.get('a[href="/teams/1"]').text()).toBe('Platform')
  })

  it('creates a team with only a name and opens its empty workspace', async () => {
    const { wrapper, router } = await render('/teams/new')
    expect(wrapper.findAll('input')).toHaveLength(1)
    await wrapper.get('#team-name').setValue('Engineering')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.createTeam).toHaveBeenCalledWith({ name: 'Engineering' })
    expect(api.createApplication).not.toHaveBeenCalled()
    expect(api.createAPIKey).not.toHaveBeenCalled()
    expect(router.currentRoute.value.path).toBe('/teams/3')
    expect(wrapper.get('h1').text()).toBe('Engineering')
    expect(wrapper.text()).toContain('No applications yet.')
  })

  it('automatically creates an unlimited Default key, reveals it once, and never persists it', async () => {
    const { wrapper, router } = await render('/teams/1/applications/new')
    await wrapper.get('#application-name').setValue('New service')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.createApplication).toHaveBeenCalledTimes(1)
    expect(api.createApplication).toHaveBeenCalledWith({
      team_id: 1,
      name: 'New service'
    })
    expect(api.createAPIKey).toHaveBeenCalledTimes(1)
    expect(api.createAPIKey).toHaveBeenCalledWith(3, { name: 'Default' })
    expect(router.currentRoute.value.path).toBe('/teams/1/applications/3')
    expect(wrapper.get('dialog code').text()).toBe(secret)
    expect(router.currentRoute.value.fullPath).not.toContain(secret)
    expect(JSON.stringify(localStorage)).not.toContain(secret)
    expect(JSON.stringify(sessionStorage)).not.toContain(secret)
    await router.push('/teams')
    expect(router.currentRoute.value.path).toBe('/teams/1/applications/3')
    await button(wrapper, 'Done').trigger('click')
    expect(wrapper.find('dialog').exists()).toBe(false)
    await router.push('/teams')
    expect(router.currentRoute.value.path).toBe('/teams')
  })

  it('retries only the key after a partial creation failure', async () => {
    api.createAPIKey.mockRejectedValueOnce(new Error('Request failed'))
    const { wrapper, router } = await render('/teams/1/applications/new')
    await wrapper.get('#application-name').setValue('New service')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain(
      'Application created, but key creation failed'
    )
    expect(wrapper.text()).toContain('Retry key creation')
    expect(wrapper.get('#application-name').attributes()).toHaveProperty(
      'readonly'
    )
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.createApplication).toHaveBeenCalledTimes(1)
    expect(api.createAPIKey).toHaveBeenCalledTimes(2)
    expect(router.currentRoute.value.path).toBe('/teams/1/applications/3')
    expect(wrapper.get('dialog code').text()).toBe(secret)
  })

  it('does not duplicate an in-flight application submission', async () => {
    let resolve
    api.createApplication.mockImplementationOnce(
      () =>
        new Promise((r) => {
          resolve = r
        })
    )
    const { wrapper, router } = await render('/teams/1/applications/new')
    await wrapper.get('#application-name').setValue('New service')
    await wrapper.get('form').trigger('submit')
    await wrapper.get('form').trigger('submit')
    await router.push('/teams/2')
    expect(router.currentRoute.value.path).toBe('/teams/1/applications/new')
    expect(api.createApplication).toHaveBeenCalledTimes(1)
    resolve({ id: 3, team_id: 1, name: 'New service' })
    await flushPromises()
    expect(wrapper.get('dialog code').text()).toBe(secret)
  })

  it('shows application and key breadcrumbs on a fresh direct link', async () => {
    const { wrapper, router } = await render('/teams/1/applications/1/keys/1')
    const crumbs = wrapper.get('nav[aria-label="Breadcrumb"]')
    expect(crumbs.text()).toContain('Teams')
    expect(crumbs.text()).toContain('Platform')
    expect(crumbs.text()).toContain('Support assistant')
    expect(crumbs.text()).toContain('Production')
    await crumbs.get('a[href="/teams/1"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/teams/1')
    expect(wrapper.text()).not.toContain('Save Changes')
    router.back()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe(
      '/teams/1/applications/1/keys/1'
    )
    expect(wrapper.text()).toContain('Save Changes')
  })

  it('switches team without keeping the previous application or key', async () => {
    const { wrapper, router } = await render('/teams/1/applications/1/keys/1')
    await wrapper.get('select[aria-label="Switch team"]').setValue('2')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/teams/2')
    expect(wrapper.text()).toContain('Evaluations')
    expect(wrapper.text()).not.toContain('Support assistant')
    expect(wrapper.text()).not.toContain('Production')
  })

  it('does not leave application loading stuck when navigating during key-count loading', async () => {
    let resolve
    api.apiKeys.mockImplementationOnce(
      () =>
        new Promise((r) => {
          resolve = r
        })
    )
    const { wrapper, router } = await render('/teams/1')
    await router.push('/teams/1/applications/1')
    resolve([])
    await flushPromises()
    expect(wrapper.text()).not.toContain('Loading applications')
    expect(wrapper.text()).toContain('Support assistant')
  })

  it('treats missing counts as unknown instead of zero', async () => {
    api.apiKeys.mockRejectedValue(new Error('Offline'))
    const { wrapper } = await render('/teams')
    const row = wrapper.findAll('tbody tr')[0]
    expect(row.text()).toContain('—')
  })

  it.each([
    ['/keys?team_id=1&app_id=1', '/teams/1/applications/1'],
    ['/applications?team_id=2', '/teams/2'],
    ['/applications', '/teams'],
    ['/keys?team_id=bad&app_id=1', '/teams']
  ])('preserves legacy navigation from %s', async (oldPath, path) => {
    const { router } = await render(oldPath)
    expect(router.currentRoute.value.path).toBe(path)
  })

  it('keeps non-admin membership results readable and hides mutations', async () => {
    user.value = { id: 'u2', is_admin: false }
    const { wrapper, router } = await render('/teams')
    expect(wrapper.text()).toContain('Platform')
    expect(wrapper.text()).not.toContain('Create team')
    expect(api.teams).not.toHaveBeenCalled()
    expect(api.teamMembers).not.toHaveBeenCalled()
    await router.push('/teams/1/applications/1')
    await flushPromises()
    expect(wrapper.text()).toContain('Production')
    expect(wrapper.text()).not.toContain('Create key')
    expect(wrapper.text()).not.toContain('Revoke')
    await router.push('/teams/new')
    await flushPromises()
    expect(wrapper.find('form').exists()).toBe(false)
  })

  it('loads members only inside their team and retains member administration', async () => {
    const { wrapper } = await render('/teams/1/members')
    expect(api.teamMembers).toHaveBeenCalledWith(1)
    expect(wrapper.text()).toContain('Alex')
    const input = wrapper.get('input[placeholder="Search by name or email"]')
    await input.setValue('Sam')
    await input.trigger('input')
    await wrapper.get('ul li').trigger('mousedown')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.addTeamMember).toHaveBeenCalledWith(1, {
      user_id: 'u2',
      role: 'member'
    })
    const role = wrapper.get('table select')
    await role.setValue('viewer')
    await flushPromises()
    expect(api.updateTeamMemberRole).toHaveBeenCalledWith(1, 'u1', {
      role: 'viewer'
    })
  })

  it('keeps a mismatched application out of the selected team', async () => {
    const { wrapper } = await render('/teams/1/applications/2')
    expect(wrapper.text()).toContain('Application not found in this team')
    expect(api.apiKeys).not.toHaveBeenCalledWith(2)
  })

  it('requires confirmation before deleting an application and its keys', async () => {
    const { wrapper } = await render('/teams/1')
    await wrapper
      .get('button[aria-label="Delete Support assistant"]')
      .trigger('click')
    expect(api.deleteApplication).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('and all its keys?')
    await button(wrapper, 'Delete').trigger('click')
    await flushPromises()
    expect(api.deleteApplication).toHaveBeenCalledWith(1)
    expect(wrapper.text()).not.toContain('Support assistant')
  })

  it('edits key limits through a direct key settings route', async () => {
    const { wrapper, router } = await render('/teams/1/applications/1/keys/1')
    const inputs = wrapper.findAll('input[type="number"]')
    await inputs[0].setValue(5)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.updateAPIKey).toHaveBeenCalledWith(
      1,
      expect.objectContaining({
        name: 'Production',
        max_rpm: 5,
        max_budget: 20
      })
    )
    expect(router.currentRoute.value.path).toBe('/teams/1/applications/1')
  })

  it('creates an additional unlimited key and returns to the application before showing it', async () => {
    const { wrapper, router } = await render('/teams/1/applications/1/keys/new')
    await wrapper.get('input[placeholder="Key name"]').setValue('Worker')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.createAPIKey).toHaveBeenCalledWith(1, { name: 'Worker', allow_decisions: false })
    expect(router.currentRoute.value.path).toBe('/teams/1/applications/1')
    expect(wrapper.get('dialog code').text()).toBe(secret)
  })

  it('requires explicit decision opt-in and keeps the per-model allowlist', async () => {
    const { wrapper } = await render('/teams/1/applications/1/keys/new')
    await wrapper.get('input[placeholder="Key name"]').setValue('Decision worker')
    const permission = wrapper.get('#key-allow-decisions')
    expect(permission.element.checked).toBe(false)
    await permission.setValue(true)
    await wrapper.get('#key-models').setValue('jev, luna')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.createAPIKey).toHaveBeenCalledWith(1, {
      name: 'Decision worker', allow_decisions: true, allowed_models: ['jev', 'luna']
    })
  })

  it('loads an existing decision permission and can disable it', async () => {
    keys[1][0].allow_decisions = true
    const { wrapper } = await render('/teams/1/applications/1/keys/1')
    expect(wrapper.get('#key-allow-decisions').element.checked).toBe(true)
    await wrapper.get('#key-allow-decisions').setValue(false)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.updateAPIKey).toHaveBeenCalledWith(1, expect.objectContaining({ allow_decisions: false }))
  })

  it('revokes a key with confirmation and retains a read-only settings view', async () => {
    const { wrapper, router } = await render('/teams/1/applications/1')
    await wrapper.get('[data-testid="revoke-key-1"]').trigger('click')
    expect(api.revokeAPIKey).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="confirm-revoke-1"]').trigger('click')
    await flushPromises()
    expect(api.revokeAPIKey).toHaveBeenCalledWith(1)
    await router.push('/teams/1/applications/1/keys/1')
    await flushPromises()
    expect(wrapper.text()).toContain('This key has been revoked')
    expect(wrapper.find('form').exists()).toBe(false)
  })
})
