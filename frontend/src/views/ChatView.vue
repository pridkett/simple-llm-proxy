<template>
  <div class="flex flex-col h-[calc(100vh-4rem)] overflow-hidden">
    <!-- Top controls bar -->
    <div class="flex-shrink-0 border-b border-gray-200 bg-white px-4 sm:px-6 lg:px-8 py-3 space-y-3">
      <!-- Heading row -->
      <div class="flex items-center justify-between">
        <div>
          <h1 class="text-xl font-semibold text-gray-900">Chat</h1>
          <p class="text-xs text-gray-500 mt-0.5">Test models and compare responses side by side</p>
        </div>
        <button
          v-if="sharedUserMsgs.length > 0"
          class="btn-secondary text-sm"
          :disabled="anyStreaming"
          @click="clearAll"
        >
          Clear
        </button>
      </div>

      <!-- Model selector -->
      <div v-if="loadingModels" class="text-sm text-gray-400">Loading models…</div>
      <div v-else-if="availableModels.length === 0" class="text-sm text-red-500 text-sm">
        No models available — check your connection and API key
      </div>
      <div v-else class="flex flex-wrap gap-2">
        <button
          v-for="model in availableModels"
          :key="model"
          class="text-xs rounded-full px-3 py-1.5 border transition-colors"
          :class="selectedModels.includes(model)
            ? 'bg-indigo-600 text-white border-indigo-600'
            : selectedModels.length >= 4
              ? 'bg-white text-gray-400 border-gray-200 cursor-not-allowed'
              : 'bg-white text-gray-700 border-gray-300 hover:bg-gray-50'"
          @click="toggleModel(model)"
        >
          {{ model }}
        </button>
      </div>

      <!-- Advanced options (collapsible) -->
      <details class="group">
        <summary class="text-xs text-gray-500 cursor-pointer select-none list-none flex items-center gap-1 hover:text-gray-700 w-fit">
          <svg class="w-3 h-3 transition-transform group-open:rotate-90" fill="currentColor" viewBox="0 0 20 20">
            <path d="M6 6l8 4-8 4V6z" />
          </svg>
          Advanced options
        </summary>
        <div class="mt-2 flex flex-wrap items-start gap-4">
          <div class="flex-1 min-w-[200px]">
            <label class="block text-xs text-gray-600 mb-1">System message (optional)</label>
            <textarea
              v-model="systemMessage"
              class="input resize-y text-sm font-mono"
              rows="2"
              placeholder="You are a helpful assistant."
              :disabled="anyStreaming"
            />
          </div>
          <div>
            <label class="block text-xs text-gray-600 mb-1">Temperature: {{ temperature.toFixed(1) }}</label>
            <input
              type="range"
              v-model.number="temperature"
              min="0" max="2" step="0.1"
              class="w-32 accent-indigo-600 block mt-2"
              :disabled="anyStreaming"
            />
          </div>
        </div>
      </details>
    </div>

    <!-- Main area: panels or empty state -->
    <div class="flex-1 min-h-0 overflow-hidden">
      <div
        v-if="selectedModels.length === 0"
        class="h-full flex items-center justify-center"
      >
        <div class="text-center">
          <svg class="w-12 h-12 text-gray-300 mx-auto mb-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
              d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
          </svg>
          <p class="text-sm text-gray-500">Select one or more models above to start chatting</p>
        </div>
      </div>

      <div
        v-else
        class="h-full p-4 gap-4 grid overflow-hidden"
        :class="gridClass"
      >
        <ChatPanel
          v-for="model in selectedModels"
          :key="model"
          :model-name="model"
          :pending-messages="pendingMsgs[model] ?? null"
          :send-trigger="sendTrigger"
          :stop-trigger="stopTrigger"
          :temperature="temperature"
          :turns="completedTurnsFor(model)"
          :current-user-content="currentUserContent"
          :turn-index="currentTurnIndex"
          @turn-complete="onTurnComplete"
          @streaming-change="onStreamingChange"
        />
      </div>
    </div>

    <!-- Shared input bar -->
    <div class="flex-shrink-0 border-t border-gray-200 bg-white px-4 sm:px-6 lg:px-8 py-3">
      <div class="flex gap-3 items-end">
        <div class="flex-1">
          <textarea
            v-model="userInput"
            class="input resize-none text-sm"
            rows="2"
            placeholder="Type your message… (Ctrl+Enter to send)"
            :disabled="anyStreaming || selectedModels.length === 0"
            @keydown.ctrl.enter.prevent="send"
            @keydown.meta.enter.prevent="send"
          />
        </div>
        <div class="flex gap-2 flex-shrink-0">
          <button
            v-if="anyStreaming"
            class="btn-secondary text-sm"
            @click="stopAll"
          >
            Stop All
          </button>
          <button
            class="btn-primary text-sm"
            :disabled="anyStreaming || !userInput.trim() || selectedModels.length === 0"
            @click="send"
          >
            Send
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { api } from '../api/client.js'
import ChatPanel from '../components/ChatPanel.vue'

