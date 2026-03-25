# Codebase Concerns

**Analysis Date:** 2026-03-25

## Tech Debt

**Silent Error Handling in Stream Event Parsing:**
- Issue: Anthropic stream event parsing skips malformed events silently instead of reporting errors
- Files: `internal/provider/anthropic/anthropic.go` (line 246)
- Impact: Clients may never know that stream chunks were lost/skipped. Debugging issues becomes difficult when chunks silently disappear.
- Fix approach: Either log malformed events or send them as error messages to allow clients to detect data loss

**Unused Legacy Completions Handler:**
- Issue: `internal/api/handler/completions.go` implements a placeholder endpoint that always returns "not implemented"
- Files: `internal/api/handler/completions.go`
- Impact: Dead code path; no clear migration path for clients still using legacy completions API
- Fix approach: Either implement the endpoint fully or remove it entirely. If removed, document migration guide for users.

**Logging Request Errors Discarded in Goroutine:**
- Issue: In `internal/api/handler/chat.go` (line 97), request logging is spawned as a fire-and-forget goroutine with no error handling
- Files: `internal/api/handler/chat.go` (line 154-171)
- Impact: If logging fails (database connection lost, insert error), the error is silently lost. No observability into logging failures.
- Fix approach: Add error logging inside the `logRequest` goroutine, or implement retry/deadletter mechanism for failed logs

**JSON Encoding Errors Not Logged in Admin API:**
- Issue: Multiple handlers use `json.NewEncoder(w).Encode()` without checking for encoding errors
- Files: `internal/api/handler/admin.go` (lines 46, 106, 129, 203)
- Impact: If encoding fails (malformed data, writer error), response is incomplete but client never knows
- Fix approach: Check `Encode()` return value and log errors

**Missing Error Check in Response Body Read:**
- Issue: `internal/provider/openai/openai.go` (line 116) reads error response body but ignores the error with `respBody, _ := io.ReadAll(resp.Body)`
- Files: `internal/provider/openai/openai.go` (line 116)
- Impact: If body read fails, error details may be lost; error message sent to client will be empty
- Fix approach: Check error and provide fallback message

**Malformed JSON Silently Ignored in Costmap:**
- Issue: `internal/costmap/costmap.go` (line 166) comment states "Unknown or non-numeric values are silently ignored (zero value used)"
- Files: `internal/costmap/costmap.go` (lines 167-180)
- Impact: Invalid cost data is silently converted to zeros, making it impossible to detect data corruption or schema changes in upstream cost map
- Fix approach: Log warnings when unexpected values are encountered, track count of dropped/converted fields

## Known Bugs

**Potential Channel Leak in Streaming Error Path:**
- Symptoms: If both providers (OpenAI and Anthropic) experience unmarshaling errors while streaming, the `errs` channel receives the error but no reader consumes it
- Files: `internal/provider/openai/openai.go` (lines 157), `internal/provider/anthropic/anthropic.go` (line 225)
- Trigger: Receive malformed stream chunk from provider while client is reading from stream
- Workaround: Client disconnection closes the context and goroutine exits
- Impact: Minor memory leak (buffered channels with 1 error) but not critical; should still be fixed for cleanup

**JSON Decode Errors Not Checked in Config Loader:**
- Symptoms: Config parsing may fail silently if YAML contains invalid types
- Files: `internal/config/loader.go` (lines 33-34)
- Trigger: YAML file with type mismatches (e.g., port as string instead of int)
- Workaround: Manual verification of config file format
- Impact: Server may start with partial/incorrect configuration

## Security Considerations

**Plaintext API Keys in HTTP Headers:**
- Risk: OpenAI/Anthropic API keys are passed in HTTP Authorization headers. While these should only be used over HTTPS in production, there's no mechanism to enforce HTTPS or warn if running over HTTP.
- Files: `internal/provider/openai/openai.go` (line 61), `internal/provider/anthropic/anthropic.go` (line 144)
- Current mitigation: Assumption that deployment is behind HTTPS reverse proxy
- Recommendations:
  - Add startup warning if server runs on non-HTTPS protocol (though HTTP is valid for localhost)
  - Document requirement to run behind HTTPS-terminating reverse proxy in production

