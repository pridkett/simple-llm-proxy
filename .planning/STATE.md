---
gsd_state_version: 1.0
milestone: v1.0
milestone_name: Multi-User Proxy
status: Ready to execute
stopped_at: "Completed 02-api-keys-enforcement plan 01: API key storage foundation, 7 new tests pass"
last_updated: "2026-03-26T00:57:47.594Z"
progress:
  total_phases: 3
  completed_phases: 1
  total_plans: 14
  completed_plans: 9
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-03-25)

**Core value:** Any application in the team can call LLMs through a single proxy with its own key, budget, and model restrictions — without anyone sharing raw API keys or losing visibility into what's being spent.
**Current focus:** Phase 02 — api-keys-enforcement

## Current Position

Phase: 02 (api-keys-enforcement) — EXECUTING
Plan: 3 of 7

## Performance Metrics

**Velocity:**

- Total plans completed: 0
- Average duration: -
- Total execution time: -

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| - | - | - | - |

**Recent Trend:**

- Last 5 plans: -
- Trend: -

*Updated after each plan completion*
| Phase 01-auth-identity P01 | 15min | 3 tasks | 1 files |
| Phase 01-auth-identity P02 | 5min | 1 tasks | 7 files |
| Phase 01-auth-identity P03 | 4min | 1 tasks | 7 files |
| Phase 01-auth-identity P05 | ~15min | 1 tasks | 8 files |
| Phase 01-auth-identity P04 | 15min | 1 tasks | 7 files |
| Phase 01-auth-identity P06 | ~20min | 1 tasks | 11 files |
| Phase 01-auth-identity P07 | ~5min | 1 tasks | 10 files |
| Phase 01-auth-identity P02 | 25 | 1 tasks | 7 files |
| Phase 02-api-keys-enforcement P00 | 10min | 2 tasks | 1 files |
| Phase 02-api-keys-enforcement P01 | ~4min | 2 tasks | 7 files |

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.
Recent decisions affecting current work:

- Foundation: PocketID chosen for SSO (already deployed, no password management)
- Foundation: SQLite retained for storage (no CGO, zero ops overhead)
- Foundation: 3-tier RBAC (admin/member/viewer) — no custom roles
- Foundation: Per-app keys (not per-user) — applications are the unit of access
- [Phase 01-auth-identity]: go-oidc/v3 v3.17.0 + oauth2 v0.36.0 chosen for OIDC; scs/v2 v2.9.0 with custom modernc.org/sqlite CtxStore (no CGO); users.id = OIDC sub (TEXT PK); SameSite=Lax for CSRF; RenewToken before Put(user_id) for session fixation
- [Phase 01-auth-identity]: users.id stores OIDC sub claim directly (TEXT PK) — no UUID reconciliation
- [Phase 01-auth-identity]: SessionStore uses modernc.org/sqlite directly (CGO-free) — no scs/sqlite3store
- [Phase 01-auth-identity]: PRAGMA foreign_keys = ON set at DB open — required for ON DELETE CASCADE in SQLite
- [Phase 01-auth-identity]: NewOIDCProvider returns (nil,nil) when IssuerURL is empty — OIDC is optional, proxy starts without SSO config
- [Phase 01-auth-identity]: RequireSession detects API callers via Accept, Content-Type, X-Requested-With headers for 401 vs 302 routing
- [Phase 01-auth-identity]: RegisterAdminRoutes lives in admin_routes.go (not router.go) to avoid file ownership conflicts with Plan 04 in Wave 4 parallel execution
- [Phase 01-auth-identity]: ErrForbidden/ErrNotFound/ErrInternal added to model/error.go — missing constructors required by admin handlers
- [Phase 01-auth-identity]: Auth routes use sm.LoadAndSave without RequireSession — login must be reachable without a session
- [Phase 01-auth-identity]: RegisterAdminRoutes stub pattern: Plan 04 owns router.go, Plan 05 fills the body
- [Phase 01-auth-identity]: useSession uses module-level singleton state — Vue pattern for shared global auth; all consumers see same isAuthenticated/currentUser refs
- [Phase 01-auth-identity]: 401 interceptor returns null (not throw) and uses router.currentRoute path check to prevent redirect loop on login page
- [Phase 01-auth-identity]: vi.mock factory hoisting: use vi.fn() inside factory, vi.mocked() per-test — external variable references fail due to hoisting
- [Phase 01-auth-identity]: data-testid on delete/confirm buttons: avoids brittle text-matching for destructive actions with inline confirmation
- [Phase 01-auth-identity]: users.id stores OIDC sub claim directly (TEXT PK) — no UUID mapping needed
- [Phase 01-auth-identity]: SessionStore uses modernc.org/sqlite CtxStore (CGO-free) — not scs/sqlite3store
- [Phase 01-auth-identity]: PRAGMA foreign_keys = ON enabled at DB open time to enforce ON DELETE CASCADE
- [Phase 02-api-keys-enforcement]: SHA-256 chosen over bcrypt for key hashing: hot-path lookup requires deterministic sub-ms cost; 192-bit key entropy provides the security property
- [Phase 02-api-keys-enforcement]: keystore package (internal/keystore/) houses TTL cache, atomic rate counters, and SpendAccumulator — in-process stdlib-only, interface-backed for future Redis swap
- [Phase 02-api-keys-enforcement]: Spend flush loop: 30s periodic goroutine with shutdown flush; eventual consistency accepted as tradeoff over per-request DB writes on hot path
- [Phase 02-api-keys-enforcement]: RecordKeySpend is a no-op stub; Plan 04 wires spend recording via logRequest
- [Phase 02-api-keys-enforcement]: empty allowedModels slice means all models allowed (enforced at middleware level in Plan 03)

### Pending Todos

None yet.

### Blockers/Concerns

- Phase 1: Existing auth middleware (master key) must remain functional or be replaced cleanly — the proxy hot path cannot break during the SSO migration. Plan carefully.
- Phase 1: PocketID OIDC integration needs the PocketID instance URL and client credentials configured before implementation begins.

## Session Continuity

Last session: 2026-03-26T00:57:47.591Z
Stopped at: Completed 02-api-keys-enforcement plan 01: API key storage foundation, 7 new tests pass
Resume file: None
