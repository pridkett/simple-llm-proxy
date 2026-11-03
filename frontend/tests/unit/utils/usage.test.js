import { describe, it, expect } from 'vitest'
import { dateRange, formatMoney, measurement } from '@/utils/usage.js'

describe('usage measurement semantics', () => {
  it('keeps missing token breakdown independent of a known total', () => {
    const row = {
      calls: 1,
      total_tokens: 900,
      known_token_calls: 1,
      input_tokens: 0,
      known_input_calls: 0
    }
    expect(measurement(row, 'total_tokens')).toBe(900)
    expect(measurement(row, 'input_tokens')).toBeNull()
  })
  it('preserves small nonzero money instead of formatting it as free', () => {
    expect(formatMoney(0)).toBe('$0.0000')
    expect(formatMoney(0.000001)).not.toBe('$0.0000')
    expect(formatMoney(1e-10)).toContain('e-10')
    expect(formatMoney(null)).toBe('Unknown')
  })
  it('uses the selected timezone and exactly seven calendar dates across DST', () => {
    expect(
      dateRange('7d', 'America/New_York', new Date('2026-11-04T02:00:00Z'))
    ).toEqual({ from: '2026-10-28', to: '2026-11-03' })
    expect(
      dateRange('today', 'Asia/Tokyo', new Date('2026-11-03T20:00:00Z'))
    ).toEqual({ from: '2026-11-04', to: '2026-11-04' })
  })
})
