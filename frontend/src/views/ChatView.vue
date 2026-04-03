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
      <div v-else-if="availableModels.length === 0" class="text-sm text-red-500">
        No models available — check your connection and API key
      </div>
      <div v-else class="space-y-2">
        <!-- Selected model chips -->
        <div v-if="selectedModels.length > 0" class="flex flex-wrap gap-2">
          <span
            v-for="model in selectedModels"
            :key="model"
            class="inline-flex items-center gap-1 text-xs rounded-full pl-3 pr-1.5 py-1 bg-indigo-600 text-white"
          >
            {{ model }}
            <button
              class="w-4 h-4 rounded-full hover:bg-indigo-500 flex items-center justify-center"
              :disabled="anyStreaming"
              @click="toggleModel(model)"
              title="Remove model"
            >
              <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </span>
        </div>
        <!-- Search dropdown -->
        <div v-if="selectedModels.length < 4" class="relative" ref="dropdownRef">
          <input
            v-model="modelSearch"
            type="text"
            class="input text-sm w-full max-w-sm"
            :placeholder="selectedModels.length === 0 ? 'Search and select models…' : 'Add another model…'"
            :disabled="anyStreaming"
            @focus="dropdownOpen = true"
            @input="dropdownOpen = true"
            @keydown.escape="dropdownOpen = false"
            @keydown.enter.prevent="selectFirstMatch"
          />
          <div
            v-if="dropdownOpen && filteredModels.length > 0"
            class="absolute z-10 mt-1 w-full max-w-sm bg-white border border-gray-200 rounded-md shadow-lg max-h-48 overflow-y-auto"
          >
            <button
              v-for="model in filteredModels"
              :key="model"
              class="w-full text-left px-3 py-2 text-sm text-gray-700 hover:bg-indigo-50 hover:text-indigo-700 transition-colors"
              @mousedown.prevent="toggleModel(model); modelSearch = ''"
            >
              {{ model }}
            </button>
          </div>
          <div
            v-if="dropdownOpen && modelSearch && filteredModels.length === 0"
            class="absolute z-10 mt-1 w-full max-w-sm bg-white border border-gray-200 rounded-md shadow-lg px-3 py-2 text-sm text-gray-400"
          >
            No matching models
          </div>
        </div>
        <p v-else class="text-xs text-gray-400">Maximum 4 models selected</p>
      </div>

      <!-- API key selector -->
      <div class="flex items-center gap-3">
        <label class="text-xs text-gray-600 whitespace-nowrap">Charge to:</label>
        <select
          v-model="selectedKeyId"
          class="input text-sm max-w-sm"
          :disabled="anyStreaming || loadingKeys"
        >
          <option v-if="currentUser?.is_admin" :value="null">Master Key</option>
          <option
            v-for="key in accessibleKeys"
            :key="key.id"
            :value="key.id"
          >
            {{ key.team_name }} / {{ key.app_name }} / {{ key.name }} ({{ key.key_prefix }}…)
          </option>
        </select>
        <span v-if="loadingKeys" class="text-xs text-gray-400">Loading keys…</span>
        <span v-else-if="!currentUser?.is_admin && accessibleKeys.length === 0" class="text-xs text-red-500">
          No API keys available — ask your team admin to create one
        </span>
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
          :charge-key-id="selectedKeyId"
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
            :disabled="anyStreaming || selectedModels.length === 0 || !hasValidKey"
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
            :disabled="anyStreaming || !userInput.trim() || selectedModels.length === 0 || !hasValidKey"
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
import { ref, reactive, computed, onMounted, onBeforeUnmount } from 'vue'
import { api } from '../api/client.js'
import { useSession } from '../composables/useSession.js'
import ChatPanel from '../components/ChatPanel.vue'

const { currentUser } = useSession()

// ── Model list ─────────────────────────────────────────────────────────────
const availableModels = ref([])
const loadingModels = ref(true)

// ── API key selection ──────────────────────────────────────────────────────
const accessibleKeys = ref([])
const loadingKeys = ref(true)
const selectedKeyId = ref(null) // null = master key (admin default)

onMounted(async () => {
  // Fetch models and keys in parallel
  const [modelsResult, keysResult] = await Promise.allSettled([
    api.models(),
    api.myKeys(),
  ])

  if (modelsResult.status === 'fulfilled' && modelsResult.value) {
    availableModels.value = (modelsResult.value.data ?? []).map(m => m.id).sort((a, b) => a.localeCompare(b))
  }
  loadingModels.value = false

  if (keysResult.status === 'fulfilled' && keysResult.value) {
    accessibleKeys.value = keysResult.value
  }
  loadingKeys.value = false

  // Non-admin users: default to their first key (they can't use master key)
  if (!currentUser.value?.is_admin && accessibleKeys.value.length > 0) {
    selectedKeyId.value = accessibleKeys.value[0].id
  }

  document.addEventListener('click', handleClickOutside)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
})

// ── Model search / dropdown ────────────────────────────────────────────────
const modelSearch = ref('')
const dropdownOpen = ref(false)
const dropdownRef = ref(null)

const filteredModels = computed(() => {
  const q = modelSearch.value.toLowerCase()
  return availableModels.value.filter(m =>
    !selectedModels.value.includes(m) && m.toLowerCase().includes(q)
  )
})

function selectFirstMatch() {
  if (filteredModels.value.length > 0) {
    toggleModel(filteredModels.value[0])
    modelSearch.value = ''
  }
}

function handleClickOutside(e) {
  if (dropdownRef.value && !dropdownRef.value.contains(e.target)) {
    dropdownOpen.value = false
  }
}

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

// Admins can always send (master key); non-admins need a selected key.
const hasValidKey = computed(() =>
  currentUser.value?.is_admin || selectedKeyId.value != null
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
// Exclude the in-flight turn only if THIS model is still streaming (not globally).
function completedTurnsFor(model) {
  const replies = assistantMsgs[model] ?? []
  const isModelStreaming = streamingState[model]
  const limit = isModelStreaming ? currentTurnIndex.value : sharedUserMsgs.value.length
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