// ── Model list ─────────────────────────────────────────────────────────────
const availableModels = ref([])
const loadingModels = ref(true)

onMounted(async () => {
  try {
    const data = await api.models()
    availableModels.value = (data.data ?? []).map(m => m.id)
  } catch {
    /* silently ignore; empty state shown */
  } finally {
    loadingModels.value = false
  }
})

// ── Model selection ────────────────────────────────────────────────────────
const selectedModels = ref([])

function toggleModel(model) {
  const idx = selectedModels.value.indexOf(model)
  if (idx >= 0) {
    selectedModels.value.splice(idx, 1)
    // Reset history for this model so it starts fresh if re-added later
    delete assistantMsgs[model]
  } else if (selectedModels.value.length < 4) {
    selectedModels.value.push(model)
    assistantMsgs[model] = []
  }
}

// ── Conversation state ─────────────────────────────────────────────────────
const sharedUserMsgs = ref([])    // string[] — user text per completed turn
const assistantMsgs = reactive({}) // Record<model, string[]>
const systemMessage = ref('')
const temperature = ref(1.0)
const userInput = ref('')

// ── Trigger coordination ───────────────────────────────────────────────────
const sendTrigger = ref(0)
const stopTrigger = ref(0)
const pendingMsgs = reactive({}) // Record<model, Message[]> — set before sendTrigger++
const currentTurnIndex = ref(0)  // which turn is in-flight
const currentUserContent = ref('') // user text for the in-flight turn

// ── Streaming state ────────────────────────────────────────────────────────
const streamingState = reactive({})

const anyStreaming = computed(() =>
  selectedModels.value.some(m => streamingState[m])
)

// ── Grid layout ────────────────────────────────────────────────────────────
const gridClass = computed(() => {
  switch (selectedModels.value.length) {
    case 1:  return 'grid-cols-1'
    case 2:  return 'grid-cols-2'
    case 3:  return 'grid-cols-3'
    default: return 'grid-cols-2' // 4 → 2×2
  }
})

// ── History helpers ────────────────────────────────────────────────────────
function buildHistoryForModel(model, turnIndex) {
  const history = []
  if (systemMessage.value.trim()) {
    history.push({ role: 'system', content: systemMessage.value.trim() })
  }
  for (let i = 0; i < turnIndex; i++) {
    history.push({ role: 'user', content: sharedUserMsgs.value[i] })
    const reply = assistantMsgs[model]?.[i]
    if (reply) history.push({ role: 'assistant', content: reply })
  }
  history.push({ role: 'user', content: sharedUserMsgs.value[turnIndex] })
  return history
}

// Returns completed turns for a model as { userContent, assistantContent } pairs.
// During streaming, only include turns before the in-flight one.
function completedTurnsFor(model) {
  const replies = assistantMsgs[model] ?? []
  const limit = anyStreaming.value ? currentTurnIndex.value : sharedUserMsgs.value.length
  return sharedUserMsgs.value.slice(0, limit).map((userContent, i) => ({
    userContent,
    assistantContent: replies[i] ?? '',
  }))
}

// ── Send ───────────────────────────────────────────────────────────────────
function send() {
  if (!userInput.value.trim() || selectedModels.value.length === 0 || anyStreaming.value) return

  const turnIndex = sharedUserMsgs.value.length
  const content = userInput.value.trim()

  // Commit the user message
  sharedUserMsgs.value.push(content)
  currentTurnIndex.value = turnIndex
  currentUserContent.value = content
  userInput.value = ''

  // Pre-allocate assistant slots and build per-model message histories
  for (const model of selectedModels.value) {
    if (!assistantMsgs[model]) assistantMsgs[model] = []
    assistantMsgs[model][turnIndex] = ''
    pendingMsgs[model] = buildHistoryForModel(model, turnIndex)
  }

  // Increment trigger — all panels watching this will fire their stream
  sendTrigger.value++
}

// ── Stop all ───────────────────────────────────────────────────────────────
function stopAll() {
  stopTrigger.value++
}

// ── Event handlers ─────────────────────────────────────────────────────────
function onTurnComplete({ modelName, turnIndex, content }) {
  if (!assistantMsgs[modelName]) assistantMsgs[modelName] = []
  assistantMsgs[modelName][turnIndex] = content
}

function onStreamingChange({ modelName, streaming }) {
  streamingState[modelName] = streaming
}

// ── Clear all ──────────────────────────────────────────────────────────────
function clearAll() {
  sharedUserMsgs.value = []
  currentTurnIndex.value = 0
  currentUserContent.value = ''
  for (const model of selectedModels.value) {
    assistantMsgs[model] = []
  }
}
</script>
