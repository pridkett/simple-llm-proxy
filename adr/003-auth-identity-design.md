# ADR 003: Auth & Identity Architecture

**Status:** Accepted
**Date:** 2026-03-25
**Issues:** pridkett/simple-llm-proxy#16, pridkett/simple-llm-proxy#17
**ADR Issue:** pridkett/simple-llm-proxy#16

---

## Context

The proxy currently has a single master key for all clients. Any caller with the key can do anything — there is no identity, no per-user audit trail, and no way to know who is making which request. This is appropriate for the initial bootstrap of the system, but as the proxy is deployed for team use, the following gaps become significant:

- No identity: all requests are attributed to "the master key holder"
- No user-level audit log: impossible to answer "who called GPT-4 at 3pm yesterday?"
- No access control beyond the binary "has key / doesn't have key"
- No self-service: adding a new user means giving them the master key

The goal is to add SSO-based identity via PocketID (an already-deployed OIDC provider) without breaking the existing machine clients that use the master key on `/v1/*`. The result is two coexisting auth models: Bearer token for machine clients (unchanged) and an HttpOnly session cookie for browser users accessing the admin interface.

This ADR documents all architectural decisions for Phase 1 (Auth & Identity) before any implementation code is written, as required by the ADR-first mandate in CLAUDE.md.

---

## Decision

### Decision 1: OIDC Library

Use `github.com/coreos/go-oidc/v3` **v3.17.0** and `golang.org/x/oauth2` **v0.36.0**.

`go-oidc/v3` is the de facto Go OIDC client library. It handles OIDC Discovery (`.well-known/openid-configuration`), JWKS key rotation, and ID token validation (issuer, audience, expiry, nonce). PocketID exposes a standard OIDC discovery endpoint. Manual JWKS handling and claim verification would be error-prone and is not hand-rolled here.

Do NOT use any other OIDC library (e.g., `golang.org/x/oauth2/google` or a custom JWT verifier).

### Decision 2: Session Management

Use `github.com/alexedwards/scs/v2` **v2.9.0** with a **custom `CtxStore`** backed by `modernc.org/sqlite`.

**Why NOT `scs/sqlite3store`:** The official SCS SQLite store (`github.com/alexedwards/scs/sqlite3store`) depends on `mattn/go-sqlite3`, which requires CGO. This project uses `modernc.org/sqlite` (pure Go, no CGO) and must remain CGO-free. A custom `CtxStore` implementation backed by `modernc.org/sqlite` requires approximately 60 lines and avoids the CGO dependency entirely.

Session tokens are opaque 32-byte random strings stored in a `sessions` table in SQLite. Sessions are **NOT JWTs** — they are opaque references into the server-side session store. The token alone reveals nothing about the user.

### Decision 3: Token Transport

Browser clients use an **HttpOnly `proxy_session` cookie**. Machine clients continue to use `Authorization: Bearer <master_key>` on `/v1/*` unchanged.

JavaScript MUST NOT read or store the session token — it is HttpOnly and browser-opaque by design. The frontend uses `fetch(..., { credentials: 'include' })` on all API calls so the browser automatically attaches the session cookie. User identity information (name, email, `is_admin`) is read from `GET /admin/me` which returns JSON; it is never derived from the cookie value itself.

### Decision 4: Session Cookie Configuration

The SCS `SessionManager` MUST be configured with these exact attributes:

```go
sm.Cookie.Name     = "proxy_session"
sm.Cookie.HttpOnly = true
sm.Cookie.Secure   = true  // false ONLY when OIDCSettings.DevMode = true
sm.Cookie.SameSite = http.SameSiteLaxMode
sm.Cookie.Path     = "/"
sm.Lifetime        = 24 * time.Hour
sm.IdleTimeout     = 2 * time.Hour
```

A `DevMode bool` field is added to `OIDCSettings` in `internal/config/config.go`. When `DevMode: true`, `Cookie.Secure` is set to `false` to allow development over HTTP (e.g., `http://localhost:8080`). **Production deployments MUST NOT set `DevMode: true`.**

### Decision 5: Session Fixation Mitigation

