# Management MCP server

Connect a Streamable HTTP MCP client to `/api/v1/mcp`. The server uses the
proxy's configured SQLite or PostgreSQL database. Restart the proxy after
changing management MCP authentication.

## Local access

Set `local_admin.enabled: true` and leave `oidc_settings.issuer_url` unset.
The proxy binds to `127.0.0.1`. At the default port, connect to
`http://127.0.0.1:8080/api/v1/mcp` with no authorization header. Local clients
can use all tools. Keep the local admin listener on your machine.

Simple Agent Tracer already has the corresponding loopback behavior. With
its default loopback bind and no OIDC issuer, connect to
`http://127.0.0.1:4318/api/v1/projects/default/mcp` without credentials.
See the [tracer's configuration](https://github.com/pridkett/simple-agent-tracer#configuration).

## Remote access

Serve the endpoint over HTTPS. Configure one or more of the following
settings. Remote access is opt-in.

```yaml
mcp:
  management:
    read_token: os.environ/PROXY_MCP_READ_TOKEN
    write_token: os.environ/PROXY_MCP_WRITE_TOKEN
    oauth: true
```

For static bearer auth, generate each configured token separately with
`printf 'sk-proxy-mcp-%s\n' "$(openssl rand -hex 32)"` and set the corresponding
environment variable. Configure the client to send
`Authorization: Bearer <token>`. A read token can query deployment-wide usage
and list teams, applications, and keys. A write token also enables key
creation. Omit the write token for a read-only static setup.

For OAuth, set `oauth: true` and configure the HTTPS OIDC issuer, client ID,
and callback in `oidc_settings`. With a callback of
`https://proxy.example.com/auth/callback`, the canonical resource is
`https://proxy.example.com/api/v1/mcp`. Route both the MCP endpoint and
`/.well-known/oauth-protected-resource/api/v1/mcp` through the HTTPS reverse
proxy. The server advertises the issuer in protected resource metadata.

With Pocket ID, register an API for the canonical resource URL and define
the `proxy:read` and `proxy:keys:write` permissions. Grant `proxy:read` to
the clients and users allowed to see deployment-wide usage and app key
metadata. Grant both permissions to users and clients allowed to issue app
keys. Register each connector's callback and enable PKCE, or configure
Client ID Metadata Documents and their grants in Pocket ID. Configure the
client with the exact MCP URL. The issuer must provide a signed JWT access
token with type `at+jwt` and an audience containing the canonical resource.
See the [tracer's OAuth guide](https://github.com/pridkett/simple-agent-tracer/blob/main/QUERY-OAUTH.md)
and [Pocket ID's API grants guide](https://pocket-id.org/docs/guides/apis).

## Tools

| Tool | Arguments and result |
| --- | --- |
| `query_usage` | Query calls, failures, known tokens, and USD spend. Filter by `model`, `team_id`, `app_id`, `key_id`, exact `status_code`, or missing attribution (`unattributed`). Group by `model`, `team`, `app`, `key`, or `status`. |
| `list_teams` | List team names and IDs. |
| `list_applications` | Pass `team_id` to look up its applications. |
| `list_keys` | Pass `app_id` to look up key IDs, prefixes, allowed models, limits, and expirations. |
| `create_key` | Pass `app_id` and `name`. Optionally set `allowed_models`, `max_rpm`, `max_rpd`, `max_budget`, `soft_budget`, `expires_at`, or `allow_decisions`. Save the returned `key` securely. This is the only response containing the full secret. |

For example, ask for Team 2's 429 responses by app and week:

```json
{
  "from": "2026-11-01",
  "to": "2026-11-14",
  "team_id": 2,
  "status_code": 429,
  "group_by": "app",
  "interval": "week"
}
```

Use inclusive dates and an IANA `timezone`, or pass paired RFC3339
`since` and `until` timestamps for a half-open window. The default is seven
days in UTC with daily buckets. The response includes coverage counts,
totals, group subtotals, and a sparse series indexed into the bucket list.
See the [reporting guide](usage-reporting.md) for metric definitions and
retention behavior. Queries support up to 366 days, 744 buckets, and 500
aggregate rows. If a result is too large, narrow the filters or choose a
coarser interval to get a complete report.
