import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import CostView from '@/views/CostView.vue'
import { addMetrics } from '@/utils/usage.js'

vi.mock('@/api/client.js', () => ({
  api: { usage: vi.fn(), logsMeta: vi.fn() }
}))
import { api } from '@/api/client.js'

const groups = [
  {
    group_id: 'local',
    name: 'local',
    calls: 10,
    total_tokens: 1200,
    known_token_calls: 10,
    total_spend: 0,
    known_cost_calls: 10,
    input_tokens: 1000,
    known_input_calls: 10,
    output_tokens: 200,
    known_output_calls: 10
  },
  {
    group_id: 'paid',
    name: 'paid',
    calls: 2,
    total_tokens: 80,
    known_token_calls: 2,
    total_spend: 1.25,
    known_cost_calls: 2,
    input_tokens: 50,
    output_tokens: 30,
    known_input_calls: 2,
    known_output_calls: 2
  },
  {
    group_id: 'opaque',
    name: 'opaque',
    calls: 1,
    total_tokens: 0,
    total_spend: 0,
    known_token_calls: 0,
    known_cost_calls: 0
  },
  {
    group_id: 'partial',
    name: 'partial',
    calls: 1,
    total_spend: 0.01,
    known_cost_calls: 1,
    partial_cost_calls: 1,
    input_tokens: 7,
    known_input_calls: 1
  }
]
function response(rows = groups) {
  return {
    groups: rows,
    series: rows.map((r) => ({ ...r, bucket: 0 })),
    summary: {
      failed_calls: 0,
      upstream_cost_calls: 2,
      estimated_cost_calls: 10,
      mixed_cost_calls: 1,
      ...addMetrics(rows)
    },
    buckets: [
      {
        start: '2026-11-01T04:00:00Z',
        end: '2026-11-02T05:00:00Z',
        label: 'Nov 1'
      }
    ],
    from: '2026-11-01',
    to: '2026-11-01',
    from_time: '2026-11-01T04:00:00Z',
    to_time: '2026-11-02T05:00:00Z',
    timezone: 'America/New_York',
    retention_days: 30,
    alerts: [],
    budget_rows: [
      {
        key_id: 1,
        key_name: 'test-key',
        app_name: 'app',
        team_name: 'team',
        total_spend: 1.26,
        max_budget: 10,
        soft_budget: 8
      }
    ]
  }
}
const chart = {
  name: 'ChartProbe',
  props: ['options', 'series'],
  template: '<div />'
}
let wrapper, router
async function render(data = response(), path = '/cost') {
  api.usage.mockResolvedValue(data)
  router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/cost', component: CostView },
      { path: '/logs', component: { template: '<div />' } }
    ]
  })
  await router.push(path)
  wrapper = mount(CostView, {
    global: { plugins: [router], stubs: { apexchart: chart } }
  })
  await flushPromises()
  return wrapper
}
const button = (text) =>
  wrapper.findAll('button').find((b) => b.text() === text)
