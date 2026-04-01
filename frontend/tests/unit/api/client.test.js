import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// We import api after mocking fetch
describe('api client', () => {
  let api

  beforeEach(async () => {
    localStorage.clear()
    vi.resetModules()
    // Stub global fetch
    global.fetch = vi.fn()
    // Mock useSession so client.js can import it without /admin/me calls
    vi.doMock('@/composables/useSession.js', () => ({
      useSession: () => ({
        isAuthenticated: { value: false },
        currentUser: { value: null },
        loading: { value: false },
        fetchCurrentUser: vi.fn().mockResolvedValue(false),
        clearSession: vi.fn(),
      }),
    }))
    // Mock router to control currentRoute
    vi.doMock('@/router/index.js', () => ({
      default: {
        currentRoute: { value: { path: '/dashboard' } },
      },
    }))
    const mod = await import('@/api/client.js')
    api = mod.api
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.clearAllMocks()
  })

  function mockFetch(data, status = 200) {
    global.fetch.mockResolvedValue({
      ok: status < 400,
      status,
      json: () => Promise.resolve(data),
    })
  }

  function mockFetchError(message, status = 500) {
    global.fetch.mockResolvedValue({
      ok: false,
      status,
      json: () => Promise.resolve({ error: { message } }),
    })
  }

  describe('health()', () => {
    it('calls /health with no auth header', async () => {
      mockFetch({ status: 'healthy' })
      const result = await api.health()
      expect(result).toEqual({ status: 'healthy' })
      expect(global.fetch).toHaveBeenCalledWith('/health')
    })
  })

  describe('status()', () => {
    it('calls GET /admin/status', async () => {
      const payload = { status: 'healthy', uptime_seconds: 42, models: [] }
      mockFetch(payload)
      const result = await api.status()
      expect(result).toEqual(payload)
      const [url, opts] = global.fetch.mock.calls[0]
      expect(url).toBe('/admin/status')
      expect(opts.method).toBeUndefined() // default GET
    })

    it('throws with the server error message on non-401 failure', async () => {
      mockFetchError('Server Error', 500)
      await expect(api.status()).rejects.toThrow('Server Error')
    })
  })

  describe('logs()', () => {
    it('calls GET /admin/logs with no params by default', async () => {
      mockFetch({ logs: [], total: 0, limit: 50, offset: 0 })
      await api.logs()
      const [url] = global.fetch.mock.calls[0]
      expect(url).toBe('/admin/logs')
    })

    it('appends limit and offset query params when provided', async () => {
      mockFetch({ logs: [], total: 0, limit: 10, offset: 20 })
      await api.logs({ limit: 10, offset: 20 })
      const [url] = global.fetch.mock.calls[0]
      expect(url).toBe('/admin/logs?limit=10&offset=20')
    })
  })

  describe('config()', () => {
    it('calls GET /admin/config', async () => {
      const payload = { model_list: [], router_settings: {}, general_settings: {} }
      mockFetch(payload)
      const result = await api.config()
      expect(result).toEqual(payload)
      const [url] = global.fetch.mock.calls[0]
      expect(url).toBe('/admin/config')
    })
  })

  describe('reload()', () => {
    it('POSTs to /admin/reload', async () => {
      mockFetch({ status: 'ok' })
      const result = await api.reload()
      expect(result).toEqual({ status: 'ok' })
      const [url, opts] = global.fetch.mock.calls[0]
      expect(url).toBe('/admin/reload')
      expect(opts.method).toBe('POST')
    })

    it('throws with server error message on failure', async () => {
      mockFetchError('failed to reload config file', 500)
      await expect(api.reload()).rejects.toThrow('failed to reload config file')
    })
  })

  describe('models()', () => {
    it('calls GET /v1/models', async () => {
      const payload = { object: 'list', data: [] }
      mockFetch(payload)
      const result = await api.models()
      expect(result).toEqual(payload)
      const [url] = global.fetch.mock.calls[0]
      expect(url).toBe('/v1/models')
    })
  })

  describe('chatCompletion()', () => {
    it('POSTs to /v1/chat/completions with model and messages', async () => {
      const payload = { choices: [{ message: { content: 'Hi' } }] }
      mockFetch(payload)
      const msgs = [{ role: 'user', content: 'Hello' }]
      const result = await api.chatCompletion('gpt-4', msgs)
      expect(result).toEqual(payload)
      const [url, opts] = global.fetch.mock.calls[0]
      expect(url).toBe('/v1/chat/completions')
      expect(opts.method).toBe('POST')
      const body = JSON.parse(opts.body)
      expect(body.model).toBe('gpt-4')
      expect(body.messages).toEqual(msgs)
      expect(body.stream).toBe(false)
    })

    it('includes temperature when provided', async () => {
      mockFetch({ choices: [] })
      await api.chatCompletion('gpt-4', [], { temperature: 0.3 })
      const [, opts] = global.fetch.mock.calls[0]
      expect(JSON.parse(opts.body).temperature).toBe(0.3)
    })

    it('omits temperature when not provided', async () => {
      mockFetch({ choices: [] })
      await api.chatCompletion('gpt-4', [])
      const [, opts] = global.fetch.mock.calls[0]
      expect(JSON.parse(opts.body).temperature).toBeUndefined()
    })
  })

  describe('chatCompletionStream()', () => {
    it('POSTs with stream:true and returns the response body', async () => {
      const mockBody = {}
      global.fetch.mockResolvedValue({ ok: true, status: 200, body: mockBody })
      const result = await api.chatCompletionStream('gpt-4', [{ role: 'user', content: 'Hi' }])
      expect(result).toBe(mockBody)
      const [url, opts] = global.fetch.mock.calls[0]
      expect(url).toBe('/v1/chat/completions')
      expect(JSON.parse(opts.body).stream).toBe(true)
    })

    it('throws with server error message on non-ok response', async () => {
      global.fetch.mockResolvedValue({
        ok: false,
        status: 500,
        json: () => Promise.resolve({ error: { message: 'Server Error' } }),
      })
      await expect(api.chatCompletionStream('gpt-4', [])).rejects.toThrow('Server Error')
    })

    it('passes AbortSignal through to fetch', async () => {
      const mockBody = {}
      global.fetch.mockResolvedValue({ ok: true, status: 200, body: mockBody })
      const controller = new AbortController()
      await api.chatCompletionStream('gpt-4', [], { signal: controller.signal })
      const [, opts] = global.fetch.mock.calls[0]
      expect(opts.signal).toBe(controller.signal)
    })
  })

  describe('costMapStatus()', () => {
    it('calls GET /admin/costmap', async () => {
      const payload = { loaded: true, model_count: 100, url: 'https://example.com' }
      mockFetch(payload)
      const result = await api.costMapStatus()
      expect(result).toEqual(payload)
      const [url, opts] = global.fetch.mock.calls[0]
      expect(url).toBe('/admin/costmap')
      expect(opts?.method).toBeUndefined()
    })

    it('throws on server error', async () => {
      mockFetchError('Server Error', 500)
      await expect(api.costMapStatus()).rejects.toThrow('Server Error')
    })
  })

  describe('costMapReload()', () => {
    it('POSTs to /admin/costmap/reload', async () => {
      mockFetch({ status: 'ok', model_count: 100 })
      const result = await api.costMapReload()
      expect(result).toEqual({ status: 'ok', model_count: 100 })
      const [url, opts] = global.fetch.mock.calls[0]
      expect(url).toBe('/admin/costmap/reload')
      expect(opts.method).toBe('POST')
    })

    it('throws on server error', async () => {
      mockFetchError('failed to reload cost map', 500)
      await expect(api.costMapReload()).rejects.toThrow('failed to reload cost map')
    })
  })

  describe('costMapSetURL()', () => {
    it('PUTs to /admin/costmap/url with the url in the body', async () => {
      const newURL = 'https://example.com/models.json'
      mockFetch({ url: newURL })
      const result = await api.costMapSetURL(newURL)
      expect(result).toEqual({ url: newURL })
      const [url, opts] = global.fetch.mock.calls[0]
      expect(url).toBe('/admin/costmap/url')
      expect(opts.method).toBe('PUT')
      expect(JSON.parse(opts.body)).toEqual({ url: newURL })
    })

    it('throws on invalid URL error from server', async () => {
      mockFetchError('URL scheme must be http or https', 400)
      await expect(api.costMapSetURL('ftp://bad')).rejects.toThrow('URL scheme must be http or https')
    })
  })

  // ── New session-auth tests ─────────────────────────────────────────────────

  describe('Test A: credentials:include on every request, no Authorization header', () => {
    it('sends credentials:include and no Authorization header', async () => {
      mockFetch({ status: 'healthy', models: [], uptime_seconds: 0 })
      await api.status()
      const [, opts] = global.fetch.mock.calls[0]
      expect(opts.credentials).toBe('include')
      expect(opts.headers?.Authorization).toBeUndefined()
    })
  })

  describe('Test B: 401 outside /login redirects to /#/login and calls clearSession', () => {
    it('clears session and redirects on 401 when not on login', async () => {
      // Capture href assignment
      let capturedHref = ''
      const locationMock = { href: '' }
      Object.defineProperty(locationMock, 'href', {
        get: () => capturedHref,
        set: (val) => { capturedHref = val },
      })
      vi.stubGlobal('location', locationMock)

      // Reset modules so client picks up new router/session mocks
      vi.resetModules()
      global.fetch = vi.fn()
      global.fetch.mockResolvedValue({
        ok: false,
        status: 401,
        json: () => Promise.resolve({ error: { message: 'Unauthorized' } }),
      })

      const clearSession = vi.fn()
      vi.doMock('@/composables/useSession.js', () => ({
        useSession: () => ({
          isAuthenticated: { value: false },
          currentUser: { value: null },
          loading: { value: false },
          fetchCurrentUser: vi.fn().mockResolvedValue(false),
          clearSession,
        }),
      }))
      vi.doMock('@/router/index.js', () => ({
        default: {
          currentRoute: { value: { path: '/dashboard' } },
        },
      }))

      const { api: freshApi } = await import('@/api/client.js')
      await freshApi.status()

      expect(clearSession).toHaveBeenCalled()
      expect(capturedHref).toBe('/#/login')

      vi.unstubAllGlobals()
    })
  })

  describe('webhooks()', () => {
    it('calls GET /admin/webhooks', async () => {
      const payload = { webhooks: [] }
      mockFetch(payload)
      const result = await api.webhooks()
      expect(result).toEqual(payload)
      const [url, opts] = global.fetch.mock.calls[0]
      expect(url).toBe('/admin/webhooks')
      expect(opts.method).toBeUndefined() // default GET
    })
  })

  describe('createWebhook()', () => {
    it('POSTs to /admin/webhooks with body', async () => {
      const data = { url: 'https://x.com', events: ['provider_failover'], secret: 's', enabled: true }
      mockFetch({ id: 1, ...data })
      const result = await api.createWebhook(data)
      expect(result.url).toBe('https://x.com')
      const [url, opts] = global.fetch.mock.calls[0]
      expect(url).toBe('/admin/webhooks')
      expect(opts.method).toBe('POST')
      const body = JSON.parse(opts.body)
      expect(body.url).toBe('https://x.com')
      expect(body.events).toEqual(['provider_failover'])
    })
  })

  describe('updateWebhook()', () => {
    it('PUTs to /admin/webhooks/{id} with body', async () => {
      const data = { url: 'https://y.com', events: ['pool_cooldown'], secret: 'new', enabled: false }
      mockFetch({ id: 5, ...data })
      await api.updateWebhook(5, data)
      const [url, opts] = global.fetch.mock.calls[0]
      expect(url).toBe('/admin/webhooks/5')
      expect(opts.method).toBe('PUT')
      const body = JSON.parse(opts.body)
      expect(body.url).toBe('https://y.com')
      expect(body.events).toEqual(['pool_cooldown'])
    })
  })

  describe('deleteWebhook()', () => {
    it('DELETEs /admin/webhooks/{id}', async () => {
      mockFetch(null, 204)
      // 204 returns null
      global.fetch.mockResolvedValue({ ok: true, status: 204, json: () => Promise.resolve(null) })
      await api.deleteWebhook(5)
      const [url, opts] = global.fetch.mock.calls[0]
      expect(url).toBe('/admin/webhooks/5')
      expect(opts.method).toBe('DELETE')
    })
  })

  describe('events()', () => {
    it('calls GET /admin/events with no params by default', async () => {
      mockFetch({ events: [], total: 0 })
      await api.events()
      const [url] = global.fetch.mock.calls[0]
      expect(url).toBe('/admin/events')
    })

    it('appends query params when provided', async () => {
      mockFetch({ events: [], total: 0 })
      await api.events({ limit: 50, offset: 0, event_type: 'provider_failover' })
      const [url] = global.fetch.mock.calls[0]
      // offset=0 is falsy, so it won't be included
      expect(url).toContain('/admin/events?')
      expect(url).toContain('limit=50')
      expect(url).toContain('event_type=provider_failover')
    })
  })

  describe('Test C: 401 on /login does NOT redirect (loop prevention)', () => {
    it('does not redirect when already on /login route', async () => {
      let capturedHref = ''
      const locationMock = { href: 'http://localhost/#/login' }
      Object.defineProperty(locationMock, 'href', {
        get: () => capturedHref,
        set: (val) => { capturedHref = val },
      })
      vi.stubGlobal('location', locationMock)

      vi.resetModules()
      global.fetch = vi.fn()
      global.fetch.mockResolvedValue({
        ok: false,
        status: 401,
        json: () => Promise.resolve({ error: { message: 'Unauthorized' } }),
      })

      const clearSession = vi.fn()
      vi.doMock('@/composables/useSession.js', () => ({
        useSession: () => ({
          isAuthenticated: { value: false },
          currentUser: { value: null },
          loading: { value: false },
          fetchCurrentUser: vi.fn().mockResolvedValue(false),
          clearSession,
        }),
      }))
      // Router is on /login route
      vi.doMock('@/router/index.js', () => ({
        default: {
          currentRoute: { value: { path: '/login' } },
        },
      }))

      const { api: freshApi } = await import('@/api/client.js')
      await freshApi.status()

      // clearSession should NOT be called and href should NOT be changed to /#/login
      expect(capturedHref).toBe('')

      vi.unstubAllGlobals()
    })
  })
})
