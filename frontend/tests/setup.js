// Global test setup
// localStorage is available via jsdom automatically, but we clear it between tests.
beforeEach(() => {
  localStorage.clear()
})
