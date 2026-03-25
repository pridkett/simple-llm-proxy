import { ref, readonly } from 'vue'

// Module-level singleton state — shared across ALL consumers of useSession()
// This is intentional: auth state must be consistent across the entire app.
// The session is an HttpOnly cookie; JavaScript never reads the token directly.
// User identity is obtained by calling GET /admin/me with credentials:'include'.
const isAuthenticated = ref(false)
const currentUser = ref(null) // { id, email, name, is_admin } or null
const loading = ref(false)

// fetchCurrentUser calls GET /admin/me with credentials:'include' (cookie auth).
// Returns true if authenticated, false if not (401 is not an error — it means unauthenticated).
// Never stores a token — the HttpOnly session cookie is managed entirely by the browser.
async function fetchCurrentUser() {
  loading.value = true
  try {
    const res = await fetch('/admin/me', { credentials: 'include' })
    if (!res.ok) {
      // 401 means unauthenticated — not an error, just not logged in
      isAuthenticated.value = false
      currentUser.value = null
      return false
    }
    const user = await res.json()
    isAuthenticated.value = true
    currentUser.value = user
    return true
  } catch {
    // Network error — treat as unauthenticated
    isAuthenticated.value = false
    currentUser.value = null
    return false
  } finally {
    loading.value = false
  }
}

function clearSession() {
  isAuthenticated.value = false
  currentUser.value = null
}

// useSession() returns the shared module-level singleton state.
// Every caller gets the same refs — there is only one instance of isAuthenticated/currentUser.
export function useSession() {
  return {
    isAuthenticated: readonly(isAuthenticated),
    currentUser: readonly(currentUser),
    loading: readonly(loading),
    fetchCurrentUser,
    clearSession,
  }
}
