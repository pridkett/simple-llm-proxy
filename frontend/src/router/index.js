import { createRouter, createWebHashHistory } from 'vue-router'
import DashboardView from '../views/DashboardView.vue'
import ModelsView from '../views/ModelsView.vue'
import LogsView from '../views/LogsView.vue'
import ConfigView from '../views/ConfigView.vue'
import SettingsView from '../views/SettingsView.vue'

const routes = [
  { path: '/', name: 'dashboard', component: DashboardView },
  { path: '/models', name: 'models', component: ModelsView },
  { path: '/logs', name: 'logs', component: LogsView },
  { path: '/config', name: 'config', component: ConfigView },
  { path: '/settings', name: 'settings', component: SettingsView },
]

export default createRouter({
  history: createWebHashHistory(),
  routes,
})
