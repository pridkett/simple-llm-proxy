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
const mockChatCompletionStream = vi.fn()

vi.mock('@/api/client.js', () => ({
  api: {
    models: (...args) => mockModels(...args),
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

describe('ChatView', () => {
  beforeEach(() => {
    mockModels.mockReset()
    mockChatCompletionStream.mockReset()
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

  it('renders model pills after loading', async () => {
    mockModels.mockResolvedValue({ data: [{ id: 'gpt-4' }, { id: 'claude-3' }] })
    const router = makeRouter()
    await router.push('/chat')
    const wrapper = mount(ChatView, { global: { plugins: [router] } })
    await flushPromises()

    const buttons = wrapper.findAll('button').filter(b => ['gpt-4', 'claude-3'].includes(b.text()))
    expect(buttons.length).toBe(2)
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

  it('selects a model when pill is clicked', async () => {
    mockModels.mockResolvedValue({ data: [{ id: 'gpt-4' }, { id: 'claude-3' }] })
    const router = makeRouter()
    await router.push('/chat')
    const wrapper = mount(ChatView, { global: { plugins: [router] } })
    await flushPromises()

    const gpt4Pill = wrapper.findAll('button').find(b => b.text() === 'gpt-4')
    await gpt4Pill.trigger('click')

    // After selection, empty state should be gone and a ChatPanel should appear
    expect(wrapper.text()).not.toContain('Select one or more models above')
    expect(wrapper.text()).toContain('gpt-4')
    expect(wrapper.text()).toContain('Send a message to start')
  })

  it('limits selection to 4 models', async () => {
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

    // Select 4 models
    for (const name of ['model-1', 'model-2', 'model-3', 'model-4']) {
      const pill = wrapper.findAll('button').find(b => b.text() === name)
      await pill.trigger('click')
    }

    // 5th pill should have disabled styling (cursor-not-allowed)
    const fifthPill = wrapper.findAll('button').find(b => b.text() === 'model-5')
    expect(fifthPill.classes()).toContain('cursor-not-allowed')
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

  it('deselects a model when its pill is clicked again', async () => {
    mockModels.mockResolvedValue({ data: [{ id: 'gpt-4' }] })
    const router = makeRouter()
    await router.push('/chat')
    const wrapper = mount(ChatView, { global: { plugins: [router] } })
    await flushPromises()

    const pill = wrapper.findAll('button').find(b => b.text() === 'gpt-4')
    await pill.trigger('click') // select
    expect(wrapper.text()).not.toContain('Select one or more models above')

    await pill.trigger('click') // deselect
    expect(wrapper.text()).toContain('Select one or more models above')
  })
})
