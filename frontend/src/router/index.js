import { createRouter, createWebHashHistory } from 'vue-router'
import DashboardView from '../views/DashboardView.vue'
import ModelsView from '../views/ModelsView.vue'
import LogsView from '../views/LogsView.vue'
import ConfigView from '../views/ConfigView.vue'
import SettingsView from '../views/SettingsView.vue'
import ApiDocsView from '../views/ApiDocsView.vue'
import LoginView from '../views/LoginView.vue'
import UsersView from '../views/UsersView.vue'
import { teamRoutes } from './teamRoutes.js'
import ChatView from '../views/ChatView.vue'
import { useSession } from '../composables/useSession.js'

const routes = [
  { path: '/login', name: 'login', component: LoginView, meta: { requiresAuth: false } },
  { path: '/', name: 'dashboard', component: DashboardView },
  { path: '/mcp', name: 'mcp', component: () => import('../views/McpConnectionsView.vue') },
  { path: '/models', name: 'models', component: ModelsView },
  { path: '/chat', name: 'chat', component: ChatView },
  { path: '/logs', name: 'logs', component: LogsView },
  { path: '/config', name: 'config', component: ConfigView },
  { path: '/settings', name: 'settings', component: SettingsView },
  { path: '/api-docs', name: 'api-docs', component: ApiDocsView },
  { path: '/users', name: 'users', component: UsersView },
  ...teamRoutes,
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
  const { isAuthenticated, currentUser, fetchCurrentUser } = useSession()
  if (to.meta.requiresAuth === false) {
    // An auth redirect already checked /admin/me. Check direct login visits
    // so that old bookmarks also open the dashboard in local admin mode.
    if (!to.redirectedFrom && (isAuthenticated.value || await fetchCurrentUser()) && currentUser.value?.local_admin) {
      return { name: 'dashboard' }
    }
    return true
  }

  return isAuthenticated.value || await fetchCurrentUser() || { name: 'login' }
})

export default router
