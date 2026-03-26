# Simple LLM Proxy

## What This Is

A lightweight Go-based LLM proxy that provides OpenAI-compatible endpoints with multi-provider support (OpenAI, Anthropic). Designed for solo developers and small teams who need to safely share and manage LLM access with per-application keys, cost controls, and team-based access — without the overhead of heavyweight solutions like LiteLLM server or OpenRouter.

## Core Value

Any application in the team can call LLMs through a single proxy with its own key, budget, and model restrictions — without anyone sharing raw API keys or losing visibility into what's being spent.

## Requirements

### Validated

<!-- Shipped and confirmed valuable. -->

- ✓ OpenAI-compatible API proxy (chat completions, embeddings, streaming) — foundation
- ✓ Multi-provider routing (OpenAI, Anthropic) with automatic format translation — foundation
- ✓ Load balancing with round-robin and shuffle strategies — foundation
- ✓ Failure tracking and cooldown management — foundation
- ✓ LiteLLM-compatible YAML configuration with env var expansion — foundation
- ✓ SQLite storage (pure Go, no CGO) for request logging — foundation
- ✓ Master key authentication — foundation
- ✓ Admin API (status, config, paginated logs) — foundation
- ✓ Vue 3 + Tailwind frontend (dashboard, models, logs, config, settings views) — foundation
- ✓ Structured logging with zerolog — foundation
- ✓ Model cost tracking and cost editor UI — foundation

### Active

<!-- Current scope. Building toward these. -->

- ✓ PocketID OAuth / OIDC SSO authentication — replace master key with real identity (Validated in Phase 1: auth-identity)
- ✓ User model with team membership and 3-tier RBAC (admin / member / viewer) (Validated in Phase 1: auth-identity)
- ✓ Team and application entities — logical groupings for key management (Validated in Phase 1: auth-identity)
- ✓ Per-application API keys with model allowlist, cost budget, rate limiting, and team scoping (Validated in Phase 2: api-keys-enforcement)
- ✓ Cost monitoring dashboards per key/app/team with hard/soft spend limits and alerts (Validated in Phase 3: cost-monitoring-complete-console)
- ✓ Full admin console in the frontend — user management, team config, key issuance, spend visibility (Validated in Phase 3: cost-monitoring-complete-console)

### Out of Scope

<!-- Explicit boundaries. Includes reasoning to prevent re-adding. -->

- Multi-tenant SaaS / isolated org namespacing — this is for a single deployment serving one team
- Custom role definitions — 3-tier RBAC (admin/member/viewer) covers the use case without complexity
- Alternative SSO providers beyond PocketID in v1 — any OIDC provider can be added later, PocketID first
- Billing / invoicing — cost visibility only, no payment processing
- Plugin or extension system — keep it lightweight

## Context

**Existing codebase:** Functional proxy with YAML-based model configuration, admin API, SQLite logging, and a Vue 3 frontend. Auth is currently a single master key configured via environment variable. The data model has no concept of users, teams, or applications — all of that needs to be built.

**Technical environment:** Go backend with Chi router, pure-Go SQLite (`modernc.org/sqlite`), Vue 3 + Vite + Tailwind frontend. PocketID instance already deployed for SSO.

**Priority order:** Auth + identity (PocketID SSO, users, teams, RBAC) → per-app keys with controls → cost monitoring dashboards → full admin console.

**Key tension:** Keep the lightweight, low-overhead feel while adding multi-user features. Don't let RBAC complexity bleed into the proxy hot path.

## Constraints

- **Tech Stack**: Go backend, Vue 3 frontend — no framework rewrites
- **Storage**: SQLite (pure Go) — no external database dependencies
- **Auth**: PocketID via OIDC/OAuth — no custom password management
- **Performance**: Auth/RBAC checks must not meaningfully impact proxy latency
- **Scope**: Single-deployment, small team — no multi-tenant isolation needed

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| PocketID for SSO | Already deployed, hate managing passwords | — Pending |
| SQLite for storage | No CGO, zero ops overhead | ✓ Good |
| 3-tier RBAC (admin/member/viewer) | Covers use case without custom role complexity | — Pending |
| Per-app keys (not per-user) | Applications are the unit of access, not individuals | — Pending |

## Current State

Phase 3 complete (2026-03-26) — full v1.0 Multi-User Proxy milestone delivered. All planned phases shipped: SSO auth, per-app API keys, cost monitoring dashboard with spend aggregation and alerts, and complete admin console frontend.

---
*Last updated: 2026-03-26 — Phase 3 complete, v1.0 milestone finished*
