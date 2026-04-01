import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createWebHashHistory } from 'vue-router'
import { nextTick } from 'vue'
import PoolsView from '@/views/PoolsView.vue'
import LoadingSpinner from '@/components/LoadingSpinner.vue'
import ErrorAlert from '@/components/ErrorAlert.vue'
import StatusBadge from '@/components/StatusBadge.vue'

const mockStatusData = {
  status: 'healthy',
  uptime_seconds: 3661,
  models: [],
  pools: [
    {
      name: 'gpt-4-pool',
      strategy: 'simple-shuffle',
      budget_spent: 12.50,
      budget_cap: 100.00,
      deployments: [
        {
          provider: 'openai',
          actual_model: 'gpt-4',
          status: 'healthy',
          failure_count: 0,
          cooldown_until: null,
          weight: 2,
        },
        {
          provider: 'anthropic',
          actual_model: 'claude-3-haiku',
          status: 'cooldown',
          failure_count: 3,
          cooldown_until: new Date(Date.now() + 30000).toISOString(),
          weight: 1,
        },
      ],
    },
    {
      name: 'claude-pool',
      strategy: 'round-robin',
      budget_spent: 0,
      budget_cap: 0,
      deployments: [
        {
          provider: 'anthropic',
          actual_model: 'claude-3-sonnet',
          status: 'backoff',
          failure_count: 0,
          cooldown_until: null,
          weight: 1,
        },
      ],
    },
  ],
  router_settings: {
    routing_strategy: 'simple-shuffle',
    num_retries: 2,
    allowed_fails: 3,
    cooldown_time: '30s',
  },
}

function makeRouter() {
  return createRouter({
    history: createWebHashHistory(),
    routes: [{ path: '/', component: PoolsView }],
  })
}

describe('PoolsView', () => {
  let fetchMock

  beforeEach(() => {
    localStorage.clear()
    fetchMock = vi.fn()
    global.fetch = fetchMock
    vi.useFakeTimers({ shouldAdvanceTime: true })
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  function mockSuccess(data = mockStatusData) {
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

  it('shows a loading spinner while fetching', async () => {
    // Never resolve to keep it in loading state
    fetchMock.mockReturnValue(new Promise(() => {}))
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(PoolsView, { global: { plugins: [router] } })
    await nextTick()
    expect(wrapper.findComponent(LoadingSpinner).exists()).toBe(true)
  })

  it('renders pool cards after successful fetch', async () => {
    mockSuccess()
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(PoolsView, { global: { plugins: [router] } })
    await flushPromises()
    // Should have 2 pool cards
    expect(wrapper.text()).toContain('gpt-4-pool')
    expect(wrapper.text()).toContain('claude-pool')
  })

  it('displays pool name, strategy, and budget in card header', async () => {
    mockSuccess()
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(PoolsView, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('gpt-4-pool')
    expect(wrapper.text()).toContain('simple-shuffle')
    expect(wrapper.text()).toContain('$12.50')
    expect(wrapper.text()).toContain('$100.00')
  })

  it('displays "Unlimited" when budget_cap is 0', async () => {
    mockSuccess()
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(PoolsView, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('Unlimited')
  })

  it('renders deployment table rows with Provider, Model, Status, Failures, Weight columns', async () => {
    mockSuccess()
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(PoolsView, { global: { plugins: [router] } })
    await flushPromises()
    // Check table headers
    expect(wrapper.text()).toContain('Provider')
    expect(wrapper.text()).toContain('Model')
    expect(wrapper.text()).toContain('Status')
    expect(wrapper.text()).toContain('Failures')
    expect(wrapper.text()).toContain('Weight')
    // Check deployment data
    expect(wrapper.text()).toContain('openai')
    expect(wrapper.text()).toContain('gpt-4')
    expect(wrapper.text()).toContain('anthropic')
    expect(wrapper.text()).toContain('claude-3-haiku')
    // Check StatusBadge components exist
    expect(wrapper.findAllComponents(StatusBadge).length).toBeGreaterThanOrEqual(2)
  })

  it('shows ErrorAlert when fetch fails', async () => {
    mockFailure()
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(PoolsView, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.findComponent(ErrorAlert).exists()).toBe(true)
  })

  it('shows empty state "No pools configured" when pools array is empty', async () => {
    mockSuccess({
      ...mockStatusData,
      pools: [],
    })
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(PoolsView, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('No pools configured')
  })

  it('pause button exists and toggles text between "Pause" and "Resume"', async () => {
    mockSuccess()
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(PoolsView, { global: { plugins: [router] } })
    await flushPromises()

    const pauseBtn = wrapper.findAll('button').find((b) => b.text() === 'Pause')
    expect(pauseBtn).toBeTruthy()

    await pauseBtn.trigger('click')
    expect(pauseBtn.text()).toBe('Resume')

    await pauseBtn.trigger('click')
    expect(pauseBtn.text()).toBe('Pause')
  })

  it('shows "Updated Xs ago" text after successful load', async () => {
    mockSuccess()
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(PoolsView, { global: { plugins: [router] } })
    await flushPromises()
    // After load, secondsAgo should be 0
    expect(wrapper.text()).toMatch(/Updated \d+s ago/)
  })
})
