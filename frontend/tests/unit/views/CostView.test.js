import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createWebHashHistory } from 'vue-router'
import CostView from '@/views/CostView.vue'

vi.mock('@/api/client.js', () => ({
  api: {
    spend: vi.fn(),
  },
}))

import { api } from '@/api/client.js'

function makeRouter() {
  return createRouter({
    history: createWebHashHistory(),
    routes: [{ path: '/cost', component: CostView }],
  })
}

const emptySpendResponse = { rows: [], alerts: [], from: '2026-03-19', to: '2026-03-26' }
const spendWithAlerts = {
  rows: [
    { key_id: 1, key_name: 'test-key', app_id: 1, app_name: 'test-app', team_id: 1, team_name: 'test-team',
      total_spend: 9.5, max_budget: 10.0, soft_budget: 8.0 }
  ],
  alerts: [
    { key_id: 1, key_name: 'test-key', app_name: 'test-app', team_name: 'test-team',
      total_spend: 9.5, soft_budget: 8.0, max_budget: 10.0, alert_type: 'soft' }
  ],
  from: '2026-03-19', to: '2026-03-26',
}

describe('CostView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders ErrorAlert on API failure', async () => {
    api.spend.mockRejectedValue(new Error('Network error'))
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()
    expect(wrapper.findComponent({ name: 'ErrorAlert' }).exists()).toBe(true)
  })

  it('hides Alerts Panel when alerts array is empty', async () => {
    api.spend.mockResolvedValue(emptySpendResponse)
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()
    expect(wrapper.text()).not.toContain('Budget Alerts')
  })

  it('renders Alerts Panel when alerts array is non-empty', async () => {
    api.spend.mockResolvedValue(spendWithAlerts)
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('Budget Alerts')
  })

  it('renders empty state when spend rows array is empty', async () => {
    api.spend.mockResolvedValue(emptySpendResponse)
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('No spend data')
  })

  it('renders breakdown table rows from spend data', async () => {
    api.spend.mockResolvedValue(spendWithAlerts)
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('test-key')
  })

  it('filter bar defaults to 7d as the active date range', async () => {
    api.spend.mockResolvedValue(emptySpendResponse)
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()
    const buttons = wrapper.findAll('button')
    const btn7d = buttons.find(b => b.text() === '7d')
    expect(btn7d).toBeTruthy()
    expect(btn7d.classes()).toContain('bg-indigo-50')
  })

  it('Reset Filters button not shown when filters are at defaults', async () => {
    api.spend.mockResolvedValue(emptySpendResponse)
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()
    expect(wrapper.text()).not.toContain('Reset Filters')
  })

  it('re-fetches from server when date range filter changes (no client-side filtering)', async () => {
    // Mount with initial 7d response
    api.spend.mockResolvedValue(emptySpendResponse)
    const router = makeRouter()
    const wrapper = mount(CostView, { global: { plugins: [router], stubs: { apexchart: true } } })
    await flushPromises()

    const initialCallCount = api.spend.mock.calls.length
    expect(initialCallCount).toBeGreaterThan(0)

    // Click Today button — should trigger a new API call
    api.spend.mockResolvedValue(emptySpendResponse)
    const buttons = wrapper.findAll('button')
    const btnToday = buttons.find(b => b.text() === 'Today')
    await btnToday.trigger('click')
    await flushPromises()

    // api.spend should have been called again (server-driven refetch, not client-side filter)
    expect(api.spend.mock.calls.length).toBeGreaterThan(initialCallCount)
  })
})
