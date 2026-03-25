import { describe, it, expect, vi, beforeEach } from 'vitest'

// useSession uses module-level singleton state.
// We must reset the module between tests that require a clean state,
// BUT the singleton test (Test 4) must use the same module instance.
describe('useSession', () => {
  beforeEach(() => {
    vi.resetModules()
    global.fetch = vi.fn()
  })

  it('Test 1: fetchCurrentUser sets isAuthenticated=true and stores user on 200', async () => {
    const user = { id: 'sub1', email: 'a@b.com', name: 'Alice', is_admin: true }
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve(user),
    })

    const { useSession } = await import('@/composables/useSession.js')
    const { fetchCurrentUser, isAuthenticated, currentUser } = useSession()

    await fetchCurrentUser()

    expect(isAuthenticated.value).toBe(true)
    expect(currentUser.value).toEqual(user)
    expect(currentUser.value.email).toBe('a@b.com')
  })

  it('Test 2: fetchCurrentUser sets isAuthenticated=false and currentUser=null on 401, no throw', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 401,
      json: () => Promise.resolve({ error: 'unauthorized' }),
    })

    const { useSession } = await import('@/composables/useSession.js')
    const { fetchCurrentUser, isAuthenticated, currentUser } = useSession()

    // Should NOT throw — 401 is expected unauthenticated state
    let threw = false
    try {
      await fetchCurrentUser()
    } catch {
      threw = true
    }

    expect(threw).toBe(false)
    expect(isAuthenticated.value).toBe(false)
    expect(currentUser.value).toBeNull()
  })

  it('Test 3: clearSession sets isAuthenticated=false and currentUser=null', async () => {
    // First authenticate
    const user = { id: 'sub1', email: 'a@b.com', name: 'Alice', is_admin: true }
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve(user),
    })

    const { useSession } = await import('@/composables/useSession.js')
    const { fetchCurrentUser, clearSession, isAuthenticated, currentUser } = useSession()

    await fetchCurrentUser()
    expect(isAuthenticated.value).toBe(true)

    clearSession()

    expect(isAuthenticated.value).toBe(false)
    expect(currentUser.value).toBeNull()
  })

  it('Test 4 (singleton): two imports share the same reactive state', async () => {
    const user = { id: 'sub1', email: 'a@b.com', name: 'Alice', is_admin: true }
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve(user),
    })

    // Import TWICE — in a real module system these resolve to the same cached module
    const mod1 = await import('@/composables/useSession.js')
    const mod2 = await import('@/composables/useSession.js')

    const session1 = mod1.useSession()
    const session2 = mod2.useSession()

    // Initially both unauthenticated
    expect(session1.isAuthenticated.value).toBe(false)
    expect(session2.isAuthenticated.value).toBe(false)

    // Authenticate via session1
    await session1.fetchCurrentUser()

    // session2 should see the same state (module-level singleton)
    expect(session1.isAuthenticated.value).toBe(true)
    expect(session2.isAuthenticated.value).toBe(true)
    expect(session2.currentUser.value?.email).toBe('a@b.com')
  })
})