**Master Key in Plain Text in Config:**
- Risk: Master key is stored and transmitted as plain text in YAML config file
- Files: `internal/config/loader.go` (line 72)
- Current mitigation: `os.environ/` syntax allows pulling from environment variables
- Recommendations:
  - Document that secrets should always use `os.environ/VAR` syntax
  - Consider rejecting empty master key at startup with fatal error

**No Rate Limiting on Admin Endpoints:**
- Risk: Admin endpoints (status, config, reload) have no rate limiting; attackers can flood with requests
- Files: `internal/api/router.go`
- Current mitigation: Master key authentication (but no brute-force protection)
- Recommendations: Add rate limiting per API key, implement exponential backoff for failed auth attempts

## Performance Bottlenecks

**SQLite Database Unbounded Log Growth:**
- Problem: `usage_logs` table grows indefinitely with no retention policy
- Files: `internal/storage/sqlite/sqlite.go`, `internal/storage/sqlite/migrations.go`
- Cause: LogRequest inserts every request but no cleanup mechanism exists
- Impact: Database file will grow without bound; queries will slow as table grows
- Improvement path:
  1. Add PRAGMA settings to limit journal size
  2. Implement retention policy (e.g., DELETE logs older than 90 days)
  3. Add vacuum operation on startup
  4. Consider creating archive table before deletion

**Unbuffered Channel in Provider Streams:**
- Problem: `chunks := make(chan *model.StreamChunk)` creates unbuffered channel
- Files: `internal/provider/openai/openai.go` (line 124), `internal/provider/anthropic/anthropic.go` (line 211)
- Cause: Unbuffered channels require sender and receiver to synchronize
- Impact: If client reads slowly, goroutine blocks writing chunks, potentially timing out provider response
- Improvement path: Add buffer of 10-100 chunks to decouple reader/writer; provides breathing room for slow clients

**Cost Map Loaded Synchronously on Startup:**
- Problem: Cost map load is fire-and-forget goroutine with 60-second timeout
- Files: `cmd/proxy/main.go` (lines 74-81)
- Cause: Blocking startup for external resource
- Impact: Server starts without cost data if CDN is slow; all requests get zero cost until load completes
- Improvement path: Add startup flag to make cost map loading blocking or optional; log warning if initial load fails

## Fragile Areas

**Router Deployment Selection Under Race Conditions:**
- Files: `internal/router/router.go` (lines 65-88)
- Why fragile: `GetDeployment` filters out cooldown deployments but doesn't hold lock during strategy.Select() call; deployments could change between filter and select
- Safe modification: Use read lock throughout selection, or return pre-filtered list atomically
- Test coverage: Limited coverage of concurrent GetDeployment calls with cooldown changes

**Cooldown Manager State Inconsistency:**
- Files: `internal/router/cooldown.go` (lines 30-49)
- Why fragile: `InCooldown` method acquires lock, checks, releases, then acquires again to delete. Race condition window exists where cooldown expires between check and cleanup.
- Safe modification: Redesign to check expiry and cleanup in single locked section
- Test coverage: No concurrent stress tests for cooldown expiry edge cases

**Concurrent Modification of Costmap Overrides:**
- Files: `internal/costmap/costmap.go` (lines 61-62)
- Why fragile: `overrideKeys` and `customSpecs` maps are accessed with RWMutex but map iteration during status retrieval is not atomic with mutations from SetOverrideKey/SetCustomSpec
- Safe modification: Use concurrent map or redesign to return snapshot
- Test coverage: No tests for concurrent Set/Get operations

**Provider HTTP Client Reuse Without Timeout Context:**
- Files: `internal/provider/openai/openai.go` (lines 55, 100, 178), `internal/provider/anthropic/anthropic.go` (lines 186)
- Why fragile: Providers use same http.Client for all requests but timeout is context-based. If context doesn't have timeout, request could hang indefinitely.
- Safe modification: Set explicit timeouts on http.Client (30-60 seconds) as fallback
- Test coverage: No timeout failure tests

## Scaling Limits

