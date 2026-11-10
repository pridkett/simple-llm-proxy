function parseDate(value) {
  if (!value || value.startsWith('0001-01-01T')) return null
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
}

export function formatDate(value) {
  return parseDate(value)?.toLocaleDateString() ?? '—'
}

export function formatDateTime(value) {
  return parseDate(value)?.toLocaleString(undefined, {
    year: 'numeric',
    month: 'numeric',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
    second: '2-digit',
    timeZoneName: 'short'
  }) ?? '—'
}

export function toLocalDateTime(value) {
  const date = parseDate(value)
  if (!date) return ''
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 19)
}

export function keyExpiration(value) {
  if (!value) return null
  const date = parseDate(value)
  const localSeconds = value.length === 16 ? value + ':00' : value.split('.')[0]
  if (!date || toLocalDateTime(date.toISOString()) !== localSeconds) {
    throw new Error('Enter a valid expiration date and time in your local time zone.')
  }
  if (date.getTime() <= Date.now()) {
    throw new Error('Expiration must be in the future.')
  }
  return date.toISOString()
}
