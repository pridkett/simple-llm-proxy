# Usage & Cost

Open **Usage & Cost** (`/#/cost`) as an administrator. The Dashboard (`/#/`)
continues to show proxy health, model deployments, cooldowns, and router settings.

- **Overview** shows stacked usage over time and a ranked breakdown.
- **Explore** adds input/output, cache read/write, and reasoning token details.
- **Budgets** retains active-key spend, configured caps, and budget status.

Calls, known tokens, and dollar spend remain visible together. Select a summary
card to change the chart metric; that preference is remembered in your browser.
Use Group by to compare models, applications, keys, or teams. Sorting works
independently in the table. Large charts show the leading eight groups plus
Other; tables and totals include every group.

Choose inclusive dates in an IANA timezone and hourly, daily, or weekly buckets.
The default seven-day period includes today and the preceding six local dates.
Daily/weekly boundaries follow local calendar time, including daylight-saving
changes. Repeated hours have distinct offsets. Reports are limited to 366 days
and 744 buckets; shorten the period or choose coarser buckets when necessary.

Select a chart bucket (or the accessible bucket dropdown) to explore that exact
interval. Select a breakdown name to filter to that group. **View requests** opens
Logs with the same model/attribution filters and exact UTC interval, with an
exclusive end. Clear the reporting interval in Logs to broaden the search.

## Reading the numbers

A call is one finalized logged logical request, including failures. Retries and
background-job polls do not create extra reporting calls. Pending requests and
requests rejected before logging are outside this report.

Token categories are independent: a provider may report a total without an
input/output breakdown. Unknown measurements show **Unknown**, not zero. Known
subtotals show coverage counts. Cache and reasoning tokens are subsets of input
and output; they are not added to the normalized total a second time. Legacy
Anthropic cache counts are normalized at query time; stored logs are unchanged.

Dollar spend sums amounts selected when requests finished, using upstream reports
or recorded estimates. Missing costs are excluded, and partial costs remain
known subtotals. A recorded zero displays as `$0.0000`; small nonzero costs retain
extra precision. Reported/estimated/mixed source counts accompany the summary.
The per-request accounting record remains the exact decimal audit evidence.

Reports include retained usage from revoked keys and unattributed/master-key
calls. IDs keep separate teams/apps/keys distinct even when names match. Key
budgets still list active keys and include all models in the selected attribution
and time window. Their window totals describe usage in that window; they do not
redefine budget enforcement. The original `/admin/spend` contract is unchanged.

Reports query retained logs; the page shows the configured retention period.
Extending the date range cannot recover already deleted history.

## Reporting API

`GET /admin/usage` requires an administrator session, like `/admin/spend`.

| Parameter | Meaning |
| --- | --- |
| `from`, `to` | Inclusive `YYYY-MM-DD` dates in the selected timezone |
| `timezone` | IANA name; defaults to `UTC` |
| `interval` | `hour`, `day` (default), or `week` (Monday start) |
| `group_by` | `model` (default), `app`, `key`, or `team` |
| `model` | Exact proxy-model alias |
| `team_id`, `app_id`, `key_id` | Positive integer attribution filters |
| `unattributed` | `team`, `app`, or `key` whose joined attribution is missing |
| `since`, `until` | Optional paired RFC3339 timestamps overriding date bounds; `until` is exclusive |

The response includes `summary`, `groups`, sparse `series` indexed by `bucket`,
all `buckets` including empty intervals, `budget_rows`, `alerts`, timezone,
retention days, and exact `from_time`/`to_time` bounds. Each measurement carries
its own known-call count. Unknown measurements use a numeric subtotal of zero
with a known-call count of zero; clients must consult both fields.

Logs additionally accept `date_before` (exclusive RFC3339 upper bound) and
`unattributed`, allowing reporting links to preserve the exact population.
