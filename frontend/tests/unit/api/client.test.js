import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// We import api after mocking fetch
describe('api client', () => {
  let api

  beforeEach(async () => {
    localStorage.clear()
    vi.resetModules()
    // Stub global fetch
    global.fetch = vi.fn()
    const mod = await import('@/api/client.js')
    api = mod.api
  })

  afterEach(() => {
    vi.restoreAllMocks()
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

    it('includes Authorization header when key is set', async () => {
      localStorage.setItem('proxy_api_key', 'my-key')
      vi.resetModules()
      global.fetch = vi.fn()
      const mod = await import('@/api/client.js')
      api = mod.api
      mockFetch({ status: 'healthy', models: [], uptime_seconds: 0 })
      await api.status()
      const [, opts] = global.fetch.mock.calls[0]
      expect(opts.headers.Authorization).toBe('Bearer my-key')
    })

    it('throws with the server error message on failure', async () => {
      mockFetchError('Unauthorized', 401)
      await expect(api.status()).rejects.toThrow('Unauthorized')
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
        status: 401,
        json: () => Promise.resolve({ error: { message: 'Unauthorized' } }),
      })
      await expect(api.chatCompletionStream('gpt-4', [])).rejects.toThrow('Unauthorized')
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
      mockFetchError('Unauthorized', 401)
      await expect(api.costMapStatus()).rejects.toThrow('Unauthorized')
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
})
