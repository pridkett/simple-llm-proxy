---
phase: 02-api-keys-enforcement
plan: "01"
subsystem: storage
tags: [api-keys, sqlite, migrations, storage-interface]
dependency_graph:
  requires: []
  provides: [APIKey storage types, key CRUD interface, migrations 11-13]
  affects: [02-02, 02-03, 02-04, 02-05]
tech_stack:
  added: []
  patterns: [transactional INSERT with allowlist, soft-delete via is_active=FALSE, nil/nil not-found pattern]
key_files:
  created:
    - internal/storage/sqlite/apikeys.go
    - internal/storage/sqlite/apikeys_test.go
  modified:
    - internal/storage/storage.go
    - internal/storage/sqlite/migrations.go
    - internal/api/handler/models_test.go
    - internal/api/handler/auth_test.go
    - internal/api/middleware/session_test.go
decisions:
  - RecordKeySpend implemented as no-op stub (Plan 04 will wire logRequest path)
  - allowedModels=nil or empty slice = all models allowed (enforced at query time in Plan 03)
  - is_active=FALSE for revoke (soft delete for audit trail)
metrics:
  duration: "~4 minutes"
  completed: "2026-03-26"
  tasks: 2
  files: 7
---

# Phase 02 Plan 01: API Key Storage Foundation Summary

## One-Liner

Full Phase 2 API key schema (application-scoped keys, budget/rate limits, model allowlists) with SQLite migrations 11-13 and transactional CRUD via extended Storage interface.

## What Was Built

### storage.go Extensions

- `APIKey` struct: `ID`, `ApplicationID`, `Name`, `KeyPrefix`, `KeyHash` (JSON:`-`), `MaxRPM`, `MaxRPD`, `MaxBudget`, `SoftBudget`, `IsActive`, `CreatedAt`
- `APIKeyAllowedModel` struct: `KeyID`, `ModelName`
- `RequestLog.APIKeyID *int64` field: nil when master key, non-nil when app key used
- 6 new Storage interface methods: `CreateAPIKey`, `GetAPIKeyByHash`, `ListAPIKeys`, `RevokeAPIKey`, `GetKeyAllowedModels`, `RecordKeySpend`

### migrations.go Additions (11-13)

- **Migration 11**: DROP legacy placeholder `api_keys` table, CREATE full Phase 2 schema with `application_id` FK referencing `applications(id) ON DELETE CASCADE`, `key_prefix`, `key_hash UNIQUE`, optional rate/budget limits
- **Migration 12**: `key_allowed_models` table — composite PK `(key_id, model_name)`, FK to `api_keys(id) ON DELETE CASCADE`
- **Migration 13**: Indexes `idx_api_keys_application_id` and `idx_api_keys_key_hash` for hot-path lookups

### apikeys.go Implementation

- `CreateAPIKey`: begins transaction, INSERTs key row with RETURNING, INSERTs each allowlist entry, commits atomically
- `GetAPIKeyByHash`: returns `(nil, nil)` on `sql.ErrNoRows` — callers check nil before proceeding
- `ListAPIKeys`: `ORDER BY created_at DESC`
- `RevokeAPIKey`: `UPDATE is_active = FALSE`, returns error if no rows affected
- `GetKeyAllowedModels`: returns empty slice (not nil) when no entries — empty = all models allowed
- `RecordKeySpend`: no-op stub — deferred to Plan 04's `logRequest` integration

## Tests

7 new tests in `apikeys_test.go` — all passing:

- `TestAPIKeySchema`: verifies `api_keys` and `key_allowed_models` tables created
- `TestCreateAPIKey`: full field verification including nullable limits and allowlist
- `TestGetAPIKeyByHash`: found/not-found cases
- `TestListAPIKeys`: multi-key listing
- `TestRevokeAPIKey`: is_active=false, key preserved, non-existent key errors
- `TestGetKeyAllowedModels`: with and without allowlist
- `TestRecordKeySpend`: no-op stub returns nil

No regressions in existing 14 storage tests.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Test mock Storage implementations missing new interface methods**
- **Found during:** Task 2 verification (`go test ./...`)
- **Issue:** Three test mock types (`mockStorage`, `mockSessionStorage`, `mockAuthStore`) in handler and middleware tests did not implement the 6 new Storage interface methods, causing build failures
- **Fix:** Added no-op stub implementations for all 6 new methods to each mock
- **Files modified:** `internal/api/handler/models_test.go`, `internal/api/handler/auth_test.go`, `internal/api/middleware/session_test.go`
- **Commit:** 044c3cf (included in Task 2 commit)

### Pre-existing Issues (Out of Scope)

- `TestTeamMembers/TestAdminAddTeamMember` and `TestAdminUpdateTeamMemberRole` were already failing on `main` before this plan (expected 201/200, got 204). Not caused by this plan's changes. Logged to deferred-items.

## Known Stubs

| Stub | File | Reason |
|------|------|--------|
| `RecordKeySpend` always returns nil | `internal/storage/sqlite/apikeys.go:105` | Spend recording is integrated via `logRequest()` in Plan 04; this stub satisfies the interface until then |

## Self-Check: PASSED

- `internal/storage/storage.go` — FOUND
- `internal/storage/sqlite/apikeys.go` — FOUND
- `internal/storage/sqlite/migrations.go` — FOUND (migrations 11-13 present)
- Commit `778f900` (RED tests) — FOUND
- Commit `2d638c6` (Task 1 storage types) — FOUND
- Commit `044c3cf` (Task 2 implementation) — FOUND
- `go build ./...` — PASSES
- `go test ./internal/storage/...` — 21 tests PASS (14 existing + 7 new)
