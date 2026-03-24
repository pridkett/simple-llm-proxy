import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ConfigView from '@/views/ConfigView.vue'

// Mock the API module
vi.mock('@/api/client.js', () => ({
  api: {
    config: vi.fn(),
    reload: vi.fn(),
    costMapStatus: vi.fn(),
    costMapReload: vi.fn(),
    costMapSetURL: vi.fn(),
  },
}))

// Mock child components to keep tests focused on ConfigView logic
vi.mock('@/components/LoadingSpinner.vue', () => ({ default: { template: '<div class="spinner" />' } }))
vi.mock('@/components/ErrorAlert.vue', () => ({ default: { template: '<div class="error" />', props: ['title', 'message'] } }))

const configPayload = {
  model_list: [
    { model_name: 'gpt-4', provider: 'openai', actual_model: 'gpt-4', api_key_set: true, api_base: '', rpm: 100, tpm: 0 },
  ],
  router_settings: { routing_strategy: 'simple-shuffle', num_retries: 2, allowed_fails: 3, cooldown_time: '30s' },
  general_settings: { master_key_set: true, database_url: './proxy.db', port: 8080 },
}

const notLoadedStatus = { loaded: false, model_count: 0, url: 'https://example.com/default.json', loaded_at: null }
const loadedStatus = { loaded: true, model_count: 5432, url: 'https://example.com/custom.json', loaded_at: '2026-01-15T10:00:00Z' }

async function mountView() {
  const { api } = await import('@/api/client.js')
  const wrapper = mount(ConfigView, { global: { stubs: { LoadingSpinner: true, ErrorAlert: true } } })
  await flushPromises()
  return { wrapper, api }
}

describe('ConfigView — cost map section', () => {
  beforeEach(async () => {
    vi.clearAllMocks()
    const { api } = await import('@/api/client.js')
    api.config.mockResolvedValue(configPayload)
  })

  it('shows "Not loaded" when cost map status returns loaded=false', async () => {
    const { api } = await import('@/api/client.js')
    api.costMapStatus.mockResolvedValue(notLoadedStatus)

    const { wrapper } = await mountView()

    expect(wrapper.text()).toContain('Not loaded')
    expect(wrapper.text()).toContain('LiteLLM Cost Map')
  })

  it('shows "Loaded" and model count when cost map is loaded', async () => {
    const { api } = await import('@/api/client.js')
    api.costMapStatus.mockResolvedValue(loadedStatus)

    const { wrapper } = await mountView()

    expect(wrapper.text()).toContain('Loaded')
    expect(wrapper.text()).toContain('5432')
  })

  it('pre-populates the URL input from status response', async () => {
    const { api } = await import('@/api/client.js')
    api.costMapStatus.mockResolvedValue(loadedStatus)

    const { wrapper } = await mountView()

    const input = wrapper.find('input[type="text"]')
    expect(input.element.value).toBe('https://example.com/custom.json')
  })

  it('shows "—" for model count and last loaded when not yet loaded', async () => {
    const { api } = await import('@/api/client.js')
    api.costMapStatus.mockResolvedValue(notLoadedStatus)

    const { wrapper } = await mountView()

    const text = wrapper.text()
    // model count and last loaded show em dash
    expect(text).toContain('—')
  })

  it('reload button calls costMapReload then re-fetches status', async () => {
    const { api } = await import('@/api/client.js')
    api.costMapStatus.mockResolvedValue(notLoadedStatus)
    api.costMapReload.mockResolvedValue({ status: 'ok', model_count: 5432 })

    const { wrapper } = await mountView()

    const reloadBtn = wrapper.findAll('button').find(b => b.text().includes('Reload Cost Map'))
    expect(reloadBtn).toBeDefined()
    await reloadBtn.trigger('click')
    await flushPromises()

    expect(api.costMapReload).toHaveBeenCalledOnce()
    expect(api.costMapStatus).toHaveBeenCalledTimes(2) // once on mount, once after reload
  })

  it('shows error message when reload fails', async () => {
    const { api } = await import('@/api/client.js')
    api.costMapStatus.mockResolvedValue(notLoadedStatus)
    api.costMapReload.mockRejectedValue(new Error('network failure'))

    const { wrapper } = await mountView()

    const reloadBtn = wrapper.findAll('button').find(b => b.text().includes('Reload Cost Map'))
    await reloadBtn.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('network failure')
  })

  it('update URL button calls costMapSetURL with input value', async () => {
    const { api } = await import('@/api/client.js')
    api.costMapStatus.mockResolvedValue(notLoadedStatus)
    api.costMapSetURL.mockResolvedValue({ url: 'https://new.example.com/models.json' })

    const { wrapper } = await mountView()

    const input = wrapper.find('input[type="text"]')
    await input.setValue('https://new.example.com/models.json')

    const updateBtn = wrapper.findAll('button').find(b => b.text().includes('Update URL'))
    await updateBtn.trigger('click')
    await flushPromises()

    expect(api.costMapSetURL).toHaveBeenCalledWith('https://new.example.com/models.json')
  })

  it('shows error when costMapSetURL fails', async () => {
    const { api } = await import('@/api/client.js')
    api.costMapStatus.mockResolvedValue(notLoadedStatus)
    api.costMapSetURL.mockRejectedValue(new Error('URL scheme must be http or https'))

    const { wrapper } = await mountView()

    const updateBtn = wrapper.findAll('button').find(b => b.text().includes('Update URL'))
    await updateBtn.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('URL scheme must be http or https')
  })

  it('shows "Not loaded" even when costMapStatus call fails', async () => {
    const { api } = await import('@/api/client.js')
    api.costMapStatus.mockRejectedValue(new Error('network error'))

    const { wrapper } = await mountView()

    // Component should still render without crashing
    expect(wrapper.text()).toContain('LiteLLM Cost Map')
    expect(wrapper.text()).toContain('Not loaded')
  })
})
