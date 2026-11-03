// Measurement coverage travels with every subtotal; missing evidence is never zero.
export const metricFields = {
  calls: 'calls',
  tokens: 'total_tokens',
  spend: 'total_spend'
}
export const knownFields = {
  total_tokens: 'known_token_calls',
  input_tokens: 'known_input_calls',
  output_tokens: 'known_output_calls',
  cache_read_tokens: 'known_cache_read_calls',
  cache_write_tokens: 'known_cache_write_calls',
  reasoning_tokens: 'known_reasoning_calls',
  total_spend: 'known_cost_calls'
}
export function measurement(row, field) {
  if (!row) return null
  if (row.calls > 0 && knownFields[field] && !row[knownFields[field]])
    return null
  return row[field] ?? 0
}
export function formatMoney(value) {
  if (value == null) return 'Unknown'
  if (value > 0 && value < 0.00000001) return `$${value.toExponential(2)}`
  return `$${value.toLocaleString(undefined, { minimumFractionDigits: 4, maximumFractionDigits: value > 0 && value < 0.0001 ? 8 : 4 })}`
}
export function formatNumber(value) {
  return value == null ? 'Unknown' : value.toLocaleString()
}
export function addMetrics(rows) {
  const totals = {}
  for (const row of rows)
    for (const [key, value] of Object.entries(row)) {
      if (typeof value === 'number' && key !== 'bucket')
        totals[key] = (totals[key] || 0) + value
    }
  return totals
}
export function sortGroups(rows, field, descending = true) {
  return [...rows].sort((a, b) => {
    const av = measurement(a, field),
      bv = measurement(b, field)
    if (av == null && bv != null) return 1
    if (bv == null && av != null) return -1
    return (
      ((av ?? 0) - (bv ?? 0)) * (descending ? -1 : 1) ||
      a.name.localeCompare(b.name) ||
      a.group_id.localeCompare(b.group_id)
    )
  })
}
export function localDate(date, timezone) {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: timezone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit'
  }).formatToParts(date)
  const get = (type) => parts.find((p) => p.type === type).value
  return `${get('year')}-${get('month')}-${get('day')}`
}
export function dateRange(preset, timezone, now = new Date()) {
  const to = localDate(now, timezone)
  const start = new Date(`${to}T12:00:00Z`)
  start.setUTCDate(
    start.getUTCDate() - (preset === 'today' ? 0 : preset === '30d' ? 29 : 6)
  )
  return { from: start.toISOString().slice(0, 10), to }
}
