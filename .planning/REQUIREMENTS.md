# Requirements: Simple LLM Proxy

**Defined:** 2026-03-25
**Core Value:** Any application in the team can call LLMs through a single proxy with its own key, budget, and model restrictions — without anyone sharing raw API keys or losing visibility into what's being spent.

## v1 Requirements

### Authentication (SSO)

- [x] **AUTH-01**: User can sign in via PocketID OAuth/OIDC (no local passwords)
- [x] **AUTH-02**: Session persists across browser refresh with valid token
- [x] **AUTH-03**: Session expires gracefully and redirects to login
- [x] **AUTH-04**: Admin can see all authenticated users in the console

### Users & Teams

- [x] **TEAM-01**: Admin can create and delete teams
- [x] **TEAM-02**: Admin can add and remove users from teams
- [x] **TEAM-03**: User has a role within a team: admin, member, or viewer
- [x] **TEAM-04**: Admin can change a user's role within a team
- [x] **TEAM-05**: User can view teams they belong to
- [x] **TEAM-06**: Admin can create and delete applications scoped to a team

### API Keys

- [x] **KEY-01**: Admin or team member can create an API key scoped to an application
- [x] **KEY-02**: API key has configurable model allowlist (empty = all models allowed)
- [x] **KEY-03**: API key has configurable cost budget (hard limit, blocks when exceeded)
- [x] **KEY-04**: API key has configurable soft budget threshold that triggers an alert
- [x] **KEY-05**: API key has configurable rate limits (requests per minute and per day)
- [x] **KEY-06**: Admin or team member can revoke (delete) a key
- [x] **KEY-07**: Admin or team member can view all keys for an application
- [x] **KEY-08**: Key is displayed in full only at creation time; thereafter only prefix is shown
- [x] **KEY-09**: Proxy validates key on every request, enforces model allowlist and rate limits

### Cost Monitoring

- [x] **COST-01**: Every proxied request records cost (tokens × model rate) against the key
- [x] **COST-02**: Dashboard shows spend breakdown by key, application, and team
- [x] **COST-03**: Hard budget limit blocks requests when exceeded; returns 429 with clear message
- [x] **COST-04**: Soft budget threshold triggers an in-UI alert (banner/badge in admin console)
- [x] **COST-05**: Cost data is queryable with date range filter (today / 7d / 30d / custom)

### Admin Console (Frontend)

- [x] **UI-01**: Login page with PocketID SSO button; redirects to dashboard after auth
- [x] **UI-02**: Teams view: list teams, create/delete team, manage members and roles
- [x] **UI-03**: Applications view: list apps per team, create/delete applications
- [x] **UI-04**: Keys view: list keys per app, create key (with all options), revoke key
- [x] **UI-05**: Cost dashboard: charts and tables for spend by key/app/team with date filter
- [x] **UI-06**: Alerts panel: show keys/apps approaching or exceeding soft/hard budgets
- [x] **UI-07**: User management: list all users (from SSO), assign/remove from teams

## v2 Requirements

### Advanced Key Management

- **KEY-V2-01**: Key expiration date (auto-revoke after date)
- **KEY-V2-02**: Key rotation — issue new key and deprecate old one in a single action
- **KEY-V2-03**: Key usage history / audit log per key

### Notifications

- **NOTF-01**: Email or webhook alert when soft budget threshold is hit
- **NOTF-02**: Email or webhook alert when hard budget limit blocks a request
- **NOTF-03**: Configurable notification recipients per team

### Additional SSO Providers

- **AUTH-V2-01**: Support generic OIDC provider configuration (beyond PocketID)
- **AUTH-V2-02**: Support for multiple simultaneous identity providers

### Usage Export

- **EXP-01**: Export cost/usage data to CSV
- **EXP-02**: API endpoint to query usage programmatically

## Out of Scope

| Feature | Reason |
|---------|--------|
| Multi-tenant org isolation | Single-deployment for one team; not a SaaS product |
| Custom roles / fine-grained permissions | 3-tier RBAC covers the use case cleanly |
| Local password management | SSO-only; no managing credentials |
| Billing / payment processing | Cost visibility only, no invoicing |
| Mobile app | Web admin console is sufficient |
| Plugin / extension system | Keep it lightweight |
| Real-time streaming cost tracking | Per-request logging is sufficient; streaming cost applied at completion |

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| AUTH-01 | Phase 1 | Complete |
| AUTH-02 | Phase 1 | Complete |
| AUTH-03 | Phase 1 | Complete |
| AUTH-04 | Phase 1 | Complete |
| TEAM-01 | Phase 1 | Complete |
| TEAM-02 | Phase 1 | Complete |
| TEAM-03 | Phase 1 | Complete |
| TEAM-04 | Phase 1 | Complete |
| TEAM-05 | Phase 1 | Complete |
| TEAM-06 | Phase 1 | Complete |
| KEY-01 | Phase 2 | Complete |
| KEY-02 | Phase 2 | Complete |
| KEY-03 | Phase 2 | Complete |
| KEY-04 | Phase 2 | Complete |
| KEY-05 | Phase 2 | Complete |
| KEY-06 | Phase 2 | Complete |
| KEY-07 | Phase 2 | Complete |
| KEY-08 | Phase 2 | Complete |
| KEY-09 | Phase 2 | Complete |
| COST-01 | Phase 2 | Complete |
| COST-02 | Phase 3 | Complete |
| COST-03 | Phase 2 | Complete |
| COST-04 | Phase 3 | Complete |
| COST-05 | Phase 3 | Complete |
| UI-01 | Phase 1 | Complete |
| UI-02 | Phase 1 | Complete |
| UI-03 | Phase 1 | Complete |
| UI-04 | Phase 2 | Complete |
| UI-05 | Phase 3 | Complete |
| UI-06 | Phase 3 | Complete |
| UI-07 | Phase 1 | Complete |

**Coverage:**
- v1 requirements: 29 total
- Mapped to phases: 29
- Unmapped: 0 ✓

---
*Requirements defined: 2026-03-25*
*Last updated: 2026-03-25 after initial definition*
