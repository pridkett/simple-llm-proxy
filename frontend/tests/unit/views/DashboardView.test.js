import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createWebHashHistory } from 'vue-router'
import { nextTick } from 'vue'
import DashboardView from '@/views/DashboardView.vue'
import LoadingSpinner from '@/components/LoadingSpinner.vue'
import ErrorAlert from '@/components/ErrorAlert.vue'

const mockStatusData = {
  status: 'healthy',
  uptime_seconds: 3661,
  models: [
    {
      model_name: 'gpt-4',
      total_deployments: 2,
      healthy_deployments: 2,
      deployments: [],
    },
    {
      model_name: 'claude-3',
      total_deployments: 1,
      healthy_deployments: 0,
      deployments: [],
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
    routes: [{ path: '/', component: DashboardView }],
  })
}

describe('DashboardView', () => {
  let fetchMock

  beforeEach(() => {
    localStorage.clear()
    fetchMock = vi.fn()
    global.fetch = fetchMock
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  function mockSuccess() {
    fetchMock.mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve(mockStatusData),
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
    const wrapper = mount(DashboardView, { global: { plugins: [router] } })
    await nextTick()
    expect(wrapper.findComponent(LoadingSpinner).exists()).toBe(true)
  })

  it('renders summary cards after successful fetch', async () => {
    mockSuccess()
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(DashboardView, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('healthy')
    expect(wrapper.text()).toContain('2') // 2 models
  })

  it('shows formatted uptime', async () => {
    mockSuccess()
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(DashboardView, { global: { plugins: [router] } })
    await flushPromises()
    // 3661 seconds = 1h 1m
    expect(wrapper.text()).toContain('1h')
  })

  it('shows cooldown count', async () => {
    mockSuccess()
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(DashboardView, { global: { plugins: [router] } })
    await flushPromises()
    // claude-3 has 0 healthy out of 1 → 1 in cooldown
    expect(wrapper.text()).toContain('1')
  })

  it('displays an error alert when the fetch fails', async () => {
    mockFailure()
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(DashboardView, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.findComponent(ErrorAlert).exists()).toBe(true)
  })

  it('renders router settings', async () => {
    mockSuccess()
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(DashboardView, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('simple-shuffle')
    expect(wrapper.text()).toContain('30s')
  })

  it('re-fetches when the Refresh button is clicked', async () => {
    mockSuccess()
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(DashboardView, { global: { plugins: [router] } })
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await wrapper.find('button').trigger('click')
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
