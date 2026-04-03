import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createWebHashHistory } from 'vue-router'
import { ref } from 'vue'

// Mock useSession
vi.mock('@/composables/useSession.js', () => ({
  useSession: () => ({
    isAuthenticated: ref(true),
    currentUser: ref({ id: 'u1', email: 'test@example.com', name: 'Test', is_admin: true }),
    loading: ref(false),
    fetchCurrentUser: vi.fn().mockResolvedValue(true),
    clearSession: vi.fn(),
  }),
}))

// Mock API
const mockModels = vi.fn()
const mockMyKeys = vi.fn()
const mockChatCompletionStream = vi.fn()

vi.mock('@/api/client.js', () => ({
  api: {
    models: (...args) => mockModels(...args),
    myKeys: (...args) => mockMyKeys(...args),
    chatCompletionStream: (...args) => mockChatCompletionStream(...args),
  },
}))

import ChatView from '@/views/ChatView.vue'

function makeRouter() {
  return createRouter({
    history: createWebHashHistory(),
    routes: [
      { path: '/', component: { template: '<div/>' } },
      { path: '/chat', name: 'chat', component: ChatView },
    ],
  })
}

/** Helper: open the dropdown, find a model option, and click it */
async function selectModelFromDropdown(wrapper, modelName) {
  const input = wrapper.find('input[type="text"]')
  await input.setValue(modelName)
  await input.trigger('focus')
  const option = wrapper.findAll('.max-h-48 button').find(b => b.text() === modelName)
  if (option) await option.trigger('mousedown')
}

describe('ChatView', () => {
  beforeEach(() => {
    mockModels.mockReset()
    mockMyKeys.mockReset()
    mockChatCompletionStream.mockReset()
    // Default: admin user with no keys (master key available)
    mockMyKeys.mockResolvedValue([])
  })

  it('renders heading and empty state when no models selected', async () => {
    mockModels.mockResolvedValue({ data: [{ id: 'gpt-4' }, { id: 'claude-3' }] })
    const router = makeRouter()
    await router.push('/chat')
    const wrapper = mount(ChatView, { global: { plugins: [router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('Chat')
    expect(wrapper.text()).toContain('Test models and compare responses side by side')
    expect(wrapper.text()).toContain('Select one or more models above to start chatting')
  })

  it('shows search input after loading', async () => {
    mockModels.mockResolvedValue({ data: [{ id: 'gpt-4' }, { id: 'claude-3' }] })
    const router = makeRouter()
    await router.push('/chat')
    const wrapper = mount(ChatView, { global: { plugins: [router] } })
    await flushPromises()

    const input = wrapper.find('input[type="text"]')
    expect(input.exists()).toBe(true)
    expect(input.attributes('placeholder')).toContain('Search and select models')
  })

  it('shows loading state while fetching models', async () => {
    mockModels.mockReturnValue(new Promise(() => {})) // never resolves
    const router = makeRouter()
    await router.push('/chat')
    const wrapper = mount(ChatView, { global: { plugins: [router] } })

    expect(wrapper.text()).toContain('Loading models')
  })

  it('shows error state when no models available', async () => {
    mockModels.mockResolvedValue({ data: [] })
    const router = makeRouter()
    await router.push('/chat')
    const wrapper = mount(ChatView, { global: { plugins: [router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('No models available')
  })

  it('sorts models alphabetically', async () => {
    mockModels.mockResolvedValue({ data: [{ id: 'zeta' }, { id: 'alpha' }, { id: 'mid' }] })
    const router = makeRouter()
    await router.push('/chat')
    const wrapper = mount(ChatView, { global: { plugins: [router] } })
    await flushPromises()

    // Open dropdown
    const input = wrapper.find('input[type="text"]')
    await input.trigger('focus')

    const options = wrapper.findAll('.max-h-48 button').map(b => b.text())
    expect(options).toEqual(['alpha', 'mid', 'zeta'])
  })

  it('filters models by search query', async () => {
    mockModels.mockResolvedValue({ data: [{ id: 'gpt-4' }, { id: 'claude-3' }, { id: 'gemini-pro' }] })
    const router = makeRouter()
    await router.push('/chat')
    const wrapper = mount(ChatView, { global: { plugins: [router] } })
    await flushPromises()

    const input = wrapper.find('input[type="text"]')
    await input.setValue('cl')
    await input.trigger('focus')

    const options = wrapper.findAll('.max-h-48 button').map(b => b.text())
    expect(options).toEqual(['claude-3'])
  })

  it('selects a model from the dropdown', async () => {
    mockModels.mockResolvedValue({ data: [{ id: 'gpt-4' }, { id: 'claude-3' }] })
    const router = makeRouter()
    await router.push('/chat')
    const wrapper = mount(ChatView, { global: { plugins: [router] } })
    await flushPromises()

    await selectModelFromDropdown(wrapper, 'gpt-4')

    // After selection: chip visible, empty state gone, panel appears
    expect(wrapper.text()).not.toContain('Select one or more models above')
    expect(wrapper.text()).toContain('gpt-4')
    expect(wrapper.text()).toContain('Send a message to start')
  })

  it('limits selection to 4 models and hides search input', async () => {
    mockModels.mockResolvedValue({
      data: [
        { id: 'model-1' }, { id: 'model-2' }, { id: 'model-3' },
        { id: 'model-4' }, { id: 'model-5' },
      ],
    })
    const router = makeRouter()
    await router.push('/chat')
    const wrapper = mount(ChatView, { global: { plugins: [router] } })
    await flushPromises()

    // Select 4 models via dropdown
    for (const name of ['model-1', 'model-2', 'model-3', 'model-4']) {
      await selectModelFromDropdown(wrapper, name)
    }

    // Search input should be gone, replaced by "Maximum 4" message
    expect(wrapper.find('input[type="text"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Maximum 4 models selected')
  })

  it('disables send button when no models selected', async () => {
    mockModels.mockResolvedValue({ data: [{ id: 'gpt-4' }] })
    const router = makeRouter()
    await router.push('/chat')
    const wrapper = mount(ChatView, { global: { plugins: [router] } })
    await flushPromises()

    const sendBtn = wrapper.findAll('button').find(b => b.text() === 'Send')
    expect(sendBtn.attributes('disabled')).toBeDefined()
  })

  it('removes a model when its chip X button is clicked', async () => {
    mockModels.mockResolvedValue({ data: [{ id: 'gpt-4' }] })
    const router = makeRouter()
    await router.push('/chat')
    const wrapper = mount(ChatView, { global: { plugins: [router] } })
    await flushPromises()

    await selectModelFromDropdown(wrapper, 'gpt-4')
    expect(wrapper.text()).not.toContain('Select one or more models above')

    // Click the X button on the chip
    const removeBtn = wrapper.find('.bg-indigo-600 button')
    await removeBtn.trigger('click')
    expect(wrapper.text()).toContain('Select one or more models above')
  })
})
