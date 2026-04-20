---
phase: 12-schema-foundation
plan: "02"
subsystem: database
tags: [go, storage, sqlite, telemetry, logging, schema]

# Dependency graph
requires: []
provides:
  - "RequestLog struct extended with PoolName, TTFTMs, ReqBodySnippet, RespBodySnippet fields"
  - "LogsFilter struct extended with Provider, PoolName, KeyID, DateFrom, DateTo fields"
  - "Stable Go data contracts for Plans 03+ to build against"
affects:
  - "12-03 (sqlite implementation builds against these structs)"
  - "phase-13 (TTFT population, RespBodySnippet)"
  - "phase-14 (body capture, ReqBodySnippet)"
  - "phase-16 (filter API uses LogsFilter new fields)"

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Empty-string sentinel for string filter fields (Provider, PoolName follow Model pattern)"
    - "Nil-pointer sentinel for optional ID filters (*int64 — KeyID follows TeamID/AppID pattern)"
    - "Nil-pointer for optional time bounds (*time.Time — DateFrom, DateTo)"
    - "Nil-pointer for nullable telemetry (*int64 — TTFTMs)"

key-files:
  created: []
  modified:
    - "internal/storage/storage.go"

key-decisions:
  - "New v1.2 telemetry fields inserted between DeploymentKey and enriched LEFT JOIN fields to preserve struct layout"
  - "Empty-string sentinel used for PoolName/Provider filters, consistent with existing Model field"
  - "*int64 nil-pointer used for TTFTMs (nil = non-streaming or TTFT not yet measured)"
  - "*time.Time nil-pointer used for DateFrom/DateTo (nil = no time bound)"

patterns-established:
  - "Empty-string sentinel: string filter fields use empty string (not pointer) for no-filter state"
  - "Nil-pointer sentinel: ID and time filter fields use pointer types; nil = no filter applied"

requirements-completed:
  - SCHEMA-03
  - SCHEMA-04

# Metrics
duration: 8min
completed: 2026-04-20
---

# Phase 12 Plan 02: Schema Foundation - Storage Structs Summary

**RequestLog and LogsFilter extended with 9 new v1.2 telemetry and filter fields using established Go sentinel patterns**

## Performance

- **Duration:** ~8 min
- **Started:** 2026-04-20T00:00:00Z
- **Completed:** 2026-04-20T00:08:00Z
- **Tasks:** 2
- **Files modified:** 1

## Accomplishments

- Extended `RequestLog` with 4 new telemetry fields: `PoolName string`, `TTFTMs *int64`, `ReqBodySnippet string`, `RespBodySnippet string`
- Extended `LogsFilter` with 5 new filter dimensions: `Provider string`, `PoolName string`, `KeyID *int64`, `DateFrom *time.Time`, `DateTo *time.Time`
- All fields use established sentinel patterns from existing code; `go build ./...` and `go test ./internal/storage/...` both pass

## Task Commits

Each task was committed atomically:

1. **Task 1: Extend RequestLog with 4 new telemetry fields** - `e19ee3e` (feat)
2. **Task 2: Extend LogsFilter with 5 new filter dimensions** - `aaa8c08` (feat)

**Plan metadata:** (docs commit follows)

## Files Created/Modified

- `internal/storage/storage.go` - Added 4 fields to RequestLog, 5 fields to LogsFilter with updated doc comment

## Decisions Made

- Inserted new RequestLog fields between `DeploymentKey` and the enriched fields comment block, preserving struct readability and grouping
- Used `*int64` for `TTFTMs` (pointer nil = TTFT not applicable or not yet measured; non-pointer would require sentinel 0 which is ambiguous)
- Updated LogsFilter doc comment to explicitly note the string empty-string sentinel pattern for new readers

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Plan 03 (sqlite implementation) can now compile against the updated struct definitions
- `LogRequest()` INSERT will need the 4 new columns with nil/zero defaults (Plan 03 task)
- `GetLogs()` SELECT+Scan will need to include the 4 new columns (Plan 03 task)
- `GetLogs()` WHERE builder will need branches for the 5 new LogsFilter fields (Plan 03 task)

## Self-Check

- [x] `internal/storage/storage.go` contains `TTFTMs *int64` in RequestLog
- [x] `internal/storage/storage.go` contains `RespBodySnippet string` in RequestLog
- [x] `internal/storage/storage.go` contains `PoolName string` in RequestLog
- [x] `internal/storage/storage.go` contains `ReqBodySnippet string` in RequestLog
- [x] `internal/storage/storage.go` contains `DateFrom *time.Time` in LogsFilter
- [x] `internal/storage/storage.go` contains `DateTo *time.Time` in LogsFilter
- [x] `internal/storage/storage.go` contains `KeyID *int64` in LogsFilter
- [x] `internal/storage/storage.go` contains `Provider string` in LogsFilter
- [x] `internal/storage/storage.go` contains `PoolName string` in LogsFilter
- [x] Commit `e19ee3e` exists (Task 1)
- [x] Commit `aaa8c08` exists (Task 2)
- [x] `go build ./...` exits 0
- [x] `go test ./internal/storage/...` exits 0

## Self-Check: PASSED

---
*Phase: 12-schema-foundation*
*Completed: 2026-04-20*
