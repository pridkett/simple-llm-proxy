<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
    <div class="flex flex-wrap items-start justify-between gap-3 mb-6">
      <div>
        <h1 class="text-2xl font-semibold">Usage &amp; Cost</h1>
        <p class="text-sm text-gray-500 dark:text-gray-400 mt-1">
          Understand traffic across paid, free, and local models.
        </p>
      </div>
      <button class="btn-secondary text-sm" :disabled="loading" @click="load">
        Refresh
      </button>
    </div>
    <nav
      class="flex gap-5 border-b border-gray-200 dark:border-gray-700 mb-5"
      aria-label="Reporting views"
    >
      <button
        v-for="item in ['overview', 'explore', 'budgets']"
        :key="item"
        type="button"
        :aria-pressed="tab === item"
        class="py-3 border-b-2 capitalize text-sm font-medium"
        :class="
          tab === item
            ? 'border-indigo-500 text-indigo-700 dark:text-indigo-300'
            : 'border-transparent text-gray-500 dark:text-gray-400'
        "
        @click="tab = item"
      >
        {{ item }}
      </button>
    </nav>
    <div class="card p-4 mb-5 flex flex-wrap gap-3 items-end">
      <fieldset>
        <legend class="text-xs text-gray-500 dark:text-gray-400 mb-1">
          Period
        </legend>
        <div class="flex flex-wrap gap-1">
          <button
            v-for="range in ['today', '7d', '30d', 'custom']"
            :key="range"
            type="button"
            class="btn-secondary text-sm px-3"
            :aria-pressed="period === range"
            :class="
              period === range
                ? 'bg-indigo-50 dark:bg-indigo-950 text-indigo-700 dark:text-indigo-300'
                : ''
            "
            @click="selectPeriod(range)"
          >
            {{
              { today: 'Today', '7d': '7d', '30d': '30d', custom: 'Custom' }[
                range
              ]
            }}
          </button>
        </div>
      </fieldset>
      <template v-if="period === 'custom'">
        <label class="text-xs text-gray-500 dark:text-gray-400"
          >From<input
            v-model="customFrom"
            aria-label="From date"
            type="date"
            class="input mt-1"
        /></label>
        <label class="text-xs text-gray-500 dark:text-gray-400"
          >To<input
            v-model="customTo"
            aria-label="To date"
            type="date"
            class="input mt-1"
        /></label>
      </template>
      <label class="text-xs text-gray-500 dark:text-gray-400"
        >Timezone<select
          v-model="timezone"
          class="input mt-1"
          aria-label="Timezone"
        >
          <option v-for="tz in timezones" :key="tz">{{ tz }}</option>
        </select></label
      >
      <label class="text-xs text-gray-500 dark:text-gray-400"
        >Buckets<select
          v-model="interval"
          class="input mt-1"
          aria-label="Buckets"
        >
          <option value="hour">Hourly</option>
          <option value="day">Daily</option>
          <option value="week">Weekly</option>
        </select></label
      >
      <label class="text-xs text-gray-500 dark:text-gray-400"
        >Group by<select
          v-model="groupBy"
          class="input mt-1"
          aria-label="Group by"
        >
          <option value="model">Model</option>
          <option value="app">Application</option>
          <option value="key">Key</option>
          <option value="team">Team</option>
        </select></label
      >
    </div>
    <div class="flex flex-wrap gap-3 items-end mb-5">
      <label class="text-xs text-gray-500 dark:text-gray-400"
        >Team<select
          v-model="filters.team_id"
          class="input mt-1"
          aria-label="Team"
        >
          <option :value="null">All teams</option>
          <option v-for="item in meta.teams" :key="item.id" :value="item.id">
            {{ item.name }} (#{{ item.id }})
          </option>
        </select></label
      >
      <label class="text-xs text-gray-500 dark:text-gray-400"
        >Application<select
          v-model="filters.app_id"
          class="input mt-1"
          aria-label="Application"
        >
          <option :value="null">All applications</option>
          <option v-for="item in meta.apps" :key="item.id" :value="item.id">
            {{ item.name }} (#{{ item.id }})
          </option>
        </select></label
      >
      <label class="text-xs text-gray-500 dark:text-gray-400"
        >Key<select
          v-model="filters.key_id"
          class="input mt-1"
          aria-label="Key"
        >
          <option :value="null">All keys</option>
          <option v-for="item in meta.keys" :key="item.id" :value="item.id">
            {{ item.name }} (#{{ item.id }})
          </option>
        </select></label
      >
      <label class="text-xs text-gray-500 dark:text-gray-400"
        >Model<select
          v-model="filters.model"
          class="input mt-1"
          aria-label="Model"
        >
          <option value="">All models</option>
          <option v-for="name in meta.models" :key="name">{{ name }}</option>
        </select></label
      >
      <button
        v-if="hasFilters"
        class="btn-secondary text-sm"
        @click="clearFilters"
      >
        Clear filters
      </button>
    </div>
    <div
      v-if="selection || filters.unattributed"
      class="flex flex-wrap gap-2 mb-4"
      aria-live="polite"
    >
      <button
        v-if="selection"
        class="btn-secondary text-xs"
        @click="selection = null"
      >
        {{ selection.label }} · {{ timezone }} ×
      </button>
      <button
        v-if="filters.unattributed"
        class="btn-secondary text-xs"
        @click="filters.unattributed = ''"
      >
        Unattributed {{ filters.unattributed }} ×
      </button>
    </div>
    <p
      v-if="validationError"
      role="alert"
      class="mb-4 text-sm text-red-600 dark:text-red-400"
    >
      {{ validationError }}
    </p>
    <LoadingSpinner v-if="loading" />
    <ErrorAlert
      v-else-if="error && !validationError"
      title="Failed to load usage report"
      :message="error"
    />
    <template v-else-if="report && !validationError">
      <div
        class="grid grid-cols-1 sm:grid-cols-3 gap-4 mb-6"
        aria-label="Usage summary"
      >
        <button
          v-for="item in metrics"
          :key="item.key"
          type="button"
          class="card p-5 text-left border-2"
          :class="
            metric === item.key
              ? 'border-indigo-500 dark:border-indigo-400'
              : 'border-transparent'
          "
          :aria-pressed="metric === item.key"
          :data-testid="`summary-${item.key}`"
          @click="metric = item.key"
        >
          <span class="text-sm text-gray-500 dark:text-gray-400">{{
            item.label
          }}</span
          ><span class="block text-2xl font-semibold tabular-nums mt-2">{{
            display(report.summary, item.field)
          }}</span>
          <span class="block text-xs text-gray-500 dark:text-gray-400 mt-2">{{
            item.key === 'calls'
              ? `${formatNumber(report.summary.failed_calls)} failed · finalized logged requests`
              : coverage(report.summary, item.field)
          }}</span>
        </button>
      </div>
      <p class="text-xs text-gray-500 dark:text-gray-400 mb-5">
        {{ report.from }} – {{ report.to }} · {{ report.timezone }} ·
        {{ report.summary.upstream_cost_calls }} reported /
        {{ report.summary.estimated_cost_calls }} estimated /
        {{ report.summary.mixed_cost_calls }} mixed costs<span
          v-if="report.summary.legacy_calls"
        >
          · {{ report.summary.legacy_calls }} legacy calls</span
        >
      </p>
      <div
        v-if="report.alerts?.length"
        class="space-y-2 mb-5"
        aria-label="Budget alerts"
      >
        <div
          v-for="alert in report.alerts"
          :key="alert.key_id"
          class="p-3 rounded-md bg-amber-50 dark:bg-amber-950 border border-amber-200 dark:border-amber-800 text-sm text-amber-900 dark:text-amber-200 flex flex-wrap gap-2 justify-between"
        >
          <span
            ><strong>{{ alert.key_name }}</strong> · {{ alert.app_name }} ·
            {{ formatMoney(alert.total_spend) }} in this window ·
            {{
              alert.alert_type === 'hard'
                ? 'hard budget reached'
                : 'soft budget reached'
            }}</span
          ><button class="underline" @click="tab = 'budgets'">
            View budgets
          </button>
        </div>
      </div>
      <UsageBudgets v-if="tab === 'budgets'" :rows="report.budget_rows" />
      <template v-else>
        <section
          v-if="tab === 'overview'"
          class="card p-4 sm:p-6 mb-5"
          aria-label="Usage trend"
        >
          <div class="flex flex-wrap items-center justify-between gap-3 mb-3">
            <h2 class="font-semibold">{{ metricLabel }} over time</h2>
            <button
              class="text-sm text-indigo-700 dark:text-indigo-300"
              @click="tab = 'explore'"
            >
              Explore →
            </button>
          </div>
          <p
            v-if="!report.summary.calls"
            class="py-12 text-center text-sm text-gray-500 dark:text-gray-400"
          >
            No requests were recorded in this date range. Try expanding the
            period or clearing filters.
          </p>
          <apexchart
            v-else
            :key="`${isDark}-${metric}-${groupBy}`"
            type="bar"
            height="300"
            :options="chartOptions"
            :series="chartSeries"
          />
          <label
            v-if="report.summary.calls"
            class="text-xs text-gray-500 dark:text-gray-400 flex flex-wrap items-center gap-2"
            >Explore a time bucket<select
              class="input w-auto max-w-full"
              aria-label="Explore time bucket"
              :value="''"
              @change="selectBucket(Number($event.target.value))"
            >
              <option value="" disabled>Select a bucket…</option>
              <option
                v-for="(bucket, index) in report.buckets"
                :key="bucket.start"
                :value="index"
              >
                {{ bucket.label }}
              </option>
            </select></label
          >
        </section>
        <section class="card overflow-hidden" aria-label="Usage breakdown">
          <div
            class="p-5 flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 dark:border-gray-700"
          >
            <h2 class="font-semibold">
              {{ tab === 'explore' ? 'Explore' : 'Usage' }} by
              {{ groupLabel.toLowerCase() }}
            </h2>
            <div class="flex flex-wrap items-center gap-3">
              <label class="text-xs text-gray-500 dark:text-gray-400"
                >Sort by<select
                  v-model="sortKey"
                  aria-label="Sort by"
                  class="input mt-1"
                >
                  <option value="calls">Calls</option>
                  <option value="total_tokens">Known tokens</option>
                  <option value="total_spend">Known spend</option>
                </select></label
              ><button
                class="btn-secondary text-xs"
                :aria-label="descending ? 'Sort ascending' : 'Sort descending'"
                @click="descending = !descending"
              >
                {{ descending ? '↓ Descending' : '↑ Ascending' }}</button
              ><RouterLink :to="logsLink()" class="btn-secondary text-xs"
                >View requests</RouterLink
              >
            </div>
          </div>
          <div class="overflow-x-auto">
            <table class="w-full text-sm">
              <thead>
                <tr
                  class="text-xs text-gray-500 dark:text-gray-400 border-b border-gray-100 dark:border-gray-700"
                >
                  <th class="px-5 py-3 text-left">{{ groupLabel }}</th>
                  <th
                    v-for="col in columns"
                    :key="col.field"
                    class="px-4 py-3 text-right whitespace-nowrap"
                  >
                    {{ col.label }}
                  </th>
                  <th v-if="tab === 'overview'" class="px-4 py-3 text-right">
                    Share of {{ metric }}
                  </th>
                  <th class="px-4 py-3 text-right">Requests</th>
                </tr>
              </thead>
              <tbody>
                <tr v-if="!sortedGroups.length">
                  <td
                    :colspan="columns.length + 3"
                    class="p-10 text-center text-gray-500 dark:text-gray-400"
                  >
                    No requests match this period and these filters.
                  </td>
                </tr>
                <tr
                  v-for="row in sortedGroups"
                  :key="row.group_id"
                  data-testid="usage-row"
                  class="border-b border-gray-50 dark:border-gray-700 hover:bg-gray-50 dark:hover:bg-gray-900"
                >
                  <td class="px-5 py-3">
                    <button
                      class="text-left text-indigo-700 dark:text-indigo-300 font-medium break-words"
                      @click="selectGroup(row)"
                    >
                      {{ row.name }}</button
                    ><span
                      v-if="groupBy !== 'model' && row.group_id"
                      class="block text-xs text-gray-500 dark:text-gray-400"
                      >#{{ row.group_id }}</span
                    >
                  </td>
                  <td
                    v-for="col in columns"
                    :key="col.field"
                    class="px-4 py-3 text-right tabular-nums whitespace-nowrap"
                  >
                    <span>{{ display(row, col.field) }}</span
                    ><span
                      v-if="isPartial(row, col.field)"
                      class="block text-xs text-gray-500 dark:text-gray-400"
                      >Known subtotal</span
                    >
                  </td>
                  <td
                    v-if="tab === 'overview'"
                    class="px-4 py-3 text-right tabular-nums"
                  >
                    {{ share(row) }}
                  </td>
                  <td class="px-4 py-3 text-right">
                    <RouterLink
                      :to="logsLink(row)"
                      class="text-indigo-700 dark:text-indigo-300 text-xs whitespace-nowrap"
                      :aria-label="`View requests for ${row.name}`"
                      >View →</RouterLink
                    >
                  </td>
                </tr>
              </tbody>
              <tfoot
                v-if="sortedGroups.length"
                class="bg-gray-50 dark:bg-gray-900 font-medium"
              >
                <tr data-testid="usage-totals">
                  <td class="px-5 py-3">Total</td>
                  <td
                    v-for="col in columns"
                    :key="col.field"
                    class="px-4 py-3 text-right tabular-nums whitespace-nowrap"
                  >
                    {{ display(report.summary, col.field) }}
                  </td>
                  <td v-if="tab === 'overview'"></td>
                  <td></td>
                </tr>
              </tfoot>
            </table>
          </div>
          <p
            class="p-4 text-xs text-gray-500 dark:text-gray-400 border-t border-gray-100 dark:border-gray-700"
            data-testid="estimate-note"
          >
            Spend uses upstream-reported amounts when available, otherwise
            recorded estimates. Unknown costs are excluded; partial amounts are
            known subtotals. Recorded $0 is distinct from unknown. Cache and
            reasoning counts are subsets, not extra tokens. Some providers
            report totals without a breakdown. Legacy Anthropic input is
            normalized for reporting; historical costs are not repriced.
          </p>
        </section>
      </template>
      <p class="mt-4 text-xs text-gray-500 dark:text-gray-400">
        {{
          report.retention_days > 0
            ? `Reports use retained logs (${report.retention_days}-day retention). Older activity may have been deleted.`
            : 'Reports use available retained logs.'
        }}
        Calls include finalized logged failures; retries and job polls are not
        additional calls.
      </p>
    </template>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '../api/client.js'
import { useColorScheme } from '../composables/useColorScheme.js'
import LoadingSpinner from '../components/LoadingSpinner.vue'
import ErrorAlert from '../components/ErrorAlert.vue'
import UsageBudgets from '../components/UsageBudgets.vue'
import {
  addMetrics,
  dateRange,
  formatMoney,
  formatNumber,
  knownFields,
  measurement,
  metricFields,
  sortGroups
} from '../utils/usage.js'

const { isDark } = useColorScheme()
const tab = ref('overview'),
  period = ref('7d'),
  interval = ref('day'),
  groupBy = ref('model')
const browserTimezone =
  Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
const timezone = ref(browserTimezone)
const timezones = [
  ...new Set([
    browserTimezone,
    'UTC',
    ...(Intl.supportedValuesOf
      ? Intl.supportedValuesOf('timeZone')
      : ['America/New_York', 'Europe/London', 'Asia/Tokyo'])
  ])
]
const customFrom = ref(''),
  customTo = ref('')
const filters = ref({
  team_id: null,
  app_id: null,
  key_id: null,
  model: '',
  unattributed: ''
})
const selection = ref(null),
  report = ref(null),
  loading = ref(false),
  error = ref('')
const meta = ref({ teams: [], apps: [], keys: [], models: [] })
let storedMetric
try {
  storedMetric = localStorage.getItem('usage-chart-metric')
} catch {
  /* preferences are optional */
}
const metric = ref(
  ['calls', 'tokens', 'spend'].includes(storedMetric) ? storedMetric : 'calls'
)
const sortKey = ref(metricFields[metric.value]),
  descending = ref(true)
const metrics = [
  { key: 'calls', label: 'Calls', field: 'calls' },
  { key: 'tokens', label: 'Known tokens', field: 'total_tokens' },
  { key: 'spend', label: 'Dollar spend', field: 'total_spend' }
]
const metricLabel = computed(
  () =>
    ({ calls: 'Calls', tokens: 'Known tokens', spend: 'Known spend' })[
      metric.value
    ]
)
const groupLabel = computed(
  () =>
    ({ model: 'Model', app: 'Application', key: 'Key', team: 'Team' })[
      groupBy.value
    ]
)
const bounds = computed(() =>
  period.value === 'custom'
    ? { from: customFrom.value, to: customTo.value }
    : dateRange(period.value, timezone.value)
)
const validationError = computed(() => {
  if (!bounds.value.from || !bounds.value.to)
    return 'Choose both a start and end date.'
  if (bounds.value.from > bounds.value.to)
    return 'The start date must be on or before the end date.'
  return ''
})
const params = computed(() => ({
  ...bounds.value,
  ...filters.value,
  timezone: timezone.value,
  interval: interval.value,
  group_by: groupBy.value,
  ...(selection.value
    ? { since: selection.value.start, until: selection.value.end }
    : {})
}))
const hasFilters = computed(
  () => Object.values(filters.value).some(Boolean) || selection.value
)
const sortedGroups = computed(() =>
  sortGroups(report.value?.groups || [], sortKey.value, descending.value)
)
const columns = computed(() => [
  { field: 'calls', label: 'Calls' },
  { field: 'total_tokens', label: 'Known tokens' },
  ...(tab.value === 'explore'
    ? [
        { field: 'input_tokens', label: 'Input' },
        { field: 'output_tokens', label: 'Output' },
        { field: 'cache_read_tokens', label: 'Cache read' },
        { field: 'cache_write_tokens', label: 'Cache write' },
        { field: 'reasoning_tokens', label: 'Reasoning' }
      ]
    : []),
  { field: 'total_spend', label: 'Dollar spend' }
])
function display(row, field) {
  const value = measurement(row, field)
  return field === 'total_spend' ? formatMoney(value) : formatNumber(value)
}
function isPartial(row, field) {
  const key = knownFields[field]
  return (
    key &&
    row[key] > 0 &&
    (row[key] < row.calls ||
      (field === 'total_spend' && row.partial_cost_calls > 0))
  )
}
function coverage(row, field) {
  if (!row.calls) return 'No calls in this period'
  const count = row[knownFields[field]] || 0
  const label = `${formatNumber(count)} / ${formatNumber(row.calls)} calls measured`
  return `${label}${isPartial(row, field) ? ' · known subtotal' : ''}${field === 'total_spend' && row.partial_cost_calls ? ` · ${row.partial_cost_calls} partial` : ''}`
}
function share(row) {
  const value = measurement(row, metricFields[metric.value]),
    total = measurement(report.value.summary, metricFields[metric.value])
  return value == null || total == null || total === 0
    ? '—'
    : `${((value / total) * 100).toFixed(1)}%`
}
function selectPeriod(value) {
  selection.value = null
  if (value === 'custom' && !customFrom.value) {
    customFrom.value = bounds.value.from
    customTo.value = bounds.value.to
  }
  period.value = value
}
function clearFilters() {
  filters.value = {
    team_id: null,
    app_id: null,
    key_id: null,
    model: '',
    unattributed: ''
  }
  selection.value = null
}
function selectGroup(row) {
  if (groupBy.value === 'model') filters.value.model = row.group_id
  else if (row.group_id)
    filters.value[`${groupBy.value}_id`] = Number(row.group_id)
  else filters.value.unattributed = groupBy.value
  tab.value = 'explore'
}
function selectBucket(index) {
  if (Number.isInteger(index) && report.value?.buckets[index]) {
    selection.value = report.value.buckets[index]
    tab.value = 'explore'
  }
}
function logsLink(row) {
  const query = {
    date_from: report.value?.from_time,
    date_before: report.value?.to_time,
    ...filters.value
  }
  if (row) {
    if (groupBy.value === 'model') query.model = row.group_id
    else if (row.group_id) query[`${groupBy.value}_id`] = row.group_id
    else query.unattributed = groupBy.value
  }
  for (const key of Object.keys(query)) if (!query[key]) delete query[key]
  return { path: '/logs', query }
}
const palette = computed(() =>
  isDark.value
    ? [
        '#a5b4fc',
        '#67e8f9',
        '#facc15',
        '#f0abfc',
        '#86efac',
        '#fda4af',
        '#fdba74',
        '#c4b5fd'
      ]
    : [
        '#4f46e5',
        '#0e7490',
        '#a16207',
        '#a21caf',
        '#15803d',
        '#be123c',
        '#c2410c',
        '#7c3aed'
      ]
)
// Keep category colors stable as metrics reorder the series.
const colorAssignments = new Map()
function groupColor(group) {
  if (group.other) return isDark.value ? '#94a3b8' : '#64748b'
  const id = `${groupBy.value}:${group.group_id}`
  if (!colorAssignments.has(id)) colorAssignments.set(id, colorAssignments.size)
  const index = colorAssignments.get(id)
  return palette.value[index] || `hsl(${(index * 137.508) % 360}, 65%, ${isDark.value ? 70 : 40}%)`
}

const chartGroups = computed(() => {
  const rows = sortGroups(
    report.value?.groups || [],
    metricFields[metric.value]
  )
  return [
    ...rows
      .slice(0, 8)
      .map((row) => ({ ...row, ids: new Set([row.group_id]) })),
    ...(rows.length > 8
      ? [
          {
            group_id: '__other',
            name: 'Other',
            ids: new Set(rows.slice(8).map((row) => row.group_id)),
            other: true
          }
        ]
      : [])
  ]
})
const bucketMeasurements = computed(() => {
  const buckets = new Map()
  for (const row of report.value?.series || []) {
    if (!buckets.has(row.bucket)) buckets.set(row.bucket, new Map())
    buckets.get(row.bucket).set(row.group_id, row)
  }
  return buckets
})
const chartSeries = computed(() =>
  chartGroups.value.map((group) => ({
    name:
      group.name +
      (groupBy.value !== 'model' && group.group_id && !group.other
        ? ` (#${group.group_id})`
        : ''),
    data: (report.value?.buckets || []).map((_, bucket) =>
      measurement(
        addMetrics(
          [...group.ids]
            .map((id) => bucketMeasurements.value.get(bucket)?.get(id))
            .filter(Boolean)
        ),
        metricFields[metric.value]
      )
    )
  }))
)
const chartOptions = computed(() => ({
  chart: {
    type: 'bar',
    stacked: true,
    // Render complete stacks immediately, including on metric/filter changes.
    animations: {
      enabled: false,
      animateGradually: { enabled: false },
      dynamicAnimation: { enabled: false }
    },
    background: 'transparent',
    foreColor: isDark.value ? '#d1d5db' : '#4b5563',
    toolbar: { show: false },
    events: {
      dataPointSelection: (_event, _context, config) =>
        selectBucket(config.dataPointIndex)
    }
  },
  theme: { mode: isDark.value ? 'dark' : 'light' },
  colors: chartGroups.value.map((row) => groupColor(row)),
  plotOptions: { bar: { columnWidth: '65%', borderRadius: 2 } },
  xaxis: {
    categories: (report.value?.buckets || []).map((b) => b.label),
    tickAmount: 7,
    labels: { hideOverlappingLabels: true, rotate: -30 }
  },
  yaxis: {
    min: 0,
    title: {
      text:
        metric.value === 'spend' ? 'USD (known subtotal)' : metricLabel.value
    },
    labels: {
      formatter: (value) =>
        metric.value === 'spend'
          ? formatMoney(value)
          : Math.round(value).toLocaleString()
    }
  },
  grid: { borderColor: isDark.value ? '#374151' : '#e5e7eb' },
  tooltip: {
    theme: isDark.value ? 'dark' : 'light',
    shared: true,
    intersect: false,
    y: {
      formatter: (value) =>
        value == null
          ? 'Unknown'
          : metric.value === 'spend'
            ? formatMoney(value)
            : formatNumber(value)
    }
  },
  dataLabels: { enabled: false },
  legend: { position: 'bottom' },
  noData: { text: 'No known measurements in this period.' }
}))
let sequence = 0,
  debounce
async function load() {
  clearTimeout(debounce)
  const request = ++sequence
  if (validationError.value) {
    loading.value = false
    return
  }
  loading.value = true
  error.value = ''
  try {
    const data = await api.usage(params.value)
    if (request === sequence) report.value = data
  } catch (e) {
    if (request === sequence)
      error.value = e?.message || 'Failed to load usage report'
  } finally {
    if (request === sequence) loading.value = false
  }
}
watch(metric, (value) => {
  sortKey.value = metricFields[value]
  try {
    localStorage.setItem('usage-chart-metric', value)
  } catch {
    /* optional */
  }
})
watch([period, customFrom, customTo, timezone, interval], () => {
  selection.value = null
})
watch(
  params,
  () => {
    sequence++
    loading.value = true
    clearTimeout(debounce)
    debounce = setTimeout(load, 150)
  },
  { deep: true }
)
onMounted(() => {
  load()
  api
    .logsMeta()
    .then((data) => {
      if (data) meta.value = { ...meta.value, ...data }
    })
    .catch(() => {
      /* Reporting remains usable if filter metadata fails. */
    })
})
onBeforeUnmount(() => {
  sequence++
  clearTimeout(debounce)
})
</script>
