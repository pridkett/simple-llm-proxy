/**
 * API client for the LLM Proxy admin endpoints.
 *
 * Auth model: session cookie (HttpOnly, browser-managed).
 * The frontend NEVER reads the session token — it uses credentials:'include' on
 * every fetch so the browser attaches the cookie automatically.
 * No Authorization header. No localStorage reads for auth.
 */

import { useSession } from '../composables/useSession.js'
import router from '../router/index.js'

const BASE_URL = import.meta.env.VITE_API_URL || ''

async function request(path, options = {}) {
  const res = await fetch(`${BASE_URL}${path}`, {
    ...options,
    credentials: 'include', // browser attaches HttpOnly session cookie automatically
    headers: {
      'Content-Type': 'application/json',
      ...(options.headers || {}),
      // NO Authorization header — session is cookie-based
    },
  })

  if (res.status === 401) {
    // 401 loop prevention: only redirect if we're not already on the login page
    if (router.currentRoute.value.path !== '/login') {
      const { clearSession } = useSession()
      clearSession()
      window.location.href = '/#/login'
    }
    return null // return null (not throw) so callers can detect unauthenticated state
  }

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

  // 204 No Content — return null (no body to parse)
  if (res.status === 204) return null

  return res.json()
}

export const api = {
  /** GET /health – no auth required */
  health() {
    return fetch(`${BASE_URL}/health`).then((r) => r.json())
  },

  /** GET /admin/me – returns current authenticated user */
  me() {
    return request('/admin/me')
  },

  /** POST /auth/logout – destroys session */
  logout() {
    return request('/auth/logout', { method: 'POST' })
  },

  /** GET /admin/status */
  status() {
    return request('/admin/status')
  },

  /** GET /admin/config */
  config() {
    return request('/admin/config')
  },

  /** POST /admin/reload – re-reads config file and updates the router */
  reload() {
    return request('/admin/reload', { method: 'POST' })
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
   * Returns the raw Response body so the caller can read it as a stream.
   * @param {string} model
   * @param {Array<{role:string,content:string}>} messages
   * @param {{ temperature?: number, signal?: AbortSignal }} options
   */
  async chatCompletionStream(model, messages, options = {}) {
    const body = { model, messages, stream: true }
    if (options.temperature !== undefined) body.temperature = options.temperature

    const res = await fetch(`${BASE_URL}/v1/chat/completions`, {
      method: 'POST',
      credentials: 'include', // HttpOnly session cookie
      headers: {
        'Content-Type': 'application/json',
        // NO Authorization header — session is cookie-based
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

  /** GET /admin/costmap — returns cost map status */
  costMapStatus() {
    return request('/admin/costmap')
  },

  /** POST /admin/costmap/reload — triggers a fresh download of the cost map */
  costMapReload() {
    return request('/admin/costmap/reload', { method: 'POST' })
  },

  /**
   * PUT /admin/costmap/url — updates the source URL for future reloads
   * @param {string} url
   */
  costMapSetURL(url) {
    return request('/admin/costmap/url', {
      method: 'PUT',
      body: JSON.stringify({ url }),
    })
  },

  /**
   * GET /admin/costmap/models — returns all cost map model entries sorted by name.
   * Each entry has name, input_cost_per_token, output_cost_per_token, max_tokens, etc.
   */
  costMapModels() {
    return request('/admin/costmap/models')
  },

  /**
   * GET /v1/models/{model} — returns model detail with cost information
   * @param {string} modelName
   */
  modelDetail(modelName) {
    return request(`/v1/models/${encodeURIComponent(modelName)}`)
  },

  /**
   * PATCH /v1/models/{model}/cost_map_key — set a cost map key override
   * @param {string} modelName
   * @param {string} costMapKey  e.g. "openai/gpt-4"
   */
  patchModelCostMapKey(modelName, costMapKey) {
    return request(`/v1/models/${encodeURIComponent(modelName)}/cost_map_key`, {
      method: 'PATCH',
      body: JSON.stringify({ cost_map_key: costMapKey }),
    })
  },

  /**
   * PATCH /v1/models/{model}/costs — set a fully custom cost spec
   * @param {string} modelName
   * @param {object} costs  Fields matching costmap.ModelSpec
   */
  patchModelCosts(modelName, costs) {
    return request(`/v1/models/${encodeURIComponent(modelName)}/costs`, {
      method: 'PATCH',
      body: JSON.stringify(costs),
    })
  },

  /**
   * DELETE /v1/models/{model}/costs — clear any cost override, reverting to auto-detection
   * @param {string} modelName
   */
  deleteModelCosts(modelName) {
    return request(`/v1/models/${encodeURIComponent(modelName)}/costs`, {
      method: 'DELETE',
    })
  },

  /** GET /admin/users — returns all authenticated users (admin only) */
  users() {
    return request('/admin/users')
  },

  /** GET /admin/teams — returns all teams */
  teams() {
    return request('/admin/teams')
  },

  /** POST /admin/teams — create a team { name } */
  createTeam(data) {
    return request('/admin/teams', { method: 'POST', body: JSON.stringify(data) })
  },

  /** DELETE /admin/teams/:id */
  deleteTeam(id) {
    return request(`/admin/teams/${id}`, { method: 'DELETE' })
  },

  /** GET /admin/teams/mine — teams the current user belongs to */
  myTeams() {
    return request('/admin/teams/mine')
  },

  /** GET /admin/teams/:id/members */
  teamMembers(teamId) {
    return request(`/admin/teams/${teamId}/members`)
  },

  /** PUT /admin/teams/:id/members — add member { user_id, role } */
  addTeamMember(teamId, data) {
    return request(`/admin/teams/${teamId}/members`, { method: 'PUT', body: JSON.stringify(data) })
  },

  /** DELETE /admin/teams/:id/members/:userId */
  removeTeamMember(teamId, userId) {
    return request(`/admin/teams/${teamId}/members/${userId}`, { method: 'DELETE' })
  },

  /** PATCH /admin/teams/:id/members/:userId — update role { role } */
  updateTeamMemberRole(teamId, userId, data) {
    return request(`/admin/teams/${teamId}/members/${userId}`, { method: 'PATCH', body: JSON.stringify(data) })
  },

  /** GET /admin/applications?team_id=N */
  applications(teamId) {
    const qs = teamId ? `?team_id=${teamId}` : ''
    return request(`/admin/applications${qs}`)
  },

  /** POST /admin/applications — create { team_id, name } */
  createApplication(data) {
    return request('/admin/applications', { method: 'POST', body: JSON.stringify(data) })
  },

  /** DELETE /admin/applications/:id */
  deleteApplication(id) {
    return request(`/admin/applications/${id}`, { method: 'DELETE' })
  },
}
