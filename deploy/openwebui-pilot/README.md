# LiteLLM replacement and OpenWebUI pilot

The migration target is a single proxy with separate app keys, direct upstream
credentials, OpenAI-compatible chat completions, streaming, and embeddings.
The proxy already has OIDC administration and SQLite key/usage storage. Keep
Responses expansion, MCP forwarding, and skills forwarding outside this work.

## Work checklist

| Item | Implemented and checked |
| --- | --- |
| Explicit pools | `enabled: false` leaves member aliases available directly; omission preserves existing behavior. |
| Chat compatibility | Preserve unknown request/response fields and explicit nulls, modern token limits, structured schemas, and string stop sequences. |
| Streamed tools | Preserve tool indices and argument fragments; translate native Anthropic tool events and tool selection. Detect incomplete/error streams. |
| Error behavior | Retain upstream HTTP status, JSON error body, and Retry-After. Invalid requests stop retries. |
| Usage | Request upstream stream usage even when the client does not; only expose it on request. Missing usage is recorded and displayed as unknown. |
| Embeddings | Forward dimensions and retain either floating point vectors or base64 strings. |
| Capacity | Enforce concurrent deployment RPM and estimated TPM reservations; known but unavailable models return retryable errors. |
| Migration checks | HTTP fixtures cover all four adapters, app-key issuance/use/attribution/revocation, and admin authentication boundaries. A live smoke script and staged Compose files are included. |

These are compatibility fixes, not proof that every LiteLLM option is supported.
Client model names must be explicitly configured. Preserve existing names such
as `openai/...` or `ollama_chat/...` in `model_name`; the provider selector inside
`litellm_params.model` uses this proxy's provider names, including `ollama/...`.

## Validation record (2026-10-18)

- All Go tests pass with the race detector. Existing suite coverage is 73.6%;
  added production files have 94.1% statement coverage.
- All 191 frontend tests pass, and the production frontend builds.
- Container image: 30,597,001 bytes (about 29.2 MiB); approximately 25 MiB idle
  resident container memory in the smoke run. Health and SPA return 200 with a
  read-only root filesystem. A fresh named volume accepts SQLite writes and
  preserves data when the container is replaced.
- Direct upstream credentials passed sync and streamed chat checks for OpenAI
  `gpt-5.4-mini`, Anthropic `claude-haiku-4-5`, and OpenRouter
  `google/gemini-3.1-flash-lite-preview`. Usage and stream terminators were present.
- Ollama `phi4:latest` passed sync and streamed chat against the actual LAN
  server, with usage and the stream terminator. Its inventory contained 18 models.
- Live tool calls and JSON schemas passed in sync and stream modes for Anthropic
  and OpenRouter. OpenAI JSON schemas and tool calls also pass in both modes.
  GPT-5.4-mini requires `reasoning_effort: none` for function tools on Chat
  Completions; `low` returns an explicit upstream 400. The proxy preserves that
  model restriction rather than changing the client's reasoning setting.
- Both GitHub CI jobs pass. Linux amd64 and local arm64 container images build.
- Real Pocket ID login succeeds, including the callback, persisted session, and
  recognition of the actual administrator. OpenWebUI 0.11.0 also signs in using
  the same Pocket ID browser session. Before cutover, its authenticated
  connection screen confirmed the default OpenAI and host.docker.internal
  Ollama connections.
- The remote sidecar accepts the dedicated app key for all four providers in
  sync and streaming modes. All eight calls report usage and are attributed to
  that key in SQLite. The real OIDC session and app/key records survive the
  database transfer.
- OpenWebUI is now connected to the proxy. Its original image digest, data, and
  OAuth settings are preserved. Model discovery, all four providers, and
  conversation continuation pass in its UI. With user approval, phi4's Builtin
  Tools capability was disabled in its OpenWebUI model profile because phi4
  rejects native tool calls. A fresh UI chat then returned the expected reply,
  and the saved model database reports `builtin_tools: false`.

## Current pilot deployment

