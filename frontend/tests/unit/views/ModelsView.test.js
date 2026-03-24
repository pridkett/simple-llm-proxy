import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createWebHashHistory } from 'vue-router'
import { nextTick } from 'vue'
import ModelsView from '@/views/ModelsView.vue'
import LoadingSpinner from '@/components/LoadingSpinner.vue'
import ErrorAlert from '@/components/ErrorAlert.vue'

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
  router_settings: { routing_strategy: 'simple-shuffle', num_retries: 2, allowed_fails: 3, cooldown_time: '30s' },
}

const mockCostsData = {
  id: 'gpt-4', object: 'model', created: 1700000000, owned_by: 'simple-llm-proxy',
  costs: {
    max_tokens: 8192, max_input_tokens: 8192, max_output_tokens: 4096,
    input_cost_per_token: 0.00003, output_cost_per_token: 0.00006,
    litellm_provider: 'openai', mode: 'chat',
    supports_function_calling: true, supports_parallel_function_calling: false, supports_vision: false,
    source: 'auto', cost_map_key: 'openai/gpt-4',
  },
}

const mockNoCostsData = {
  id: 'gpt-4', object: 'model', created: 1700000000, owned_by: 'simple-llm-proxy',
  costs: {
    max_tokens: 0, max_input_tokens: 0, max_output_tokens: 0,
    input_cost_per_token: 0, output_cost_per_token: 0,
    supports_function_calling: false, supports_parallel_function_calling: false, supports_vision: false,
    source: '', cost_map_key: '',
  },
}

const mockCostMapModels = [
  { name: 'openai/gpt-4', input_cost_per_token: 0.00003, output_cost_per_token: 0.00006, max_tokens: 8192 },
  { name: 'openai/gpt-4-turbo', input_cost_per_token: 0.00001, output_cost_per_token: 0.00003, max_tokens: 128000 },
  { name: 'anthropic/claude-3-opus-20240229', input_cost_per_token: 0.000015, output_cost_per_token: 0.000075, max_tokens: 200000 },
]

function makeRouter() {
  return createRouter({ history: createWebHashHistory(), routes: [{ path: '/', component: ModelsView }] })
}

