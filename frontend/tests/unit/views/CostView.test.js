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

const spendWithModels = {
  rows: [
    { key_id: 1, key_name: 'test-key', app_id: 1, app_name: 'test-app', team_id: 1, team_name: 'test-team',
      total_spend: 1.5, max_budget: 10.0, soft_budget: null }
  ],
  model_rows: [
    { model: 'claude-sonnet', total_spend: 1.25, request_count: 1200,
      input_tokens: 150000, output_tokens: 40000, cache_read_tokens: 2000000, cache_write_tokens: 50000 },
    { model: 'gpt-4o', total_spend: 0.25, request_count: 300,
      input_tokens: 90000, output_tokens: 10000, cache_read_tokens: 0, cache_write_tokens: 0 },
  ],
  daily_rows: [],
  alerts: [],
  from: '2026-03-19', to: '2026-03-26',
}

async function mountByModel(data) {
  api.spend.mockResolvedValue(data)
  const wrapper = mount(CostView, { global: { plugins: [makeRouter()], stubs: { apexchart: true } } })
  await flushPromises()
  await wrapper.findAll('button').find(b => b.text() === 'By Model').trigger('click')
  return wrapper
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

  describe('By Model breakdown (ADR 012)', () => {
    it('renders token columns with grouped counts for each model', async () => {
      const wrapper = await mountByModel(spendWithModels)
      const headers = wrapper.findAll('th').map(th => th.text())
      expect(headers).toEqual(expect.arrayContaining(
        ['Model', 'Requests', 'Input Tokens', 'Output Tokens', 'Cache Read', 'Cache Write', 'Est. Spend']))
      const rows = wrapper.findAll('[data-testid="model-row"]')
      expect(rows).toHaveLength(2)
      const cells = rows[0].findAll('td').map(td => td.text())
      expect(cells).toEqual([
        'claude-sonnet',
        (1200).toLocaleString(),
        (150000).toLocaleString(),
        (40000).toLocaleString(),
        (2000000).toLocaleString(),
        (50000).toLocaleString(),
        '~$1.2500',
      ])
    })

    it('renders a totals row summing every column', async () => {
      const wrapper = await mountByModel(spendWithModels)
      const cells = wrapper.find('[data-testid="model-totals"]').findAll('td').map(td => td.text())
      expect(cells).toEqual([
        'Total',
        (1500).toLocaleString(),
        (240000).toLocaleString(),
        (50000).toLocaleString(),
        (2000000).toLocaleString(),
        (50000).toLocaleString(),
        '~$1.5000',
      ])
    })

    it('treats missing token fields as zero', async () => {
      const wrapper = await mountByModel({
        ...spendWithModels,
        model_rows: [{ model: 'legacy', total_spend: 0.1, request_count: 2 }],
      })
      const cells = wrapper.find('[data-testid="model-row"]').findAll('td').map(td => td.text())
      expect(cells.slice(2, 6)).toEqual(['0', '0', '0', '0'])
    })

    it('omits the totals row when there is no model data', async () => {
      const wrapper = await mountByModel({ ...spendWithModels, model_rows: [] })
      expect(wrapper.text()).toContain('No model data')
      expect(wrapper.find('[data-testid="model-totals"]').exists()).toBe(false)
    })
  })

  describe('estimated cost labeling (ADR 012 D-04)', () => {
    it('prefixes spend with ~ but leaves budget caps exact', async () => {
      api.spend.mockResolvedValue(spendWithModels)
      const wrapper = mount(CostView, { global: { plugins: [makeRouter()], stubs: { apexchart: true } } })
      await flushPromises()
      const text = wrapper.text()
      expect(text).toContain('Est. Spend')
      expect(text).toContain('~$1.5000')
      expect(text).toContain('$10.00')
      expect(text).not.toContain('~$10.00')
    })

    it('shows the estimate explanation note', async () => {
      api.spend.mockResolvedValue(spendWithModels)
      const wrapper = mount(CostView, { global: { plugins: [makeRouter()], stubs: { apexchart: true } } })
      await flushPromises()
      expect(wrapper.find('[data-testid="estimate-note"]').text()).toContain('Costs are estimates')
    })
  })
})