After a successful OIDC token exchange and user upsert, but BEFORE writing the `user_id` to the session, the `/auth/callback` handler MUST call `sm.RenewToken(ctx)`. This rotates the session token so any pre-authentication session ID is invalidated and cannot be reused by an attacker.

The exact order in `/auth/callback` is:
1. Verify `state` query param matches the `state` cookie value
2. Exchange authorization code for tokens
3. Extract and verify the `id_token` (using `IDTokenVerifier.Verify`)
4. Verify `idToken.Nonce` matches the `nonce` cookie value
5. Extract claims (`sub`, `email`, `name`, `groups`)
6. Upsert user in the `users` table
7. Call `sm.RenewToken(ctx)` — session fixation mitigation
8. Call `sm.Put(ctx, "user_id", sub)` — bind authenticated identity to session

### Decision 6: CSRF Protection Model

Since the session cookie uses `SameSite=Lax`, cross-origin POST requests to `/admin/*` from other origins are blocked by the browser's SameSite enforcement. Under Lax mode, the cookie is sent on top-level navigations (GET) but NOT on cross-origin subresource POST/PATCH/DELETE requests.

For this deployment model (single-deployment, small team, admin-only operations), **`SameSite=Lax` is the accepted CSRF protection for all admin mutations (POST/PATCH/DELETE)**. No synchronizer token or double-submit cookie pattern is required. This is an explicit architectural decision, not an oversight.

Future v2 requirements (multi-tenant SaaS, third-party integrations) would require re-evaluation. If the CORS `AllowedOrigins` list is ever extended beyond the known dev/prod origins, the CSRF posture must be reassessed.

### Decision 7: Dual Auth Group Design

Two separate Chi `mux.Group()` blocks serve distinct auth populations:

- **`/v1/*` group:** retains `middleware.Auth(masterKey)` UNCHANGED. All existing machine clients continue to authenticate with `Authorization: Bearer <master_key>`. No changes to this group's behavior.
- **`/admin/*` group:** gets `sessionManager.LoadAndSave` + `middleware.RequireSession(store, sm)`. Browser users authenticate via OIDC and are identified by session cookie.

The OIDC flow endpoints (`/auth/login`, `/auth/callback`, `/auth/logout`) and the identity endpoint (`/admin/me`) have NO auth middleware — they establish or query the session itself.

**Ownership:** Plan 01-03 owns the `internal/api/router.go` route scaffolding changes. Plan 01-04 provides `RegisterAdminRoutes(r chi.Router, store storage.Storage)` in a new file `internal/api/handler/admin_routes.go` — it does NOT modify `router.go` directly.

### Decision 8: RBAC Model

Three-tier per-team roles: `admin`, `member`, `viewer`. Stored as:

```sql
role TEXT NOT NULL CHECK(role IN ('admin','member','viewer'))
```

in the `team_members` table.

System-wide admin status is detected from the PocketID `groups` OIDC claim. Users whose `groups` array contains the configured `admin_group` value (default: `"admin"`) get `is_admin = TRUE` in the `users` table. This field is updated on every login. No custom roles beyond this 3-tier model exist.

### Decision 9: User Identity Anchoring

`users.id` IS the OIDC `sub` claim (TEXT primary key, not a UUID). The subject identifier from PocketID is stable and unique per user. Using it directly as the primary key avoids fragile account reconciliation between OIDC subjects and internal IDs. If a user re-authenticates after a long absence, the same `sub` resolves to the same row.

### Decision 10: OIDC Token Validation Requirements

The `/auth/callback` handler MUST perform these validation steps in order:

1. Verify `state` query param matches the `state` cookie value (reject HTTP 400 if mismatch)
2. Exchange authorization code for tokens via `oauth2.Config.Exchange`
3. Extract raw `id_token` from the token response extras
4. Call `IDTokenVerifier.Verify(ctx, rawIDToken)` — this handles issuer, audience, and expiry validation automatically
5. Verify `idToken.Nonce` matches the `nonce` cookie value (reject HTTP 400 if mismatch)
6. Extract claims: `sub`, `email`, `name`, `groups`
7. If the `email_verified` claim exists and is `false`, reject login with HTTP 403
8. If the `groups` claim is absent or empty: treat as empty array (non-admin user), log `WARN "groups claim absent for user {sub}"`, do NOT reject login

