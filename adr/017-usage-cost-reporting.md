# ADR 017: Usage and Cost Reporting

**Status:** Accepted
**Date:** 2026-11-03
**Design issue:** pridkett/simple-llm-proxy#123
**Implementation issue:** pridkett/simple-llm-proxy#124

## Context

The Dashboard currently answers health questions. Cost has spend totals, budget
alerts, and per-model token counts, but cannot explain usage trends across paid,
free, and local models. Calls and tokens must be first-class reporting metrics;
dollar spend must remain visible regardless of the chart metric.

Accounting now preserves independent nullable token categories and cost evidence.
The original spend API's active-key inner joins omit revoked and unattributed
traffic. Its projected token columns cannot represent a known total whose input
and output breakdown is unknown. Reusing those numbers without their evidence
would misleadingly turn missing measurements into free calls or zero tokens.

## Decision

### Product and navigation

Keep `/` as the health Dashboard. Rename the existing `/cost` navigation entry to
Usage & Cost. Provide Overview (stacked time series and ranked breakdown) and
Explore (sortable detailed breakdown), sharing the same report and filters.
Always display calls, known tokens, and dollar spend summary cards. Default the
chart to calls and remember the selected metric. Keep key budgets and alerts in
the page through the existing spend API; reporting does not change enforcement.

Support model, application, key, and team grouping; date presets and custom
inclusive dates; IANA timezone selection; and hourly, daily, or weekly buckets.
Clicking a series/breakdown row adds an explicit filter; clicking a time bucket
narrows the interval. Links to Logs preserve the exact UTC interval and dimensions.
Controls and tables must remain usable on narrow screens and in both themes.

### Reporting API and storage

Add admin-only `GET /admin/usage`, leaving `/admin/spend` compatible for key
management and budget badges. Bound report intervals and bucket counts. Validate
all dimensions, dates, IDs, timezone names, and bucket sizes before querying.
Return one response containing summary, time buckets, grouped rows, coverage,
source counts, and exact UTC bounds for drill-down.

Use the shared SQL store for SQLite and PostgreSQL. Construct bounded UTC start/end intervals in Go for the selected timezone and
calendar granularity, then join these intervals through a SQL VALUES CTE and
aggregate by bucket and dimension. This handles DST and fractional timezone
offsets without requiring SQLite timezone extensions or loading individual
request bodies. Hour buckets retain UTC identity to distinguish repeated hours. The same filtered base
supplies summaries and breakdowns; empty buckets are filled explicitly.

Use left joins from usage logs to attribution and accounting. Include revoked
keys and logs without a key. Missing attribution is a separately identifiable
Unattributed group, and grouping uses IDs rather than names. Exclude `_flush`.
Count finalized logged logical requests once; attempts, job polls, and pending
records are not extra calls. Include failed logged requests and their known cost.

### Measurement semantics

Prefer normalized accounting evidence, including authoritative total-only token
reports. Preserve null fields instead of falling back to projected zeros on
accounted requests. Cache and reasoning fields are subsets of input/output and
are never added to normalized totals. For legacy logs, use usage-known evidence;
normalize Anthropic's separately stored cached input for reporting without
rewriting historical rows or repricing them. Expose legacy record counts.

Sum selected costs recorded at request time. Exclude unknown cost from the known
subtotal, retain measured zero, and expose complete/partial/unknown coverage plus
upstream/estimated/mixed/legacy source counts. Format money with enough precision
to show small nonzero costs. Show Unknown when a group has no known measurements,
and label partial totals explicitly. Accounting records remain the exact decimal
audit evidence; aggregate money follows existing numeric reporting conventions.

### Scope and validation

No pricing changes, model calls, migration, or persistent rollup table is required.
Query retained logs, disclose the configured retention window, and avoid
comparison deltas when equivalent previous-period evidence is unavailable.

Test SQL behavior for both supported backends, unknown versus zero, authoritative
total-only usage, cache normalization, retries, historical attribution, duplicate
names, filters, and interval boundaries. Test API access and malformed queries,
DST and calendar buckets, and frontend metric switching, persistent dollars,
sorting, stale responses, empty/error states, and Logs drill-down. Run the
frontend production build and inspect the rendered page at desktop/mobile widths.

## Consequences

Reporting and health remain separate, free models remain visible, and dollar
spend never disappears when another metric is selected. Existing budget clients
continue using their established endpoint. The portable interval aggregation
adds a small amount of server-side bucketing work; report bounds limit its size.
Longer history still depends on configured log retention; durable rollups can be
introduced separately if that requirement emerges.
