import TeamsView from '../views/TeamsView.vue'

function legacyWorkspace(to) {
  const teamId = String(to.query.team_id ?? '')
  const appId = String(to.query.app_id ?? '')
  if (!/^[1-9]\d*$/.test(teamId)) return '/teams'
  const team = `/teams/${teamId}`
  return /^[1-9]\d*$/.test(appId) ? `${team}/applications/${appId}` : team
}

export const teamRoutes = [
  { path: '/teams', name: 'teams', component: TeamsView },
  { path: '/teams/new', name: 'team-new', component: TeamsView },
  {
    path: '/teams/:teamId(\\d+)',
    name: 'team-workspace',
    component: TeamsView
  },
  {
    path: '/teams/:teamId(\\d+)/members',
    name: 'team-members',
    component: TeamsView
  },
  {
    path: '/teams/:teamId(\\d+)/applications/new',
    name: 'team-application-new',
    component: TeamsView
  },
  {
    path: '/teams/:teamId(\\d+)/applications/:appId(\\d+)',
    name: 'team-application',
    component: TeamsView
  },
  {
    path: '/teams/:teamId(\\d+)/applications/:appId(\\d+)/keys/new',
    name: 'team-key-new',
    component: TeamsView
  },
  {
    path: '/teams/:teamId(\\d+)/applications/:appId(\\d+)/keys/:keyId(\\d+)',
    name: 'team-key',
    component: TeamsView
  },
  { path: '/applications', name: 'applications', redirect: legacyWorkspace },
  { path: '/keys', name: 'keys', redirect: legacyWorkspace }
]