### Decision 11: Session Storage Schema

A `sessions` table stores SCS session data:

```sql
CREATE TABLE IF NOT EXISTS sessions (
    token  TEXT     PRIMARY KEY,
    data   BLOB     NOT NULL,
    expiry DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expiry);
```

A custom `SessionStore` struct implements the SCS `CtxStore` interface with three methods: `FindCtx`, `CommitCtx`, and `DeleteCtx`. A `CleanExpiredSessions(ctx context.Context) error` method is added to the `Storage` interface and the SQLite implementation. A background goroutine in `cmd/proxy/main.go` calls `CleanExpiredSessions` on a 1-hour ticker to prevent unbounded table growth.

### Decision 12: Config Extension

Add `OIDCSettings` struct to `internal/config/config.go`:

```go
type OIDCSettings struct {
    IssuerURL    string `yaml:"issuer_url"`    // PocketID base URL, e.g. https://pocketid.example.com
    ClientID     string `yaml:"client_id"`     // supports os.environ/VAR_NAME
    ClientSecret string `yaml:"client_secret"` // supports os.environ/VAR_NAME
    RedirectURL  string `yaml:"redirect_url"`  // must be a real server path, NOT a hash route
    AdminGroup   string `yaml:"admin_group"`   // PocketID group for system admins (default: "admin")
    DevMode      bool   `yaml:"dev_mode"`      // when true, Cookie.Secure=false for local HTTP dev
}
```

The `redirect_url` MUST be a real server-side path (e.g., `https://proxy.example.com/auth/callback`), NOT a Vue hash route (e.g., `/#/auth/callback`). The OIDC authorization server redirects to the backend, not the frontend SPA.

### Decision 13: Migration Strategy

Append-only. Add 5 new entries to the `migrations` slice in `internal/storage/sqlite/migrations.go` (migrations 6–10). Numbering:
- 6 = `users`
- 7 = `teams`
- 8 = `team_members`
- 9 = `applications`
- 10 = `sessions`

The existing migrations runner executes all entries unconditionally using `CREATE TABLE IF NOT EXISTS`, making append-safe. A note: a proper versioned migration system (e.g., with applied-version tracking) should be introduced in a future ADR to support safe production deployments.

### Decision 14: New SQLite Tables

```sql
-- Migration 6: users
CREATE TABLE IF NOT EXISTS users (
    id         TEXT     PRIMARY KEY,
    email      TEXT     NOT NULL,
    name       TEXT     NOT NULL,
    is_admin   BOOLEAN  NOT NULL DEFAULT FALSE,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    last_seen  DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- Migration 7: teams
CREATE TABLE IF NOT EXISTS teams (
    id         INTEGER  PRIMARY KEY AUTOINCREMENT,
    name       TEXT     NOT NULL UNIQUE,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- Migration 8: team_members (both FK sides use ON DELETE CASCADE)
CREATE TABLE IF NOT EXISTS team_members (
    team_id INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role    TEXT    NOT NULL CHECK(role IN ('admin','member','viewer')),
    PRIMARY KEY (team_id, user_id)
);

-- Migration 9: applications (ON DELETE CASCADE so team deletion removes its apps)
CREATE TABLE IF NOT EXISTS applications (
    id         INTEGER  PRIMARY KEY AUTOINCREMENT,
    team_id    INTEGER  NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    name       TEXT     NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(team_id, name)
);

-- Migration 10: sessions
CREATE TABLE IF NOT EXISTS sessions (
    token  TEXT PRIMARY KEY,
    data   BLOB NOT NULL,
    expiry DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expiry);
```

### Decision 15: PKCE

Include PKCE (`oauth2.S256ChallengeOption`) in the authorization code flow. PocketID supports PKCE. Using PKCE mitigates authorization code interception attacks, and is considered best practice for server-side OIDC flows even when a client secret is present.