async function settle() {
  await vi.advanceTimersByTimeAsync(160)
  await flushPromises()
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  api.logsMeta.mockResolvedValue({
    teams: [{ id: 1, name: 'team' }],
    apps: [{ id: 2, name: 'app' }],
    keys: [{ id: 3, name: 'key' }],
    models: ['local', 'paid']
  })
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('Usage & Cost', () => {
  it('applies linked key filters and budget dates before the first request', async () => {
    localStorage.setItem('usage-chart-metric', 'tokens')
    await render(response(), '/cost?team_id=1&app_id=2&key_id=3&metric=spend&from=2026-10-28&to=2026-11-04&timezone=UTC')
    expect(api.usage).toHaveBeenCalledTimes(1)
    expect(api.usage).toHaveBeenCalledWith(expect.objectContaining({
      team_id: 1, app_id: 2, key_id: 3,
      from: '2026-10-28', to: '2026-11-04', timezone: 'UTC'
    }))
    expect(wrapper.get('[aria-label="Team"]').element.value).toBe('1')
    expect(wrapper.get('[aria-label="Application"]').element.value).toBe('2')
    expect(wrapper.get('[aria-label="Key"]').element.value).toBe('3')
    expect(wrapper.get('[aria-label="From date"]').element.value).toBe('2026-10-28')
    expect(wrapper.get('[aria-label="To date"]').element.value).toBe('2026-11-04')
    expect(wrapper.get('[data-testid="summary-spend"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('[aria-label="Sort by"]').element.value).toBe('total_spend')
  })

  it('updates filters on navigation and restores them with browser back', async () => {
    await render(response(), '/cost?team_id=1&app_id=2&key_id=3&metric=spend')
    await router.push('/cost?team_id=1&metric=spend')
    await settle()
    expect(api.usage).toHaveBeenLastCalledWith(expect.objectContaining({ team_id: 1, app_id: null, key_id: null }))
    router.back()
    await flushPromises()
    await settle()
    expect(api.usage).toHaveBeenLastCalledWith(expect.objectContaining({ team_id: 1, app_id: 2, key_id: 3 }))
    await button('Clear filters').trigger('click')
    await settle()
    expect(api.usage).toHaveBeenLastCalledWith(expect.objectContaining({ team_id: null, app_id: null, key_id: null }))
  })

  it('keeps linked entities selected even when they have no retained logs', async () => {
    api.logsMeta.mockResolvedValue({ teams: [], apps: [], keys: [], models: [] })
    await render(response([]), '/cost?team_id=1&app_id=2&key_id=3&metric=spend')
    for (const [label, id] of [['Team', '1'], ['Application', '2'], ['Key', '3']]) {
      expect(wrapper.get(`[aria-label="${label}"]`).element.value).toBe(id)
    }
    expect(wrapper.text()).toContain('No requests were recorded')
    expect(api.usage).toHaveBeenCalledWith(expect.objectContaining({ team_id: 1, app_id: 2, key_id: 3 }))
  })

  it.each(['0', '-1', 'abc', '1.5', '9007199254740993', '1&team_id=2'])(
    'ignores invalid or repeated filter IDs: %s', async (id) => {
      await render(response(), `/cost?team_id=${id}&app_id=bad&key_id=-3&metric=bad&timezone=bad`)
      expect(api.usage).toHaveBeenCalledWith(expect.objectContaining({ team_id: null, app_id: null, key_id: null }))
      expect(wrapper.get('[data-testid="summary-calls"]').attributes('aria-pressed')).toBe('true')
    }
  )

  it('keeps dollars visible when calls or tokens drive the chart', async () => {
    await render()
    expect(wrapper.find('[data-testid="summary-spend"]').text()).toContain(
      '$1.2600'
    )
    await wrapper.find('[data-testid="summary-tokens"]').trigger('click')
    expect(wrapper.find('[data-testid="summary-spend"]').text()).toContain(
      '$1.2600'
    )
    expect(wrapper.findAll('th').map((h) => h.text())).toContain('Dollar spend')
    expect(wrapper.findComponent(chart).props('series')[0].data[0]).toBe(1200)
    expect(localStorage.getItem('usage-chart-metric')).toBe('tokens')
  })
  it('keeps distinct category colors when a metric changes their order', async () => {
    await render()
    const initial = wrapper.findComponent(chart).props()
    const colors = new Map(initial.series.map((series, i) => [series.name, initial.options.colors[i]]))
    expect(new Set(colors.values()).size).toBe(groups.length)
    await wrapper.find('[data-testid="summary-spend"]').trigger('click')
    const updated = wrapper.findComponent(chart).props()
    updated.series.forEach((series, i) => expect(updated.options.colors[i]).toBe(colors.get(series.name)))
  })
  it('restores the chart metric preference', async () => {
    localStorage.setItem('usage-chart-metric', 'spend')
    await render()
    expect(
      wrapper.find('[data-testid="summary-spend"]').attributes('aria-pressed')
    ).toBe('true')
    expect(wrapper.findAll('[data-testid="usage-row"]')[0].text()).toContain(
      'paid'
    )
  })
  it('distinguishes measured zero, missing measurements, and partial spend', async () => {
    await render()
    const rows = wrapper.findAll('[data-testid="usage-row"]')
    expect(rows.find((r) => r.text().includes('local')).text()).toContain(
      '$0.0000'
    )
    expect(rows.find((r) => r.text().includes('opaque')).text()).toContain(
      'Unknown'
    )
    expect(rows.find((r) => r.text().includes('partial')).text()).toContain(
      'Known subtotal'
    )
    expect(wrapper.find('[data-testid="summary-spend"]').text()).toContain(
      '13 / 14 calls measured'
    )
  })
  it('sorts every metric and keeps unknown values last in both directions', async () => {
    await render()
    await wrapper.find('[aria-label="Sort by"]').setValue('total_spend')
    expect(wrapper.findAll('[data-testid="usage-row"]')[0].text()).toContain(
      'paid'
    )
    expect(
      wrapper.findAll('[data-testid="usage-row"]').at(-1).text()
    ).toContain('opaque')
    await wrapper.find('[aria-label="Sort ascending"]').trigger('click')
    expect(wrapper.findAll('[data-testid="usage-row"]')[0].text()).toContain(
      'local'
    )
    expect(
      wrapper.findAll('[data-testid="usage-row"]').at(-1).text()
    ).toContain('opaque')
  })
  it('exposes token detail and dollar totals together in Explore', async () => {
    await render()
    await button('explore').trigger('click')
    const headers = wrapper.findAll('th').map((h) => h.text())
    expect(headers).toEqual(
      expect.arrayContaining([
        'Input',
        'Output',
        'Cache read',
        'Cache write',
        'Reasoning',
        'Dollar spend'
      ])
    )
    expect(wrapper.find('[data-testid="usage-totals"]').text()).toContain(
      '$1.2600'
    )
    expect(wrapper.find('[data-testid="estimate-note"]').text()).toContain(
      'subsets, not extra tokens'
    )
  })
  it('refetches grouping, timezone and attribution filters on the server', async () => {
    await render()
    await wrapper.find('[aria-label="Group by"]').setValue('app')
    await wrapper.find('[aria-label="Timezone"]').setValue('UTC')
    await wrapper.find('[aria-label="Team"]').setValue(1)
    await settle()
    expect(api.usage).toHaveBeenLastCalledWith(
      expect.objectContaining({ group_by: 'app', timezone: 'UTC', team_id: 1 })
    )
  })
  it('drills into a model and preserves exact report bounds in Logs links', async () => {
    await render()
    const link = wrapper.find('a[aria-label="View requests for local"]')
    const parsed = router.resolve(link.attributes('href').replace(/^#/, ''))
    expect(parsed.query).toMatchObject({
      model: 'local',
      date_from: '2026-11-01T04:00:00Z',
      date_before: '2026-11-02T05:00:00Z'
    })
    await button('local').trigger('click')
    await settle()
    expect(api.usage).toHaveBeenLastCalledWith(
      expect.objectContaining({ model: 'local' })
    )
    expect(wrapper.findAll('th').map((h) => h.text())).toContain('Reasoning')
  })
  it('offers a keyboard-accessible time-bucket drill-down with exclusive bounds', async () => {
    await render()
    await wrapper.find('[aria-label="Explore time bucket"]').setValue(0)
    await settle()
    expect(api.usage).toHaveBeenLastCalledWith(
      expect.objectContaining({
        since: '2026-11-01T04:00:00Z',
        until: '2026-11-02T05:00:00Z'
      })
    )
    expect(wrapper.text()).toContain('Nov 1 ·')
    await button('Today').trigger('click')
    await settle()
    expect(api.usage.mock.lastCall[0].since).toBeUndefined()
  })
  it('preserves key budgets and alerts with exact configured dollar caps', async () => {
    const data = response()
    data.alerts = [
      {
        key_id: 1,
        key_name: 'test-key',
        app_name: 'app',
        total_spend: 9.5,
        alert_type: 'soft'
      }
    ]
    await render(data)
    expect(wrapper.text()).toContain('soft budget reached')
    await button('budgets').trigger('click')
    expect(wrapper.text()).toContain('$10.00')
    expect(wrapper.text()).toContain('12.6%')
    expect(wrapper.find('[data-testid="summary-spend"]').exists()).toBe(true)
  })
  it('shows empty and error states and allows retry', async () => {
    await render(response([]))
    expect(wrapper.text()).toContain('No requests were recorded')
    api.usage.mockRejectedValueOnce(new Error('Unavailable'))
    await button('Refresh').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Unavailable')
    await button('Refresh').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('No requests were recorded')
  })
  it('rejects reversed custom dates without requesting a misleading report', async () => {
    await render()
    await button('Custom').trigger('click')
    await wrapper.find('[aria-label="From date"]').setValue('2026-12-01')
    await wrapper.find('[aria-label="To date"]').setValue('2026-11-01')
    const count = api.usage.mock.calls.length
    await settle()
    expect(wrapper.find('[role="alert"]').text()).toContain('start date')
    expect(api.usage.mock.calls.length).toBe(count)
  })
  it('does not replace a newer report with a stale response', async () => {
    await render()
    let release
    api.usage.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          release = resolve
        })
    )
    await button('Today').trigger('click')
    await settle()
    api.usage.mockResolvedValueOnce(response([groups[0]]))
    await button('30d').trigger('click')
    await settle()
    release(response([groups[1]]))
    await flushPromises()
    expect(wrapper.find('[data-testid="summary-spend"]').text()).toContain(
      '$0.0000'
    )
  })
  it('updates chart theme and tooltip with system appearance', async () => {
    const media = new EventTarget()
    media.matches = true
    vi.stubGlobal(
      'matchMedia',
      vi.fn(() => media)
    )
    await render()
    expect(wrapper.findComponent(chart).props('options').theme.mode).toBe(
      'dark'
    )
    media.matches = false
    media.dispatchEvent(new Event('change'))
    await flushPromises()
    expect(wrapper.findComponent(chart).props('options').tooltip.theme).toBe(
      'light'
    )
  })
})
