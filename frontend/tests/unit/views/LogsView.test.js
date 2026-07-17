import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createWebHashHistory } from 'vue-router'
import { nextTick } from 'vue'
import LogsView from '@/views/LogsView.vue'

vi.mock('@/api/client.js', () => ({
  api: {
    logs: vi.fn(),
    logsMeta: vi.fn(),
    logDetail: vi.fn(),
  },
}))

import { api } from '@/api/client.js'

const mockMetaResponse = {
  teams: [{ id: 1, name: 'test-team' }],
  apps: [{ id: 1, name: 'test-app' }],
  keys: [{ id: 1, name: 'test-key' }],
  providers: ['openai', 'anthropic'],
  models: ['gpt-4', 'claude-3'],
  pools: ['pool-a'],
}

const mockLogsData = {
  logs: [
    { request_id: 'req-abc123', model: 'gpt-4', provider: 'openai',
      endpoint: '/v1/chat/completions', prompt_tokens: 100, completion_tokens: 50,
      total_tokens: 150, total_cost: 0.003, status_code: 200, latency_ms: 1234,
      ttft_ms: 456, request_time: '2026-06-01T10:00:00Z', is_streaming: true,
      api_key_id: 1, key_name: 'test-key', app_name: 'test-app', team_name: 'test-team' },
    { request_id: 'req-def456', model: 'claude-3', provider: 'anthropic',
      endpoint: '/v1/chat/completions', prompt_tokens: 200, completion_tokens: 80,
      total_tokens: 280, total_cost: 0.005, status_code: 200, latency_ms: 2000,
      ttft_ms: null, request_time: '2026-06-01T11:00:00Z', is_streaming: false,
      api_key_id: null, key_name: '', app_name: '', team_name: '' },
  ],
  total: 2, limit: 50, offset: 0,
}

const mockDetailResponse = {
  request_id: 'req-abc123', model: 'gpt-4', provider: 'openai', pool_name: 'pool-a',
  ttft_ms: 456, req_body_snippet: '{"model":"gpt-4","messages":[...]}',
  resp_body_snippet: '{"choices":[{"message":{"content":"Hello"}}]}',
  status_code: 200, latency_ms: 1234, prompt_tokens: 100, completion_tokens: 50,
  total_cost: 0.003, cache_read_tokens: 0, cache_write_tokens: 0,
  key_name: 'test-key', app_name: 'test-app', team_name: 'test-team',
}

function makeRouter() {
  return createRouter({
    history: createWebHashHistory(),
    routes: [{ path: '/logs', component: LogsView }],
  })
}

function setupMocks() {
  api.logsMeta.mockResolvedValue(mockMetaResponse)
  api.logs.mockResolvedValue(mockLogsData)
}

describe('LogsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('filter change triggers server fetch with filter params (UI-01)', async () => {
    setupMocks()
    api.logs.mockResolvedValue(mockLogsData)
    const wrapper = mount(LogsView, { global: { plugins: [makeRouter()] } })
    await flushPromises()
    const initialCallCount = api.logs.mock.calls.length
    expect(initialCallCount).toBeGreaterThan(0)
    // Click a filter button — e.g., Today
    const buttons = wrapper.findAll('button')
    const btnToday = buttons.find(b => b.text() === 'Today')
    expect(btnToday).toBeDefined()
    await btnToday.trigger('click')
    await flushPromises()
    expect(api.logs.mock.calls.length).toBeGreaterThan(initialCallCount)
    const lastCall = api.logs.mock.calls[api.logs.mock.calls.length - 1][0]
    expect(lastCall.date_from).toBeDefined()
  })

  it('calls api.logsMeta on mount to populate dropdowns (UI-02)', async () => {
    setupMocks()
    const wrapper = mount(LogsView, { global: { plugins: [makeRouter()] } })
    await flushPromises()
    expect(api.logsMeta).toHaveBeenCalledTimes(1)
    // Provider dropdown contains options from meta
    const html = wrapper.html()
    expect(html).toContain('openai')
    expect(html).toContain('anthropic')
  })

  it('filter bar has Today/7d/30d/Custom date preset buttons and Provider/Pool/Key controls (UI-03)', async () => {
    setupMocks()
    const wrapper = mount(LogsView, { global: { plugins: [makeRouter()] } })
    await flushPromises()
    const text = wrapper.text()
    expect(text).toContain('Today')
    expect(text).toContain('7d')
    expect(text).toContain('30d')
    expect(text).toContain('Custom')
    expect(text).toContain('Provider')
    expect(text).toContain('Pool')
    expect(text).toContain('Key')
  })

  it('TTFT column shows Xms for non-null ttft_ms and — for null (UI-04)', async () => {
    setupMocks()
    const wrapper = mount(LogsView, { global: { plugins: [makeRouter()] } })
    await flushPromises()
    const text = wrapper.text()
    expect(text).toContain('TTFT')
    expect(text).toContain('456ms')
    expect(text).toContain('—')
  })

  it('clicking a row calls api.logDetail and renders expanded detail sub-row (UI-05)', async () => {
    setupMocks()
    api.logDetail.mockResolvedValue(mockDetailResponse)
    const wrapper = mount(LogsView, { global: { plugins: [makeRouter()] } })
    await flushPromises()
    expect(api.logDetail).not.toHaveBeenCalled()
    const rows = wrapper.findAll('tbody tr')
    expect(rows.length).toBeGreaterThan(0)
    await rows[0].trigger('click')
    await flushPromises()
    expect(api.logDetail).toHaveBeenCalledWith('req-abc123')
    expect(wrapper.text()).toContain('pool-a')
  })

  it('clicking an expanded row again collapses it and does not refetch (UI-05)', async () => {
    setupMocks()
    api.logDetail.mockResolvedValue(mockDetailResponse)
    const wrapper = mount(LogsView, { global: { plugins: [makeRouter()] } })
    await flushPromises()
    const rows = wrapper.findAll('tbody tr')
    await rows[0].trigger('click')
    await flushPromises()
    const callCountAfterExpand = api.logDetail.mock.calls.length
    // Expansion must have actually fetched the detail (guards against vacuous pass)
    expect(callCountAfterExpand).toBe(1)
    // Collapse
    await rows[0].trigger('click')
    await nextTick()
    // No additional logDetail calls
    expect(api.logDetail.mock.calls.length).toBe(callCountAfterExpand)
  })
})