### Decision 16: Admin Group Detection and Scopes

Scopes requested from PocketID: `openid email profile groups`. The `groups` claim MUST be included in the scopes list or PocketID will omit it from the ID token. The `is_admin` field in the `users` table is set or updated on every login based on whether the user's `groups` array contains the configured `admin_group` value.

---

## Consequences

- **No breaking change for machine clients.** Existing `/v1/*` clients using `Authorization: Bearer <master_key>` continue to work exactly as before. The master key middleware is not replaced; it is retained in its own Chi route group.
- **Per-user identity and audit capability.** Admin routes gain per-user identity: every request to `/admin/*` is attributable to a specific OIDC `sub`. This enables per-user audit logging in future phases.
- **Adding a new OIDC provider.** The design uses standard OIDC discovery, so switching from PocketID to another provider (Keycloak, Authentik, Google) requires a config change and nothing else.
- **Custom SCS store.** The custom `CtxStore` implementation adds approximately 60 lines of code to avoid the CGO dependency on `mattn/go-sqlite3`. This is a one-time cost.
- **SameSite=Lax constraint.** The session cookie is not sent on cross-origin POST requests. This is both the CSRF protection mechanism and a constraint: all admin mutations must originate from the same origin (or be triggered by top-level navigation). Third-party integrations calling `/admin/*` from a different origin will not have access to the session cookie and must use the master key via `/v1/*` instead.
- **Session storage growth.** The `sessions` table will grow without the background cleanup goroutine. The 1-hour ticker and `CleanExpiredSessions` are load-bearing — if the background goroutine is not started, the database will accumulate expired session rows indefinitely.

---

## Alternatives Considered

### JWT session tokens
**Rejected.** JWTs cannot be revoked without a token blocklist, which adds complexity equivalent to the session store. JWTs also expose claims to anyone who inspects the token. The opaque-token session approach (via SCS) is simpler and provides server-side revocation for free.

### SCS sqlite3store
**Rejected.** The official `github.com/alexedwards/scs/sqlite3store` depends on `mattn/go-sqlite3` which requires CGO. This project is CGO-free by constraint. A custom `CtxStore` backed by `modernc.org/sqlite` achieves the same result without CGO.

### Single auth group for all routes
**Rejected.** Combining `/v1/*` and `/admin/*` into a single auth group would require all machine clients to adopt session-cookie auth, breaking backward compatibility. The dual-group design preserves the existing machine client interface.

### Local password storage
**Rejected.** PocketID SSO is already deployed and is a hard requirement. Adding a local password system introduces credential management complexity that is out of scope by explicit project constraint.

### Synchronizer CSRF tokens
**Rejected.** `SameSite=Lax` is sufficient CSRF protection for this single-deployment, admin-only model. The additional complexity of synchronizer token generation, storage, and validation is not justified for this threat model.

### localStorage session token
**Rejected.** Storing the session token in `localStorage` would allow JavaScript to read and potentially exfiltrate it. HttpOnly cookies are inaccessible to JavaScript by design. The frontend reads user identity from `GET /admin/me` JSON — it never holds the session token itself. Any design that puts the session token in `localStorage` defeats the HttpOnly protection.

---

## References

- [`github.com/coreos/go-oidc/v3`](https://pkg.go.dev/github.com/coreos/go-oidc/v3) — OIDC client
- [`github.com/alexedwards/scs/v2`](https://pkg.go.dev/github.com/alexedwards/scs/v2) — Session management
- [`golang.org/x/oauth2`](https://pkg.go.dev/golang.org/x/oauth2) — OAuth2 / token exchange
- `internal/api/middleware/auth.go` — Master key middleware (retained on `/v1/*`)
- `internal/api/router.go` — Chi router (extended with new auth groups in Plan 01-03)
- `internal/storage/sqlite/migrations.go` — Migration slice (extended with migrations 6-10)
- `internal/config/config.go` — Config structs (extended with `OIDCSettings`)
- `.planning/phases/01-auth-identity/01-RESEARCH.md` — Library version verification and architecture patterns