The sidecar runs on `docker-services.wagstrom.net` as
`docker-services-simple-llm-proxy-1`, using the locally loaded immutable image
`simple-llm-proxy:pilot-0d0269c` (Linux amd64). It measured about 14 MiB resident
container memory after the live checks, with a 256 MiB limit.

Its deployment files are in `/opt/docker-services/simple-llm-proxy`; the SQLite
volume is `simple-llm-proxy-data`. The host port binds to `127.0.0.1:8088`, and
OpenWebUI reaches `http://simple-llm-proxy:8080/v1` on `proxy-network`. The admin
UI uses the localhost SSH tunnel described below. Resolved environment values
are stored in `.env.proxy` with mode 0600 and are never part of the image.

The host's `openwebui-rollback.json` records the original Compose inputs and
image digest. `openwebui.runtime.override.json` pins that image and supplies only
the proxy connection settings. The local `ansible-config` source also records
the connection URL and the dedicated key's `op://` reference. That focused
infrastructure diff remains uncommitted alongside a new deployment-plan document;
its pre-existing unpublished commits and unrelated files were left untouched.

When transferring a SQLite database, stop the source cleanly, retain a backup,
and copy the database to **`/data/proxy.db` at the volume root**, preserving the
proxy user's ownership. Check app/key records and authenticate with the app key
before cutting over a client; copying a parent directory can silently start a
new empty database instead.

## Compatibility boundaries

OpenAI-compatible adapters retain additional JSON fields rather than silently
discarding them. Acceptance still depends on the upstream model. Anthropic uses
translation: tools, explicit tool selection, parallel-tool control, and JSON
schemas are translated; unsupported `json_object` requests return a clear 400.
Arbitrary OpenAI-only options are not guaranteed on Anthropic.

Deployment quotas are per process, per upstream model/base URL/credential, in
minute windows starting at first use. Aliases sharing those values share usage;
different credentials are independent. Chat TPM reserves serialized message/tool
bytes plus maximum output (default 4096) and reconciles reported usage. Embeddings
reserve serialized input bytes. This is conservative for text, not exact tokenizer
or multimodal metering. Missing usage retains the reservation. These limits are
not distributed across replicas, and Responses TPM estimation is not included.

SQLite usage logs are written asynchronously; do not treat them as a billing
ledger. Back up the database before upgrading: startup adds `usage_known` and
backfills historical records with nonzero token counts. Zero-count historical
rows cannot be distinguished from missing usage and remain unknown.

## Stage the sidecar

The existing `ansible-config` OpenWebUI role supplies its Docker network, data,
and OAuth environment. The running container is `docker-services-openwebui-1`
on `proxy-network`, with its data bind mounted from
`/opt/docker-services/openwebui/data`. Neither the source template nor the
running environment declares OpenAI connection credentials; the running base
URL is empty and persistence is disabled. Inspect the authenticated connection
configuration before cutover rather than assuming an existing LiteLLM URL.
Capture its current URL(s), connection options, and secret references for rollback.

1. Build and tag an immutable image from this branch, then transfer/load it on
   the Docker host or push it to your existing private registry. Do not use a
   floating image tag for the deployed pilot.

   ```sh
   docker build -t simple-llm-proxy:pilot .
   ```

2. Copy this directory's `compose.yaml` and a customized `config.yaml.example`
   as `config.yaml` to a dedicated deployment directory on the Docker host.
   Keep secrets out of YAML. Set `PROXY_IMAGE`, the existing `PROXY_NETWORK`,
   direct provider keys, master key, and OIDC environment using the existing
   Ansible 1Password injection workflow. Its `op://` references belong in the
   source secret template; decrypted temporary files must never be inspected.
   The `.op.env.example` files describe the required variables for local checks
   with `op run` as an alternative.

3. Match `admin_group` to the identity provider's actual claims (`admins` in the
   existing OpenWebUI configuration). Set Ollama's base to its reachable address
   **including `/v1`**, verify `/api/tags`, and enable an explicit installed model.
   Do not substitute a LiteLLM virtual key for an upstream provider credential.

4. With those environment variables injected on the Docker host, validate and
   start only the new sidecar:

   ```sh
   docker compose -f compose.yaml config --quiet
   docker compose -f compose.yaml up -d simple-llm-proxy
   curl --fail http://127.0.0.1:8088/health
   ```

