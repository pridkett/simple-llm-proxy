/**
 * API client for the LLM Proxy admin endpoints.
 * Reads the API key from localStorage via the useAuth composable.
 */

const BASE_URL = import.meta.env.VITE_API_URL || ''

function getApiKey() {
  return localStorage.getItem('proxy_api_key') || ''
}

function authHeaders() {
  const key = getApiKey()
  return key ? { Authorization: `Bearer ${key}` } : {}
}

async function request(path, options = {}) {
  const res = await fetch(`${BASE_URL}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...authHeaders(),
      ...(options.headers || {}),
    },
  })

  if (!res.ok) {
    let message = `HTTP ${res.status}`
    try {
      const body = await res.json()
      message = body?.error?.message || message
    } catch {
      // ignore parse errors
    }
    const err = new Error(message)
    err.status = res.status
    throw err
  }

  return res.json()
}

export const api = {
  /** GET /health – no auth required */
  health() {
    return fetch(`${BASE_URL}/health`).then((r) => r.json())
  },

  /** GET /admin/status */
  status() {
    return request('/admin/status')
  },

  /** GET /admin/config */
  config() {
    return request('/admin/config')
  },

  /**
   * GET /admin/logs
   * @param {{ limit?: number, offset?: number }} params
   */
  logs(params = {}) {
    const qs = new URLSearchParams()
    if (params.limit) qs.set('limit', String(params.limit))
    if (params.offset) qs.set('offset', String(params.offset))
    const query = qs.toString() ? `?${qs}` : ''
    return request(`/admin/logs${query}`)
  },

  /** GET /v1/models */
  models() {
    return request('/v1/models')
  },

  /**
   * POST /v1/chat/completions (non-streaming)
   * @param {string} model
   * @param {Array<{role:string,content:string}>} messages
   * @param {{ temperature?: number }} options
   */
  chatCompletion(model, messages, options = {}) {
    const body = { model, messages, stream: false }
    if (options.temperature !== undefined) body.temperature = options.temperature
    return request('/v1/chat/completions', { method: 'POST', body: JSON.stringify(body) })
  },

  /**
   * POST /v1/chat/completions (streaming)
   * Returns the raw Response so the caller can read the body as a stream.
   * @param {string} model
   * @param {Array<{role:string,content:string}>} messages
   * @param {{ temperature?: number, signal?: AbortSignal }} options
   */
  async chatCompletionStream(model, messages, options = {}) {
    const body = { model, messages, stream: true }
    if (options.temperature !== undefined) body.temperature = options.temperature

    const res = await fetch(`${BASE_URL}/v1/chat/completions`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        ...authHeaders(),
      },
      body: JSON.stringify(body),
      signal: options.signal,
    })

    if (!res.ok) {
      let message = `HTTP ${res.status}`
      try {
        const b = await res.json()
        message = b?.error?.message || message
      } catch { /* ignore */ }
      const err = new Error(message)
      err.status = res.status
      throw err
    }

    return res.body  // ReadableStream
  },
}
