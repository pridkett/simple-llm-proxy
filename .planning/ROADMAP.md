# Roadmap: Simple LLM Proxy

## Overview

The proxy already functions as a solid foundation: OpenAI-compatible endpoints, multi-provider routing, SQLite logging, and a Vue 3 admin frontend. This milestone adds the full multi-user layer — SSO identity, team and application organization, per-app API keys with enforcement, and spend visibility — so any application in the team can use LLMs safely through a single proxy without sharing raw API keys.

Three phases deliver this in dependency order: identity first, enforcement second, visibility third.

## Milestone: v1.0 Multi-User Proxy

**Goal:** Ship a fully operational multi-user proxy where teams manage their own applications and API keys, the proxy enforces per-key model restrictions and budgets on every request, and admins can monitor spend across the entire deployment.

## Phases

- [x] **Phase 1: Auth & Identity** - SSO login, user model, teams, applications, and RBAC — the identity foundation everything else depends on (completed 2026-03-25)
- [ ] **Phase 2: API Keys & Enforcement** - Per-app keys with model allowlists, rate limits, hard budget caps, and real-time cost recording enforced on every proxied request
- [ ] **Phase 3: Cost Monitoring & Complete Console** - Spend dashboards by key/app/team with date filters and soft-budget alert surfacing

## Phase Details

### Phase 1: Auth & Identity
**Goal**: Any team member can sign in via SSO and admins can organize users into teams with scoped applications — the identity foundation for all downstream access control
**Depends on**: Nothing (first phase)
**Requirements**: AUTH-01, AUTH-02, AUTH-03, AUTH-04, TEAM-01, TEAM-02, TEAM-03, TEAM-04, TEAM-05, TEAM-06, UI-01, UI-02, UI-03, UI-07
**Success Criteria** (what must be TRUE):
  1. User can sign in at the login page via the PocketID SSO button and land on the dashboard after auth
  2. User's session persists across browser refreshes; an expired or invalid session redirects cleanly to the login page
  3. Admin can view all authenticated users, create and delete teams, and assign users to teams with a specific role (admin, member, or viewer)
  4. Admin can change a user's role within a team and remove users from teams
  5. Admin can create and delete applications scoped to a team; user can view the teams they belong to
**Plans**: 7 plans
**UI hint**: yes

Plans:
- [x] 01-01-PLAN.md — ADR 003: Auth & Identity architecture document + GitHub issues
- [x] 01-02-PLAN.md — Schema: 5 new SQLite tables + extended Storage interface + session store + feature branch creation
- [x] 01-03-PLAN.md — Backend: go-oidc/v3 + scs/v2 deps, OIDCSettings config, internal/auth/ package, session middleware
- [x] 01-04-PLAN.md — Backend: OIDC auth handlers, router dual-group restructure, main.go session wiring
- [x] 01-05-PLAN.md — Backend: Users/Teams/Applications CRUD admin endpoints via RegisterAdminRoutes
- [x] 01-06-PLAN.md — Frontend: Login view, session composable, router guards, 401 interceptor, NavBar session display
- [x] 01-07-PLAN.md — Frontend: Users, Teams, Applications admin views + human verification checkpoint

### Phase 2: API Keys & Enforcement
**Goal**: Each application has its own API key with configurable model restrictions, rate limits, and a hard spend cap that the proxy validates and enforces on every request
**Depends on**: Phase 1
**Requirements**: KEY-01, KEY-02, KEY-03, KEY-04, KEY-05, KEY-06, KEY-07, KEY-08, KEY-09, COST-01, COST-03, UI-04
**Success Criteria** (what must be TRUE):
  1. Admin or team member can create an API key for an application with model allowlist, rate limits, and cost budget configured; the full key value is shown only at creation time
  2. Proxy rejects requests from revoked keys with 401, from disallowed models with 403, and from keys exceeding rate limits with 429
  3. Requests that would push a key over its hard budget limit are blocked with 429 and a message indicating the budget was exceeded
  4. Every successfully proxied request records its cost (tokens x model rate) against the originating key in storage
  5. Admin or team member can list all keys for an application, see each key's prefix and configuration, and revoke any key
**Plans**: 7 plans
**UI hint**: yes

Plans:
- [x] 02-00-PLAN.md — ADR 004: API Keys enforcement architecture document + GitHub issue
- [x] 02-01-PLAN.md — Schema migrations (replace api_keys table, add key_allowed_models), Storage interface + SQLite key CRUD
- [x] 02-02-PLAN.md — In-memory enforcement engine: key cache (TTL+invalidation), RPM/RPD counters, spend accumulator
- [ ] 02-03-PLAN.md — KeyAuth middleware replacing Auth() on /v1/*; ErrRateLimited + ErrBudgetExceeded error types
- [x] 02-04-PLAN.md — Cost attribution: model allowlist check in handlers, APIKeyID in logRequest, sa.Credit() after success
- [ ] 02-05-PLAN.md — Admin key handlers (list/create/revoke), route registration, router wiring, main.go keystore startup
- [ ] 02-06-PLAN.md — Frontend: KeysView.vue (Team→App→Keys drill-down, create form, modal, revoke), client.js, router, NavBar

### Phase 3: Cost Monitoring & Complete Console
**Goal**: Admins and team members can see a full breakdown of spend across the deployment — by key, application, and team — with soft-budget alerts surfaced in the console, completing the admin experience
**Depends on**: Phase 2
**Requirements**: COST-02, COST-04, COST-05, UI-05, UI-06
**Success Criteria** (what must be TRUE):
  1. Admin can view a cost dashboard showing spend broken down by key, application, and team with chart and table views
  2. Dashboard supports date range filtering: today, last 7 days, last 30 days, and a custom date range
  3. Console displays an alert banner or badge for any key or application that is approaching or has exceeded its soft budget threshold
**Plans**: TBD
**UI hint**: yes

## Progress

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Auth & Identity | 7/7 | Complete   | 2026-03-25 |
| 2. API Keys & Enforcement | 0/7 | Not started | - |
| 3. Cost Monitoring & Complete Console | 0/TBD | Not started | - |

---

*Roadmap created: 2026-03-25*
*Granularity: coarse (3 phases)*
*Coverage: 29/29 v1 requirements mapped*
