import { createRouter, createWebHashHistory } from 'vue-router'
import DashboardView from '../views/DashboardView.vue'
import ModelsView from '../views/ModelsView.vue'
import LogsView from '../views/LogsView.vue'
import ConfigView from '../views/ConfigView.vue'
import SettingsView from '../views/SettingsView.vue'
import ApiDocsView from '../views/ApiDocsView.vue'
import LoginView from '../views/LoginView.vue'
import UsersView from '../views/UsersView.vue'
import TeamsView from '../views/TeamsView.vue'
import ApplicationsView from '../views/ApplicationsView.vue'
import KeysView from '../views/KeysView.vue'
import { useSession } from '../composables/useSession.js'

const routes = [
  { path: '/login', name: 'login', component: LoginView, meta: { requiresAuth: false } },
  { path: '/', name: 'dashboard', component: DashboardView },
  { path: '/models', name: 'models', component: ModelsView },
  { path: '/logs', name: 'logs', component: LogsView },
  { path: '/config', name: 'config', component: ConfigView },
  { path: '/settings', name: 'settings', component: SettingsView },
  { path: '/api-docs', name: 'api-docs', component: ApiDocsView },
  { path: '/users', name: 'users', component: UsersView },
  { path: '/teams', name: 'teams', component: TeamsView },
  { path: '/applications', name: 'applications', component: ApplicationsView },
  { path: '/keys', name: 'keys', component: KeysView },
  { path: '/cost', name: 'cost', component: () => import('../views/CostView.vue') },
  { path: '/pools', name: 'pools', component: () => import('../views/PoolsView.vue') },
  { path: '/webhooks', name: 'webhooks', component: () => import('../views/WebhooksView.vue') },
  { path: '/events', name: 'events', component: () => import('../views/EventsView.vue') },
]

const router = createRouter({
  history: createWebHashHistory(),
  routes,
})

router.beforeEach(async (to) => {
  // No auth required for public routes (e.g. /login)
  if (to.meta.requiresAuth === false) return true

  const { isAuthenticated, fetchCurrentUser } = useSession()

  // If session state not yet loaded (e.g. page refresh), check /admin/me
  // fetchCurrentUser() returns false (not throw) if unauthenticated — 401 loop safe
  if (!isAuthenticated.value) {
    const ok = await fetchCurrentUser()
    if (!ok) return { name: 'login' }
  }
  return true
})

export default router
