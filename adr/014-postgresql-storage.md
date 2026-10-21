# ADR 014: Optional PostgreSQL persistence

**Status:** Accepted
**Date:** 2026-10-21
**Design:** pridkett/simple-llm-proxy#115
**Implementation:** pridkett/simple-llm-proxy#116

## Context

Persistence currently uses SQLite through `database/sql`. SQLite serializes
queries on one connection to avoid write contention. It stores identities,
teams, applications, API keys, sessions, cost overrides, request usage and body
snippets, routing state, notifications, webhooks, and Responses background jobs.
The existing `storage.Storage` interface separates consumers from persistence,
but startup, session handling, migrations, and several queries assume SQLite.

A home-network installation with an existing PostgreSQL service should be able
to use its connection concurrency, backup tooling, and independently managed
storage. Moving the database off-host also introduces network latency and a
new availability dependency. PostgreSQL is not inherently faster for a small
proxy, and a single database server is not high availability without operational
work such as backups, restore testing, monitoring, and replication/failover.

## Decision

### Backend selection and compatibility

Keep SQLite as the default and preserve existing paths, `file:` URIs, and
`:memory:` configurations. Select PostgreSQL for `postgres://` or
`postgresql://` URLs in `general_settings.database_url`. Reject other URL
schemes instead of creating an accidental SQLite file. An empty value retains
the existing disabled-persistence behavior. Support `os.environ/PROXY_DATABASE_URL`
so credentials need not appear in YAML. Changing backend requires a restart.

Use the pure-Go pgx v5 `database/sql` driver. Keep the public storage interface
and use one shared SQL implementation for CRUD, request logs, reporting, and
routing state. Backend packages own connections and migrations. Bind PostgreSQL
parameters as `$1`, `$2`, etc.; explicitly isolate dialect-specific date
expressions. Prefer portable `ON CONFLICT` and `RETURNING` SQL over SQLite
replacement semantics or `LastInsertId`. Avoid an ORM and a second copy of every
query. Keep the SQLite migration history intact.

Use a bounded PostgreSQL pool (10 open, 5 idle connections, 30-minute maximum
lifetime). SQLite keeps its single connection, WAL, busy timeout, and foreign
key settings. Bound database startup by a context deadline and fail startup
on connection or migration failure. Never silently fall back to SQLite.

### Schema, migrations, and sessions

Create a PostgreSQL-specific, versioned baseline representing the current
schema, without replaying SQLite's historical destructive migrations. Use
BIGINT identity keys and foreign keys, DOUBLE PRECISION for existing float64
cost fields, native BOOLEAN, BYTEA session data, and TIMESTAMPTZ timestamps.
Keep JSON payloads as TEXT to preserve current opaque payload semantics and
avoid introducing validation differences. Daily spend grouping uses UTC;
budget reset dates remain ISO date strings at the application boundary.

Apply PostgreSQL schema changes and their version records in one transaction,
protected by a transaction advisory lock before bootstrapping migration metadata.
This prevents competing initializers from racing and rolls back failed DDL.
Give PostgreSQL its own migration sequence, independent of SQLite's versions.
Future schema changes must update and test both backend migration histories.

Persist SCS login sessions in the selected database, sharing its pool. Preserve
session expiry and binary payload behavior. Remove credentials and connection
query parameters from the admin configuration response. Startup errors must
not print the supplied PostgreSQL URL, username, or password.

### Deployment and cutover

Target PostgreSQL 17 or newer and exercise PostgreSQL in CI with a real server.
Use a dedicated database and role with permission to create and migrate tables.
For a home-network database, configure TLS with `sslmode=verify-full` and a
trusted certificate where available. Choosing `sslmode=disable` is an explicit
operator decision for a trusted development network. Document backup and restore
responsibilities and the pool's connection budget.

Selecting a PostgreSQL URL creates an empty database schema; it does not move
SQLite data. Automatic import is deferred: ID preservation, sequences, timestamp
conversion, references, and reconciliation deserve a separately tested migration
tool. Existing installations should back up SQLite and either start with empty
PostgreSQL state (recreate teams/keys and sign in again), or postpone switching
until a verified import is available. Do not dual-write. For a fresh-state cutover,
stop the proxy, preserve SQLite, configure PostgreSQL, start and verify login,
key creation, logging and spend. Rollback means stop, restore the old database
configuration, and restart; PostgreSQL-only writes will not appear in SQLite.

### Scaling and future request/response archives

This change supports a single proxy process using a network database. It does
not promise coordinated horizontal scaling. Rate-limit windows, key caches,
spend/budget accumulators, routing snapshots, and background workers retain
process-local state. Multiple replicas could overspend, enforce inconsistent
limits, overwrite snapshots, or duplicate polling and webhook work. Replica-safe
operation requires a separate design for shared counters, cache invalidation,
atomic budgets, job claims/leases, and worker ownership.

Preserve current bounded, configurable body snippets and retention. Full
request/response archival is a separate opt-in feature requiring sensitive-data
handling, size limits, retention, and indexing decisions. PostgreSQL makes later
JSONB indexing or time partitioning possible, but this implementation neither
captures full bodies nor adds those mechanisms speculatively.

## Alternatives considered

- **SQLite only:** simplest deployment, but retains one local writer and lacks
  the requested independently managed database option.
- **Replace SQLite:** removes a useful zero-service deployment and forces every
  installation to operate PostgreSQL.
- **Separate copied PostgreSQL CRUD:** isolates dialects but duplicates dozens
  of methods and lets behavior drift. Shared SQL with small explicit dialect
  differences keeps fixes and reporting semantics consistent.
- **ORM or general migration framework:** larger persistence rewrite than this
  change requires; handwritten SQL and versioned migrations already fit the app.
- **Full archival, automatic import, or replica coordination now:** distinct
  correctness and operational problems; defer them rather than implying that a
  database driver change solves them.

## Consequences and validation

Operators can use their PostgreSQL service without changing HTTP APIs or losing
SQLite support. The project gains a driver dependency, a second migration
history, and a PostgreSQL integration-test requirement. LAN latency may outweigh
concurrency gains; no throughput improvement is claimed without measurement.
Logging remains asynchronous and best effort during database outages.

Run existing SQLite tests against the shared queries. Add real PostgreSQL
coverage for initialization/reinitialization and competing migrations, identity
and key transactions, sessions and expiry, log filtering and retention, UTC
spend totals, cost overrides, routing state, webhook cascades, Responses jobs,
and reopen persistence. Test URL selection, startup failures, and credential
redaction. Run backend build, vet, and race tests.

## References

- [pgx database/sql adapter](https://github.com/jackc/pgx/wiki/Getting-started-with-pgx-through-database-sql)
- [PostgreSQL advisory locks](https://www.postgresql.org/docs/18/explicit-locking.html#ADVISORY-LOCKS)
- [PostgreSQL TLS connection settings](https://www.postgresql.org/docs/17/libpq-ssl.html)