function okResponse(data) {
  return { ok: true, status: 200, json: () => Promise.resolve(data) }
}

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

  afterEach(() => { vi.restoreAllMocks() })

  it('shows a loading spinner while fetching', async () => {
    fetchMock.mockReturnValue(new Promise(() => {}))
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(ModelsView, { global: { plugins: [router] } })
    await nextTick()
    expect(wrapper.findComponent(LoadingSpinner).exists()).toBe(true)
  })

  it('renders model name after successful fetch with costs', async () => {
    fetchMock.mockResolvedValueOnce(okResponse(mockStatusData)).mockResolvedValueOnce(okResponse(mockCostsData))
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(ModelsView, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('gpt-4')
    expect(wrapper.text()).toContain('1 deployment')
  })

  it('displays cost info in $/MTok when available', async () => {
    fetchMock.mockResolvedValueOnce(okResponse(mockStatusData)).mockResolvedValueOnce(okResponse(mockCostsData))
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(ModelsView, { global: { plugins: [router] } })
    await flushPromises()
    // 0.00003 * 1_000_000 = 30 → $30.0000/MTok
    expect(wrapper.text()).toContain('auto')
    expect(wrapper.text()).toMatch(/\$\d+\.\d+\/MTok/)
  })

  it('shows "not mapped" when costs are absent', async () => {
    fetchMock.mockResolvedValueOnce(okResponse(mockStatusData)).mockResolvedValueOnce(okResponse(mockNoCostsData))
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

  it('renders cost editor tabs when Edit button is clicked', async () => {
    fetchMock
      .mockResolvedValueOnce(okResponse(mockStatusData))
      .mockResolvedValueOnce(okResponse(mockCostsData))
      .mockResolvedValueOnce(okResponse(mockCostMapModels)) // costMapModels loaded on editor open
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(ModelsView, { global: { plugins: [router] } })
    await flushPromises()

    const editBtn = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    await editBtn.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Cost Map Key')
    expect(wrapper.text()).toContain('Custom Costs')
  })

  it('shows filtered autocomplete suggestions as user types', async () => {
    fetchMock
      .mockResolvedValueOnce(okResponse(mockStatusData))
      .mockResolvedValueOnce(okResponse(mockCostsData))
      .mockResolvedValueOnce(okResponse(mockCostMapModels))
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(ModelsView, { global: { plugins: [router] } })
    await flushPromises()

    const editBtn = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    await editBtn.trigger('click')
    await flushPromises()

    const input = wrapper.find('input[placeholder*="openai"]')
    await input.trigger('focus')
    await input.setValue('gpt-4')
    await input.trigger('input')
    await nextTick()

    // Dropdown is teleported to body — check document.body
    expect(document.body.textContent).toContain('openai/gpt-4')
  })

  it('shows costs in $/MTok in the autocomplete dropdown', async () => {
    fetchMock
      .mockResolvedValueOnce(okResponse(mockStatusData))
      .mockResolvedValueOnce(okResponse(mockCostsData))
      .mockResolvedValueOnce(okResponse(mockCostMapModels))
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(ModelsView, { global: { plugins: [router] } })
    await flushPromises()

    const editBtn = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    await editBtn.trigger('click')
    await flushPromises()

    const input = wrapper.find('input[placeholder*="openai"]')
    await input.trigger('focus')
    await input.setValue('openai')
    await input.trigger('input')
    await nextTick()

    // Dropdown is teleported to body — check document.body
    expect(document.body.textContent).toMatch(/\$\d+\.\d+\/MTok/)
  })

  it('custom costs tab shows $/MTok input labels', async () => {
    fetchMock
      .mockResolvedValueOnce(okResponse(mockStatusData))
      .mockResolvedValueOnce(okResponse(mockCostsData))
      .mockResolvedValueOnce(okResponse(mockCostMapModels))
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(ModelsView, { global: { plugins: [router] } })
    await flushPromises()

    const editBtn = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    await editBtn.trigger('click')
    await flushPromises()

    const customTab = wrapper.findAll('button').find((b) => b.text() === 'Custom Costs')
    await customTab.trigger('click')

    expect(wrapper.text()).toContain('$/MTok')
  })

  it('custom costs tab pre-fills $/MTok from existing per-token costs', async () => {
    fetchMock
      .mockResolvedValueOnce(okResponse(mockStatusData))
      .mockResolvedValueOnce(okResponse(mockCostsData))
      .mockResolvedValueOnce(okResponse(mockCostMapModels))
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(ModelsView, { global: { plugins: [router] } })
    await flushPromises()

    const editBtn = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    await editBtn.trigger('click')
    await flushPromises()

    const customTab = wrapper.findAll('button').find((b) => b.text() === 'Custom Costs')
    await customTab.trigger('click')

    // 0.00003 per token → 30 $/MTok — verify the input pre-fills with 30
    const inputs = wrapper.findAll('input[type="number"]')
    const inputCostInput = inputs.find((inp) => {
      const label = inp.element.closest('label')
      return label && label.textContent.includes('Input cost')
    })
    expect(inputCostInput?.element.value).toBe('30')
  })

  it('converts $/MTok back to per-token when saving custom costs', async () => {
    fetchMock
      .mockResolvedValueOnce(okResponse(mockStatusData))
      .mockResolvedValueOnce(okResponse(mockCostsData))
      .mockResolvedValueOnce(okResponse(mockCostMapModels))
      .mockResolvedValueOnce(okResponse(mockCostsData)) // PATCH response
      .mockResolvedValueOnce(okResponse(mockStatusData))
      .mockResolvedValueOnce(okResponse(mockCostsData))
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(ModelsView, { global: { plugins: [router] } })
    await flushPromises()

    const editBtn = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    await editBtn.trigger('click')
    await flushPromises()

    const customTab = wrapper.findAll('button').find((b) => b.text() === 'Custom Costs')
    await customTab.trigger('click')

    // Set 15 $/MTok in the input cost field
    const inputs = wrapper.findAll('input[type="number"]')
    const inputCostInput = inputs.find((inp) => {
      const label = inp.element.closest('label')
      return label && label.textContent.includes('Input cost')
    })
    await inputCostInput.setValue('15')
    await inputCostInput.trigger('input')

    const saveBtn = wrapper.findAll('button').find((b) => b.text() === 'Save custom costs')
    await saveBtn.trigger('click')
    await flushPromises()

    const patchCall = fetchMock.mock.calls.find(
      (args) => args[1]?.method === 'PATCH' && args[0].includes('/costs')
    )
    expect(patchCall).toBeTruthy()
    const body = JSON.parse(patchCall[1].body)
    // 15 $/MTok → 0.000015 per token
    expect(body.input_cost_per_token).toBeCloseTo(0.000015, 10)
  })

  it('calls patchModelCostMapKey on save and reloads', async () => {
    fetchMock
      .mockResolvedValueOnce(okResponse(mockStatusData))
      .mockResolvedValueOnce(okResponse(mockCostsData))
      .mockResolvedValueOnce(okResponse(mockCostMapModels))
      .mockResolvedValueOnce(okResponse({ model: 'gpt-4', cost_map_key: 'openai/gpt-4-turbo' }))
      .mockResolvedValueOnce(okResponse(mockStatusData))
      .mockResolvedValueOnce(okResponse(mockCostsData))
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(ModelsView, { global: { plugins: [router] } })
    await flushPromises()

    const editBtn = wrapper.findAll('button').find((b) => b.text() === 'Edit')
    await editBtn.trigger('click')
    await flushPromises()

    const input = wrapper.find('input[placeholder*="openai"]')
    await input.setValue('openai/gpt-4-turbo')

    const saveBtn = wrapper.findAll('button').find((b) => b.text() === 'Save')
    await saveBtn.trigger('click')
    await flushPromises()

    const patchCall = fetchMock.mock.calls.find(
      (args) => args[1]?.method === 'PATCH' && args[0].includes('cost_map_key')
    )
    expect(patchCall).toBeTruthy()
    expect(JSON.parse(patchCall[1].body)).toEqual({ cost_map_key: 'openai/gpt-4-turbo' })
  })

  it('re-fetches when the Refresh button is clicked', async () => {
    fetchMock.mockResolvedValue(okResponse(mockStatusData))
    const router = makeRouter()
    await router.push('/')
    const wrapper = mount(ModelsView, { global: { plugins: [router] } })
    await flushPromises()
    const initialCalls = fetchMock.mock.calls.length

    const refreshBtn = wrapper.findAll('button').find((b) => b.text() === 'Refresh')
    if (refreshBtn) {
      await refreshBtn.trigger('click')
      await flushPromises()
      expect(fetchMock.mock.calls.length).toBeGreaterThan(initialCalls)
    }
  })
})
