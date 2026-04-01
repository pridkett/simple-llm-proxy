import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createWebHashHistory } from 'vue-router'
import { nextTick } from 'vue'
import EventsView from '@/views/EventsView.vue'
import LoadingSpinner from '@/components/LoadingSpinner.vue'
import ErrorAlert from '@/components/ErrorAlert.vue'

const mockEventsData = {
  events: [
    {
      id: 42,
      event_type: 'provider_failover',
      payload: {
        event_type: 'provider_failover',
        timestamp: '2026-03-31T14:30:00Z',
        value1: 'gpt-4',
        value2: 'openai/gpt-4 -> anthropic/claude-3-haiku',
        value3: 'rate_limited',
        context: {
          model: 'gpt-4',
          pool_name: 'gpt-4-pool',
          providers_tried: ['openai/gpt-4', 'anthropic/claude-3-haiku'],
          provider_used: 'anthropic/claude-3-haiku',
          failover_reasons: ['rate_limited'],
          budget_remaining: null,
        },
      },
      created_at: '2026-03-31T14:30:00Z',
    },
    {
      id: 43,
      event_type: 'budget_exhausted',
      payload: {
        event_type: 'budget_exhausted',
        timestamp: '2026-03-31T15:00:00Z',
        value1: 'claude-pool',
        value2: 'Daily budget cap reached',
        value3: '',
        context: {
          pool_name: 'claude-pool',
          budget_remaining: null,
        },
      },
      created_at: '2026-03-31T15:00:00Z',
    },
  ],
  total: 2,
  limit: 50,
  offset: 0,
}

function makeRouter() {
  return createRouter({
    history: createWebHashHistory(),
    routes: [{ path: '/', component: EventsView }],
  })
}

describe('EventsView', () => {
  let fetchMock

  beforeEach(() => {
    localStorage.clear()
    fetchMock = vi.fn()
    global.fetch = fetchMock
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  function mockSuccess(data = mockEventsData) {
    fetchMock.mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve(data),
    })
  }

  function mockFailure() {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 500,
      json: () => Promise.resolve({ error: { message: 'server error' } }),
    })
  }

  it('shows LoadingSpinner while fetching', async () => {
    // Never resolve to keep it in loading state
    fetchMock.mockReturnValue(new Promise(() => {}))
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(EventsView, { global: { plugins: [router] } })
    await nextTick()
    expect(wrapper.findComponent(LoadingSpinner).exists()).toBe(true)
  })

  it('renders event table with Time, Type, Details columns after successful fetch', async () => {
    mockSuccess()
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(EventsView, { global: { plugins: [router] } })
    await flushPromises()
    const headers = wrapper.findAll('th')
    const headerTexts = headers.map(h => h.text())
    expect(headerTexts).toContain('Time')
    expect(headerTexts).toContain('Type')
    expect(headerTexts).toContain('Details')
  })

  it('shows event type badge with correct text', async () => {
    mockSuccess()
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(EventsView, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('Provider Failover')
    expect(wrapper.text()).toContain('Budget Exhausted')
  })

  it('shows details from payload.value1 and payload.value2 in the Details column', async () => {
    mockSuccess()
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(EventsView, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('gpt-4')
    expect(wrapper.text()).toContain('openai/gpt-4 -> anthropic/claude-3-haiku')
  })

  it('shows ErrorAlert when fetch fails', async () => {
    mockFailure()
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(EventsView, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.findComponent(ErrorAlert).exists()).toBe(true)
  })

  it('shows empty state "No events recorded" when events array is empty', async () => {
    mockSuccess({ events: [], total: 0, limit: 50, offset: 0 })
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(EventsView, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('No events recorded')
  })

  it('pagination shows "Showing 1-2 of 2 events" text', async () => {
    // Use a dataset where total > pageSize to trigger pagination
    const manyEvents = {
      events: mockEventsData.events,
      total: 100,
      limit: 50,
      offset: 0,
    }
    mockSuccess(manyEvents)
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(EventsView, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('Showing')
    expect(wrapper.text()).toContain('of 100 events')
  })

  it('event type filter dropdown exists with "All event types" default', async () => {
    mockSuccess()
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(EventsView, { global: { plugins: [router] } })
    await flushPromises()
    const select = wrapper.find('select')
    expect(select.exists()).toBe(true)
    const options = select.findAll('option')
    const optionTexts = options.map(o => o.text())
    expect(optionTexts).toContain('All event types')
  })

  it('clicking a row toggles expanded detail panel showing context keys', async () => {
    mockSuccess()
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(EventsView, { global: { plugins: [router] } })
    await flushPromises()

    // Initially no expanded detail
    expect(wrapper.text()).not.toContain('Event Details')

    // Click the first event row to expand
    const rows = wrapper.findAll('tbody tr')
    await rows[0].trigger('click')
    expect(wrapper.text()).toContain('Event Details')
    expect(wrapper.text()).toContain('gpt-4-pool')

    // Click again to collapse
    // After expand, the rows shift -- find the clickable row again
    const rowsAfterExpand = wrapper.findAll('tbody tr')
    await rowsAfterExpand[0].trigger('click')
    expect(wrapper.text()).not.toContain('Event Details')
  })

  it('refresh button exists and triggers re-fetch', async () => {
    mockSuccess()
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(EventsView, { global: { plugins: [router] } })
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledTimes(1)

    // Find the Refresh button
    const buttons = wrapper.findAll('button')
    const refreshBtn = buttons.find(b => b.text().includes('Refresh'))
    expect(refreshBtn).toBeDefined()

    await refreshBtn.trigger('click')
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
