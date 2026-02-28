import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ModelPlayground from '@/components/ModelPlayground.vue'
import ErrorAlert from '@/components/ErrorAlert.vue'

// Stub the api module
vi.mock('@/api/client.js', () => ({
  api: {
    chatCompletion: vi.fn(),
    chatCompletionStream: vi.fn(),
  },
}))

import { api } from '@/api/client.js'

// Helper: builds a ReadableStream that yields SSE lines for the given text chunks.
function makeStreamBody(chunks) {
  const encoder = new TextEncoder()
  const lines = chunks.map(
    (c) => `data: ${JSON.stringify({ choices: [{ delta: { content: c } }] })}\n\n`
  )
  lines.push('data: [DONE]\n\n')
  const combined = lines.join('')
  return new ReadableStream({
    start(controller) {
      controller.enqueue(encoder.encode(combined))
      controller.close()
    },
  })
}

function mountPlayground(modelName = 'gpt-4') {
  return mount(ModelPlayground, { props: { modelName } })
}

// The component has two <textarea> elements:
//   index 0 = system message (inside <details>)
//   index 1 = user message
function userTextarea(wrapper) {
  return wrapper.findAll('textarea')[1]
}

function streamCheckbox(wrapper) {
  return wrapper.find('input[type="checkbox"]')
}

function sendButton(wrapper) {
  return wrapper.findAll('button').find((b) => b.text().includes('Send'))
}

describe('ModelPlayground', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders the "Test request" heading', () => {
    const wrapper = mountPlayground('claude-3')
    expect(wrapper.text()).toContain('Test request')
  })

  it('Send button is disabled when user message is empty', () => {
    const wrapper = mountPlayground()
    expect(sendButton(wrapper).element.disabled).toBe(true)
  })

  it('Send button is enabled after typing a message in the user field', async () => {
    const wrapper = mountPlayground()
    await userTextarea(wrapper).setValue('Hello')
    expect(sendButton(wrapper).element.disabled).toBe(false)
  })

  describe('non-streaming mode', () => {
    async function setupNonStreaming(wrapper) {
      await streamCheckbox(wrapper).setChecked(false)
    }

    it('calls chatCompletion with correct model and messages', async () => {
      api.chatCompletion.mockResolvedValue({
        choices: [{ message: { content: 'Hello there!' } }],
        usage: { prompt_tokens: 5, completion_tokens: 3, total_tokens: 8 },
      })

      const wrapper = mountPlayground('gpt-4')
      await setupNonStreaming(wrapper)
      await userTextarea(wrapper).setValue('Hi')
      await sendButton(wrapper).trigger('click')
      await flushPromises()

      expect(api.chatCompletion).toHaveBeenCalledWith(
        'gpt-4',
        [{ role: 'user', content: 'Hi' }],
        expect.objectContaining({ temperature: expect.any(Number) })
      )
      expect(wrapper.text()).toContain('Hello there!')
    })

    it('prepends system message when provided', async () => {
      api.chatCompletion.mockResolvedValue({
        choices: [{ message: { content: 'OK' } }],
        usage: null,
      })

      const wrapper = mountPlayground('gpt-4')
      await setupNonStreaming(wrapper)

      // textareas[0] = system message, textareas[1] = user message
      await wrapper.findAll('textarea')[0].setValue('Be concise.')
      await userTextarea(wrapper).setValue('Explain gravity.')
      await sendButton(wrapper).trigger('click')
      await flushPromises()

      const [, messages] = api.chatCompletion.mock.calls[0]
      expect(messages[0]).toEqual({ role: 'system', content: 'Be concise.' })
      expect(messages[1]).toEqual({ role: 'user', content: 'Explain gravity.' })
    })

    it('shows token usage stats after response', async () => {
      api.chatCompletion.mockResolvedValue({
        choices: [{ message: { content: 'Response' } }],
        usage: { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15 },
      })

      const wrapper = mountPlayground()
      await setupNonStreaming(wrapper)
      await userTextarea(wrapper).setValue('Question')
      await sendButton(wrapper).trigger('click')
      await flushPromises()

      const text = wrapper.text()
      expect(text).toContain('10')
      expect(text).toContain('15')
    })

    it('shows error alert on API failure', async () => {
      api.chatCompletion.mockRejectedValue(new Error('Model not found'))

      const wrapper = mountPlayground()
      await setupNonStreaming(wrapper)
      await userTextarea(wrapper).setValue('Hi')
      await sendButton(wrapper).trigger('click')
      await flushPromises()

      expect(wrapper.findComponent(ErrorAlert).exists()).toBe(true)
      expect(wrapper.text()).toContain('Model not found')
    })

    it('clears previous response before sending a new request', async () => {
      api.chatCompletion.mockResolvedValueOnce({
        choices: [{ message: { content: 'First response' } }],
        usage: null,
      })
      api.chatCompletion.mockResolvedValueOnce({
        choices: [{ message: { content: 'Second response' } }],
        usage: null,
      })

      const wrapper = mountPlayground()
      await setupNonStreaming(wrapper)
      await userTextarea(wrapper).setValue('First')
      await sendButton(wrapper).trigger('click')
      await flushPromises()
      expect(wrapper.text()).toContain('First response')

      await sendButton(wrapper).trigger('click')
      await flushPromises()
      expect(wrapper.text()).not.toContain('First response')
      expect(wrapper.text()).toContain('Second response')
    })
  })

  describe('streaming mode', () => {
    it('calls chatCompletionStream and renders streamed text', async () => {
      api.chatCompletionStream.mockResolvedValue(makeStreamBody(['Hello', ', ', 'world!']))

      const wrapper = mountPlayground('gpt-4')
      // Streaming is on by default
      await userTextarea(wrapper).setValue('Hi')
      await sendButton(wrapper).trigger('click')
      await flushPromises()

      expect(api.chatCompletionStream).toHaveBeenCalledWith(
        'gpt-4',
        [{ role: 'user', content: 'Hi' }],
        expect.objectContaining({ signal: expect.any(AbortSignal) })
      )
      expect(wrapper.text()).toContain('Hello, world!')
    })

    it('shows error alert when stream request fails', async () => {
      api.chatCompletionStream.mockRejectedValue(new Error('Provider error'))

      const wrapper = mountPlayground()
      await userTextarea(wrapper).setValue('Hi')
      await sendButton(wrapper).trigger('click')
      await flushPromises()

      expect(wrapper.findComponent(ErrorAlert).exists()).toBe(true)
      expect(wrapper.text()).toContain('Provider error')
    })
  })

  describe('temperature control', () => {
    it('passes temperature value to chatCompletion', async () => {
      api.chatCompletion.mockResolvedValue({
        choices: [{ message: { content: 'ok' } }],
        usage: null,
      })

      const wrapper = mountPlayground()
      await streamCheckbox(wrapper).setChecked(false)
      await wrapper.find('input[type="range"]').setValue('0.5')
      await userTextarea(wrapper).setValue('test')
      await sendButton(wrapper).trigger('click')
      await flushPromises()

      const [, , options] = api.chatCompletion.mock.calls[0]
      expect(options.temperature).toBeCloseTo(0.5)
    })

    it('displays current temperature value on the label', async () => {
      const wrapper = mountPlayground()
      await wrapper.find('input[type="range"]').setValue('0.7')
      expect(wrapper.text()).toContain('Temp 0.7')
    })
  })
})
