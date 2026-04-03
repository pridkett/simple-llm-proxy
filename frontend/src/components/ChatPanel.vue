<template>
  <div class="flex flex-col h-full min-h-0 border border-gray-200 rounded-lg bg-white overflow-hidden">
    <!-- Panel header -->
    <div class="flex items-center justify-between px-4 py-2.5 border-b border-gray-100 bg-gray-50 flex-shrink-0">
      <span class="text-sm font-semibold text-gray-800 truncate">{{ modelName }}</span>
      <div class="flex items-center gap-2 flex-shrink-0">
        <span v-if="isStreaming" class="text-xs text-indigo-600 flex items-center gap-1">
          <span class="inline-block w-1.5 h-1.5 rounded-full bg-indigo-500 animate-pulse" />
          Streaming
        </span>
        <button
          v-if="isStreaming"
          class="btn-secondary text-xs py-0.5 px-2"
          @click="abort"
        >
          Stop
        </button>
      </div>
    </div>

    <!-- Message list -->
    <div ref="messageContainer" class="flex-1 overflow-y-auto p-4 space-y-4 min-h-0">
      <!-- Empty state -->
      <div v-if="turns.length === 0 && !isStreaming" class="h-full flex items-center justify-center">
        <p class="text-sm text-gray-400">Send a message to start</p>
      </div>

      <!-- Completed turns -->
      <template v-for="(turn, i) in turns" :key="i">
        <div class="flex justify-end">
          <div class="max-w-[85%] bg-indigo-50 border border-indigo-100 rounded-lg px-3 py-2">
            <pre class="text-sm text-gray-800 whitespace-pre-wrap font-sans leading-relaxed">{{ turn.userContent }}</pre>
          </div>
        </div>
        <div class="flex justify-start">
          <div class="max-w-[85%] bg-white border border-gray-200 rounded-lg px-3 py-2 shadow-sm">
            <pre class="text-sm text-gray-800 whitespace-pre-wrap font-sans leading-relaxed">{{ turn.assistantContent || '…' }}</pre>
            <!-- Stats on last completed turn -->
            <div v-if="i === turns.length - 1 && lastStats && !isStreaming" class="mt-2 flex gap-3 text-xs text-gray-400 border-t border-gray-100 pt-1.5">
              <span v-if="lastStats.promptTokens">In: {{ lastStats.promptTokens }}</span>
              <span v-if="lastStats.completionTokens">Out: {{ lastStats.completionTokens }}</span>
              <span v-if="lastStats.latencyMs">{{ lastStats.latencyMs }}ms</span>
            </div>
          </div>
        </div>
      </template>

      <!-- Streaming turn (in progress) -->
      <template v-if="isStreaming">
        <div class="flex justify-end">
          <div class="max-w-[85%] bg-indigo-50 border border-indigo-100 rounded-lg px-3 py-2">
            <pre class="text-sm text-gray-800 whitespace-pre-wrap font-sans leading-relaxed">{{ currentUserContent }}</pre>
          </div>
        </div>
        <div class="flex justify-start">
          <div class="max-w-[85%] bg-white border border-gray-200 rounded-lg px-3 py-2 shadow-sm">
            <pre class="text-sm text-gray-800 whitespace-pre-wrap font-sans leading-relaxed min-h-[1.5rem]">{{ streamingText }}<span class="inline-block w-2 h-4 bg-indigo-500 animate-pulse ml-0.5 align-text-bottom" /></pre>
          </div>
        </div>
      </template>

      <!-- Error -->
      <ErrorAlert v-if="localError" :title="localError" />
    </div>
  </div>
</template>

<script setup>
import { ref, watch, nextTick } from 'vue'
import { api } from '../api/client.js'
import ErrorAlert from './ErrorAlert.vue'

const props = defineProps({
  modelName:          { type: String,  required: true },
  pendingMessages:    { type: Array,   default: null },
  sendTrigger:        { type: Number,  default: 0 },
  stopTrigger:        { type: Number,  default: 0 },
  temperature:        { type: Number,  default: 1.0 },
  turns:              { type: Array,   default: () => [] },
  currentUserContent: { type: String,  default: '' },
  turnIndex:          { type: Number,  default: 0 },
  chargeKeyId:        { type: Number,  default: null },
})

const emit = defineEmits(['turn-complete', 'streaming-change', 'error'])

const isStreaming = ref(false)
const streamingText = ref('')
const localError = ref('')
const lastStats = ref(null)
const messageContainer = ref(null)

let abortController = null

function scrollToBottom() {
  nextTick(() => {
    if (messageContainer.value) {
      messageContainer.value.scrollTop = messageContainer.value.scrollHeight
    }
  })
}

// Fire streaming when parent increments sendTrigger
watch(() => props.sendTrigger, (newVal) => {
  if (newVal === 0 || !props.pendingMessages) return
  runStreaming()
})

// Abort when parent increments stopTrigger
watch(() => props.stopTrigger, () => {
  abort()
})

async function runStreaming() {
  localError.value = ''
  streamingText.value = ''
  lastStats.value = null
  isStreaming.value = true
  emit('streaming-change', { modelName: props.modelName, streaming: true })

  const t0 = Date.now()
  abortController = new AbortController()

  try {
    const body = await api.chatCompletionStream(props.modelName, props.pendingMessages, {
      temperature: props.temperature,
      signal: abortController.signal,
      chargeKeyId: props.chargeKeyId,
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
            lastStats.value = { latencyMs: Date.now() - t0 }
            emit('turn-complete', {
              modelName: props.modelName,
              turnIndex: props.turnIndex,
              content: streamingText.value,
              stats: lastStats.value,
            })
            return
          }
          try {
            const chunk = JSON.parse(data)
            const content = chunk.choices?.[0]?.delta?.content
            if (content) {
              streamingText.value += content
              scrollToBottom()
              // Yield to browser so Vue renders each chunk visibly.
              // Without this, providers that send all SSE events at once
              // (e.g. OpenRouter-proxied models) appear to load in bulk.
              await new Promise(resolve => setTimeout(resolve, 0))
            }
            // Extract usage from the final chunk if available
            const usage = chunk.usage
            if (usage) {
              lastStats.value = {
                promptTokens: usage.prompt_tokens,
                completionTokens: usage.completion_tokens,
                latencyMs: Date.now() - t0,
              }
            }
          } catch { /* skip malformed chunk */ }
        }
      }
    } finally {
      reader.releaseLock()
    }
    // Stream ended without [DONE]
    emit('turn-complete', {
      modelName: props.modelName,
      turnIndex: props.turnIndex,
      content: streamingText.value,
      stats: lastStats.value ?? { latencyMs: Date.now() - t0 },
    })
  } catch (e) {
    if (e.name !== 'AbortError') {
      localError.value = e.message
      emit('error', { modelName: props.modelName, error: e.message })
    }
    emit('turn-complete', {
      modelName: props.modelName,
      turnIndex: props.turnIndex,
      content: streamingText.value,
      stats: lastStats.value ?? { latencyMs: Date.now() - t0 },
    })
  } finally {
    isStreaming.value = false
    abortController = null
    emit('streaming-change', { modelName: props.modelName, streaming: false })
    scrollToBottom()
  }
}

function abort() {
  abortController?.abort()
}
</script>
