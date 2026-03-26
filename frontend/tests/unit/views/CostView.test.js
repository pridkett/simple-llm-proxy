import { describe, it } from 'vitest'

// CostView.vue will be implemented in Plan 4.
// These tests are stubs for Wave 0 — they pass trivially.
// Plan 4 will replace these with real assertions.
describe('CostView', () => {
  it.todo('renders LoadingSpinner while loading')
  it.todo('renders ErrorAlert on API failure')
  it.todo('renders Alerts Panel when alerts array is non-empty')
  it.todo('hides Alerts Panel when alerts array is empty')
  it.todo('renders breakdown table rows from spend data')
  it.todo('renders empty state when spend rows array is empty')
  it.todo('filter bar defaults to 7d date range selection')
  it.todo('re-fetches data when date range filter changes')
  it.todo('re-fetches data with resolved team_id when team dropdown changes')
  it.todo('re-fetches data with resolved app_id when app dropdown changes')
  it.todo('re-fetches data with resolved key_id when key dropdown changes')
})