The named volume `simple-llm-proxy-data` owns the database. Never remove it as part
of rollback. The admin port binds only to host loopback; OpenWebUI reaches the
service by Docker DNS on the existing proxy network.

## Login, issue a pilot key, and exercise real providers

Use the existing localhost OIDC callback registration through an SSH tunnel:

```sh
ssh -N -L 5173:127.0.0.1:8088 docker-services.wagstrom.net
```

Open `http://localhost:5173`, complete OIDC login, and verify admin access. The
pilot's `dev_mode: true` permits HTTP cookies for this localhost tunnel; a public
deployment needs HTTPS and `dev_mode: false` with its registered callback.

Create an OpenWebUI app and a dedicated key using the admin UI. Restrict it to
the pilot aliases if desired. Save the one-time key in 1Password and reference
it as `PROXY_API_KEY` for checks and `OPENWEBUI_PROXY_API_KEY` for the service.
The master key is not an admin session and should not be given to OpenWebUI.

From the repository, using a private `pilot.op.env` containing the app-key
reference:

```sh
op run --env-file pilot.op.env -- python3 scripts/migration-smoke.py \
  --base-url http://localhost:5173/v1 \
  --model openai/gpt-5.4-mini \
  --model anthropic/claude-haiku-4-5 \
  --model openrouter/google/gemini-3.1-flash-lite-preview
```

Add the verified Ollama alias with another `--model`. This sends small paid
requests, prints status/usage availability rather than content or credentials,
and exits nonzero on failure. Check logs for key attribution and usage. Models
using hidden reasoning may need a higher `--max-tokens` value.

## Cut over OpenWebUI and roll back

After the live gates pass, include `openwebui.override.yaml` alongside the
existing Compose inputs and the sidecar definition. Inject the dedicated app
key into the Compose environment. Validate the merged configuration with
`config --quiet`, then recreate **only `openwebui`** using
`up -d --no-deps openwebui`. Preserve its existing data mount and OAuth values.
Record the override in the owning Ansible role before the next reconciliation.

The override sets `OPENAI_API_BASE_URLS` to
`http://simple-llm-proxy:8080/v1`, `OPENAI_API_KEYS` to the dedicated app key, and
`ENABLE_PERSISTENT_CONFIG=false` so database settings do not override the
connection. This follows [OpenWebUI's configuration documentation](https://docs.openwebui.com/reference/env-configuration/).

In OpenWebUI, verify model discovery, a streamed conversation on each provider,
conversation continuation, and a tool-using conversation. Check proxy logs and
app attribution. Revoke a disposable test key and confirm immediate rejection;
keep the real pilot key active.

OpenWebUI 0.11 injects built-in tool definitions by default, even for ordinary
chat. For a model without native tool support, such as `phi4:latest`, open
Admin Settings → Models → Edit → Capabilities and disable **Builtin Tools** for
that model, then save and reload the chat. This profile is stored in OpenWebUI's
database. Do not globally disable tools for models that support them or make the
proxy silently strip a client's explicitly requested tools.

Rollback: restore the captured OpenWebUI connection configuration (including
whether it came from environment or persistent settings), remove the override,
and recreate only OpenWebUI with its original Compose inputs. Keep LiteLLM
running throughout the pilot. Retain the proxy volume for inspection; shut down
only the sidecar if needed. A successful sidecar smoke run alone does not justify
retiring LiteLLM or switching every client.

For the deployed pilot, the original Compose file and the prepared image-only
rollback override have both been validated. On the Docker host, this restores
the previous connection environment while retaining the original image:

```sh
docker compose -p docker-services \
  -f /opt/docker-services/docker-compose.openwebui.yml \
  -f /opt/docker-services/simple-llm-proxy/openwebui.rollback-image.json \
  up -d --no-deps openwebui
```

Also revert the focused connection change in the local Ansible source if the
pilot is rolled back, so the next reconciliation retains the chosen connection.
Do not delete `simple-llm-proxy-data`.
