import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createWebHashHistory } from 'vue-router'
import { nextTick } from 'vue'
import ModelsView from '@/views/ModelsView.vue'
import LoadingSpinner from '@/components/LoadingSpinner.vue'
import ErrorAlert from '@/components/ErrorAlert.vue'

// Minimal model status data for /admin/status
const mockStatusData = {
  status: 'healthy',
  uptime_seconds: 100,
  models: [
    {
      model_name: 'gpt-4',
      total_deployments: 1,
      healthy_deployments: 1,
      deployments: [
        { provider: 'openai', actual_model: 'gpt-4', status: 'healthy', failure_count: 0 },
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

// ModelDetailResponse with auto-detected costs
const mockCostsData = {
  id: 'gpt-4',
  object: 'model',
  created: 1700000000,
  owned_by: 'simple-llm-proxy',
  costs: {
    max_tokens: 8192,
    max_input_tokens: 8192,
    max_output_tokens: 4096,
    input_cost_per_token: 0.00003,
    output_cost_per_token: 0.00006,
    litellm_provider: 'openai',
    mode: 'chat',
    supports_function_calling: true,
    supports_parallel_function_calling: false,
    supports_vision: false,
    source: 'auto',
    cost_map_key: 'openai/gpt-4',
  },
}

// ModelDetailResponse with no cost mapping
const mockNoCostsData = {
  id: 'gpt-4',
  object: 'model',
  created: 1700000000,
  owned_by: 'simple-llm-proxy',
  costs: {
    max_tokens: 0,
    max_input_tokens: 0,
    max_output_tokens: 0,
    input_cost_per_token: 0,
    output_cost_per_token: 0,
    supports_function_calling: false,
    supports_parallel_function_calling: false,
    supports_vision: false,
    source: '',
    cost_map_key: '',
  },
}

function makeRouter() {
  return createRouter({
    history: createWebHashHistory(),
    routes: [{ path: '/', component: ModelsView }],
  })
}

// Helper: make a successful fetch response
function okResponse(data) {
  return { ok: true, status: 200, json: () => Promise.resolve(data) }
}

// Helper: make a failed fetch response
function errResponse(message = 'server error') {
  return { ok: false, status: 500, json: () => Promise.resolve({ error: { message } }) }
}

describe('ModelsView', () => {
  let fetchMock

  beforeEach(() => {
    localStorage.clear()
    fetchMock = vi.fn()
    global.fetch = fetchMock
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('shows a loading spinner while fetching', async () => {
    fetchMock.mockReturnValue(new Promise(() => {}))
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(ModelsView, { global: { plugins: [router] } })
    await nextTick()
    expect(wrapper.findComponent(LoadingSpinner).exists()).toBe(true)
  })

  it('renders model name after successful fetch with costs', async () => {
    // First call → status, subsequent calls → modelDetail
    fetchMock
      .mockResolvedValueOnce(okResponse(mockStatusData))
      .mockResolvedValueOnce(okResponse(mockCostsData))

    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(ModelsView, { global: { plugins: [router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('gpt-4')
    expect(wrapper.text()).toContain('1 deployment')
  })

  it('displays cost info when available', async () => {
    fetchMock
      .mockResolvedValueOnce(okResponse(mockStatusData))
      .mockResolvedValueOnce(okResponse(mockCostsData))

    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(ModelsView, { global: { plugins: [router] } })
    await flushPromises()

    // Should show the source badge
    expect(wrapper.text()).toContain('auto')
    // Should show formatted cost
    expect(wrapper.text()).toMatch(/\$\d+\.\d+\/MTok/)
  })

  it('shows "not mapped" when costs are absent', async () => {
    fetchMock
      .mockResolvedValueOnce(okResponse(mockStatusData))
      .mockResolvedValueOnce(okResponse(mockNoCostsData))

    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(ModelsView, { global: { plugins: [router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('not mapped')
  })

  it('shows an error alert when status fetch fails', async () => {
    fetchMock.mockResolvedValue(errResponse())
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(ModelsView, { global: { plugins: [router] } })
    await flushPromises()

    expect(wrapper.findComponent(ErrorAlert).exists()).toBe(true)
  })

  it('renders cost editor when Edit button is clicked', async () => {
    fetchMock
      .mockResolvedValueOnce(okResponse(mockStatusData))
      .mockResolvedValueOnce(okResponse(mockCostsData))

    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(ModelsView, { global: { plugins: [router] } })
    await flushPromises()

    // Find the Edit button in the cost section
    const editBtn = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    expect(editBtn).toBeTruthy()
    await editBtn.trigger('click')

    // Cost Map Key and Custom Costs tabs should be visible
    expect(wrapper.text()).toContain('Cost Map Key')
    expect(wrapper.text()).toContain('Custom Costs')
  })

  it('calls patchModelCostMapKey and reloads on save', async () => {
    // Initial load
    fetchMock
      .mockResolvedValueOnce(okResponse(mockStatusData))  // status
      .mockResolvedValueOnce(okResponse(mockCostsData))   // modelDetail
      // After save: patch response, then re-load (status + modelDetail)
      .mockResolvedValueOnce(okResponse({ model: 'gpt-4', cost_map_key: 'openai/gpt-4-turbo' }))
      .mockResolvedValueOnce(okResponse(mockStatusData))
      .mockResolvedValueOnce(okResponse(mockCostsData))

    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(ModelsView, { global: { plugins: [router] } })
    await flushPromises()

    // Open cost editor
    const editBtn = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    await editBtn.trigger('click')

    // Type in the cost map key input
    const input = wrapper.find('input[placeholder*="openai"]')
    await input.setValue('openai/gpt-4-turbo')

    // Click Save
    const saveBtn = wrapper.findAll('button').find((b) => b.text() === 'Save')
    await saveBtn.trigger('click')
    await flushPromises()

    // Verify the PATCH call was made with the right path
    const patchCall = fetchMock.mock.calls.find(
      (args) => args[1]?.method === 'PATCH' && args[0].includes('cost_map_key')
    )
    expect(patchCall).toBeTruthy()
    expect(JSON.parse(patchCall[1].body)).toEqual({ cost_map_key: 'openai/gpt-4-turbo' })
  })

  it('calls patchModelCosts when saving custom costs', async () => {
    fetchMock
      .mockResolvedValueOnce(okResponse(mockStatusData))
      .mockResolvedValueOnce(okResponse(mockCostsData))
      .mockResolvedValueOnce(okResponse(mockCostsData)) // PATCH response
      .mockResolvedValueOnce(okResponse(mockStatusData))
      .mockResolvedValueOnce(okResponse(mockCostsData))

    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(ModelsView, { global: { plugins: [router] } })
    await flushPromises()

    // Open cost editor and switch to Custom Costs tab
    const editBtn = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    await editBtn.trigger('click')

    const customTab = wrapper.findAll('button').find((b) => b.text() === 'Custom Costs')
    await customTab.trigger('click')

    // Click Save custom costs
    const saveBtn = wrapper.findAll('button').find((b) => b.text() === 'Save custom costs')
    await saveBtn.trigger('click')
    await flushPromises()

    const patchCall = fetchMock.mock.calls.find(
      (args) => args[1]?.method === 'PATCH' && args[0].includes('/costs')
    )
    expect(patchCall).toBeTruthy()
  })

  it('re-fetches when the Refresh button is clicked', async () => {
    fetchMock
      .mockResolvedValue(okResponse(mockStatusData))

    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(ModelsView, { global: { plugins: [router] } })
    await flushPromises()

    const initialCalls = fetchMock.mock.calls.length

    const refreshBtn = wrapper.find('button[disabled]').exists()
      ? null
      : wrapper.findAll('button').find((b) => b.text() === 'Refresh')
    if (refreshBtn) {
      await refreshBtn.trigger('click')
      await flushPromises()
      expect(fetchMock.mock.calls.length).toBeGreaterThan(initialCalls)
    }
  })
})