**SQLite as Single-Writer Bottleneck:**
- Current capacity: SQLite WAL mode supports concurrent reads, but writes are serialized
- Limit: With high request volume (>1000 req/sec), database becomes bottleneck; WAL write contention causes queueing
- Scaling path:
  1. Implement batch logging (buffer N requests, write as batch)
  2. Add async queue with background flush worker
  3. For extreme scale, switch to PostgreSQL or push logs to external system (e.g., Datadog)

**No Request Rate Limiting:**
- Current capacity: Go http.Server default is 128 concurrent connections (SOMAXCONN), then requests queue
- Limit: Under sustained >5000 req/sec load, server may become unresponsive
- Scaling path:
  1. Add rate limiting middleware per API key
  2. Implement token bucket or sliding window rate limiter
  3. Configure rpm/tpm enforcement at deployment level (currently parsed but not enforced)

**Cost Map Download Size:**
- Current capacity: Full cost map is loaded into memory as map[string]ModelSpec
- Limit: LiteLLM cost map JSON is ~500KB; with frequent reloads, memory pressure increases
- Scaling path:
  1. Implement incremental/delta updates instead of full replacement
  2. Cache cost map file locally with etag validation
  3. Lazy-load models on-demand rather than all-at-once

## Dependencies at Risk

**Anthropic API Version Hardcoded:**
- Risk: `anthropicVersion = "2023-06-01"` is hardcoded; Anthropic may deprecate older versions
- Files: `internal/provider/anthropic/anthropic.go` (line 20)
- Impact: Provider stops working when Anthropic retires old API version
- Migration plan:
  1. Add version config setting to allow override
  2. Add automatic version detection via API response headers
  3. Implement version compatibility layer

**LiteLLM Cost Map URL as Single Source of Truth:**
- Risk: Dependency on external GitHub-hosted JSON file; no fallback if GitHub is down
- Files: `internal/costmap/costmap.go` (line 15)
- Impact: Cost data becomes stale; requests without cost data default to $0
- Migration plan:
  1. Implement local cost map cache with file fallback
  2. Add alternate cost map URLs for failover
  3. Use database to persist known-good cost map snapshot

**Go stdlib HTTP Client Version Compatibility:**
- Risk: Providers use bare http.Client with no timeout configuration; may behave differently across Go versions
- Files: `internal/provider/openai/openai.go` (line 37), `internal/provider/anthropic/anthropic.go` (line 40)
- Impact: Requests may hang indefinitely if client doesn't set timeout
- Migration plan:
  1. Set explicit Timeout on http.Client
  2. Set MaxConnsPerHost and MaxIdleConnsPerHost
  3. Implement circuit breaker for repeated failures

## Test Coverage Gaps

**No Tests for Streaming Errors:**
- What's not tested: What happens when stream chunks are malformed, or provider returns error mid-stream
- Files: `internal/provider/openai/openai.go` (ChatCompletionStream), `internal/provider/anthropic/anthropic.go` (ChatCompletionStream)
- Risk: Stream error handling code paths are untested; client may receive incomplete responses or hangs
- Priority: High - streaming is a critical path

**No Tests for Cooldown Expiry Race Conditions:**
- What's not tested: Concurrent calls to InCooldown while cooldown expires; concurrent ReportFailure during cooldown check
- Files: `internal/router/cooldown.go`
- Risk: Race conditions causing inconsistent failure counts or double-entry into cooldown
- Priority: Medium - affects failover behavior under load

**No Tests for Config Reload Under Load:**
- What's not tested: Concurrent requests to different endpoints while config is being reloaded
- Files: `internal/api/handler/admin.go` (AdminReload), `internal/router/router.go` (Reload)
- Risk: Requests may see inconsistent router state during reload
- Priority: Medium - affects operational safety

**No Tests for Logging Failures:**
- What's not tested: What happens when database insert fails, connection timeout, etc.
- Files: `internal/api/handler/chat.go` (logRequest function)
- Risk: Log failures are silently dropped; no way to detect data loss
- Priority: Low - doesn't affect request path, only observability

**No Tests for HTTP Header Edge Cases:**
- What's not tested: Missing Authorization header, malformed bearer token, missing Content-Type
- Files: `internal/provider/openai/openai.go`, `internal/provider/anthropic/anthropic.go`
- Risk: Unclear error messages sent to client when headers are malformed
- Priority: Low - header validation typically done upstream

---

*Concerns audit: 2026-03-25*
