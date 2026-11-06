# HTTPS MCP gateway

Each operator-registered server gets a native Streamable HTTP endpoint at
`/mcp/{name}`. Configure this URL separately in your MCP client. For Open WebUI,
add an external tool server of type MCP (Streamable HTTP) with Bearer auth. The
gateway uses HTTPS to reach each upstream. Serve the proxy over HTTPS for
remote clients.

## Register a server

See the `mcp` section in [config.yaml.example](../config.yaml.example). Restart
the proxy to apply MCP registry changes.

- **Shared** uses an operator-provisioned key or an admin-connected OAuth
  account. Any caller granted access can use everything the upstream account
  can reach. Give the shared account only the permissions that all its callers
  should have.
- **User** requires each caller to save their upstream API key or connect
  their OAuth account in **MCP Connections** after signing in. A
  separate AES-256 key from `mcp.credential_key` protects upstream credentials
  stored in SQLite or PostgreSQL. Keep it outside the database and back it up.
  After changing it, users must re-enter their upstream keys.
- **None** sends no upstream credential. The gateway still requires a grant.

Use `allowed_app_key_ids` for existing application keys and `allowed_user_ids`
for OIDC subject IDs. The key ID is the numeric ID returned when listing or
creating application keys. A signed-in user can obtain their subject ID from
`GET /admin/me`. An omitted or empty list grants access to nobody.
Personal upstream credentials require a user grant. App keys can use shared
or no-auth registrations. The proxy's master key is excluded from MCP access.

`credential_header` defaults to `Authorization`, which sends the configured
key as a Bearer token. `Api-Key` and supported `X-*` names send the key as
the header value. Use a URL controlled by the operator or a trusted provider.
The relay uses the configured destination and drops client-supplied
authentication, cookies, and routing headers.

## Connect a client

For a user grant, open **MCP Connections**, enter an upstream key if required,
and issue a gateway token. Copy it into the client as a Bearer token. The
page displays it once. Each user has one gateway token per server, expiring
after 30 days. Issuing a replacement revokes the previous token immediately
for new requests. Disconnect removes the stored upstream key and revokes the
token. An Open WebUI connection with a single configured bearer key uses one
gateway identity for every caller of that integration.

For an application key grant, use the existing `sk-app-…` key as the Bearer
credential. Requests count against its configured RPM and RPD limits.
MCP is a separate service from model inference and has no token-spend
accounting.

The relay supports JSON and SSE responses, including legacy Streamable HTTP
sessions and modern per-request metadata. Legacy session handles expire after
one hour or a proxy restart. Requests are limited to 2 MiB, and individual
requests and streams time out after 110 seconds. Use a client and upstream
with a compatible MCP revision. Managed local servers are tracked separately.

## Connect an OAuth upstream

Register the proxy as an OAuth client with the upstream provider. Add an
`oauth` block under a `user` or `shared` registration. The callback must use
the proxy's public HTTPS origin and the exact path shown here.

```yaml
mcp:
  credential_key: os.environ/MCP_CREDENTIAL_KEY
  servers:
    reports:
      url: https://tools.example.org/mcp
      credential_scope: user
      allowed_user_ids: ["oidc-subject"]
      oauth:
        issuer: https://identity.example.org
        client_id: registered-client-id
        client_secret: os.environ/MCP_REPORTS_CLIENT_SECRET
        redirect_url: https://proxy.example.org/api/mcp/oauth/callback/reports
        scopes: [reports:read]
```

The provider must support authorization code with S256 PKCE, a pre-registered
public client or confidential client using `client_secret_basic`, and MCP
protected-resource discovery. The authorization and token endpoints must
be on the configured issuer's HTTPS origin. The optional `resource` and
`resource_metadata_url` settings can override the defaults for providers
that publish a different canonical resource or metadata path on the same
origin as the MCP server. Provider discovery requests use fixed HTTPS
origins and disallow redirects.

For personal OAuth, each granted user selects **Connect upstream** in MCP
Connections and then issues a gateway token. For shared OAuth, set
`credential_scope: shared`, omit `shared_key`, and assign the caller grants.
A proxy admin connects the designated upstream account. Admin access to
manage that connection is separate from permission to call the MCP endpoint.

The proxy stores access and refresh tokens encrypted. It refreshes expiring
tokens when the provider supplies a refresh token. Use **Reconnect upstream**
if the provider rejects the grant. Disconnect removes the proxy's stored
grant and revokes the personal gateway token, when applicable. Revoke the
grant at the provider if you also want to end it there. Native OAuth sign-in
to the gateway itself and the client-credentials grant are follow-up work.
