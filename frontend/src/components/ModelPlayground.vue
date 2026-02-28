<template>
  <div class="border-t border-gray-100 bg-gray-50/50">
    <div class="px-6 py-5 space-y-4">
      <h3 class="text-sm font-semibold text-gray-700">Test request</h3>

      <!-- System message (collapsed by default) -->
      <details class="group">
        <summary class="text-xs text-gray-500 cursor-pointer select-none list-none flex items-center gap-1 hover:text-gray-700">
          <svg
            class="w-3 h-3 transition-transform group-open:rotate-90"
            fill="currentColor" viewBox="0 0 20 20"
          >
            <path d="M6 6l8 4-8 4V6z" />
          </svg>
          System message (optional)
        </summary>
        <textarea
          v-model="systemMessage"
          class="input mt-2 resize-y text-sm font-mono"
          rows="2"
          placeholder="You are a helpful assistant."
          :disabled="sending"
        />
      </details>

      <!-- User message -->
      <div>
        <textarea
          v-model="userMessage"
          class="input resize-y text-sm"
          rows="3"
          placeholder="Type your message…"
          :disabled="sending"
          @keydown.ctrl.enter.prevent="send"
          @keydown.meta.enter.prevent="send"
        />
        <p class="mt-1 text-xs text-gray-400">Ctrl+Enter to send</p>
      </div>

      <!-- Controls -->
      <div class="flex flex-wrap items-center gap-x-5 gap-y-2">
        <label class="flex items-center gap-2 text-xs text-gray-600">
          <span>Temp {{ temperature.toFixed(1) }}</span>
          <input
            type="range" v-model.number="temperature"
            min="0" max="2" step="0.1"
            class="w-24 accent-indigo-600"
            :disabled="sending"
          />
        </label>

        <label class="flex items-center gap-1.5 text-xs text-gray-600 cursor-pointer select-none">
          <input type="checkbox" v-model="stream" class="accent-indigo-600" :disabled="sending" />
          Stream
        </label>

        <div class="flex gap-2 ml-auto">
          <button
            v-if="sending"
            class="btn-secondary text-xs"
            @click="abort"
          >
            Stop
          </button>
          <button
            class="btn-primary text-xs"
            :disabled="sending || !userMessage.trim()"
            @click="send"
          >
            {{ sending ? 'Sending…' : 'Send' }}
          </button>
        </div>
      </div>

      <!-- Error -->
      <ErrorAlert v-if="error" :title="error" />

      <!-- Response -->
      <div v-if="responseText || (sending && stream)" class="space-y-2">
        <div class="relative bg-white rounded-md border border-gray-200 p-4">
          <pre
            class="text-sm text-gray-800 whitespace-pre-wrap font-sans leading-relaxed min-h-[2rem]"
          >{{ responseText }}<span v-if="sending && stream" class="inline-block w-2 h-4 bg-indigo-500 animate-pulse ml-0.5 align-text-bottom" /></pre>

          <button
            v-if="responseText && !sending"
            class="absolute top-2 right-2 text-xs text-gray-400 hover:text-gray-600"
            title="Copy response"
            @click="copy"
          >
            {{ copied ? 'Copied!' : 'Copy' }}
          </button>
        </div>

        <!-- Stats -->
        <div v-if="stats" class="flex gap-4 text-xs text-gray-500">
          <span v-if="stats.promptTokens">In: {{ stats.promptTokens }} tok</span>
          <span v-if="stats.completionTokens">Out: {{ stats.completionTokens }} tok</span>
          <span v-if="stats.totalTokens">Total: {{ stats.totalTokens }} tok</span>
          <span v-if="stats.latencyMs">{{ stats.latencyMs }}ms</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { api } from '../api/client.js'
import ErrorAlert from './ErrorAlert.vue'

const props = defineProps({
  modelName: { type: String, required: true },
})

const systemMessage = ref('')
const userMessage = ref('')
const temperature = ref(1.0)
const stream = ref(true)

const sending = ref(false)
const responseText = ref('')
const stats = ref(null)
const error = ref('')
const copied = ref(false)

let abortController = null

function buildMessages() {
  const msgs = []
  if (systemMessage.value.trim()) {
    msgs.push({ role: 'system', content: systemMessage.value.trim() })
  }
  msgs.push({ role: 'user', content: userMessage.value.trim() })
  return msgs
}

async function send() {
  if (sending.value || !userMessage.value.trim()) return

  error.value = ''
  responseText.value = ''
  stats.value = null
  sending.value = true

  const messages = buildMessages()
  const options = { temperature: temperature.value }
  const t0 = Date.now()

  try {
    if (stream.value) {
      await sendStreaming(messages, options, t0)
    } else {
      await sendBlocking(messages, options, t0)
    }
  } catch (e) {
    if (e.name !== 'AbortError') {
      error.value = e.message
    }
  } finally {
    sending.value = false
    abortController = null
  }
}

async function sendBlocking(messages, options, t0) {
  const resp = await api.chatCompletion(props.modelName, messages, options)
  responseText.value = resp.choices?.[0]?.message?.content ?? ''
  const u = resp.usage
  if (u) {
    stats.value = {
      promptTokens: u.prompt_tokens,
      completionTokens: u.completion_tokens,
      totalTokens: u.total_tokens,
      latencyMs: Date.now() - t0,
    }
  }
}

async function sendStreaming(messages, options, t0) {
  abortController = new AbortController()
  const body = await api.chatCompletionStream(props.modelName, messages, {
    ...options,
    signal: abortController.signal,
  })

  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() ?? ''

      for (const line of lines) {
        const trimmed = line.trim()
        if (!trimmed.startsWith('data: ')) continue
        const data = trimmed.slice(6)
        if (data === '[DONE]') {
          stats.value = { latencyMs: Date.now() - t0 }
          return
        }
        try {
          const chunk = JSON.parse(data)
          const content = chunk.choices?.[0]?.delta?.content
          if (content) responseText.value += content
        } catch { /* skip malformed chunk */ }
      }
    }
  } finally {
    reader.releaseLock()
  }
}

function abort() {
  abortController?.abort()
}

async function copy() {
  try {
    await navigator.clipboard.writeText(responseText.value)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch { /* ignore */ }
}
</script>
