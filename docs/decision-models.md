# Decision models

The proxy supports typed decisions through `POST /v1/decisions` and
`POST /v1/systemone`, with `/decisions` and `/systemone` aliases. Both use normal
Bearer key authentication. See [ADR 016](../adr/016-decision-models.md) and
[ADR 019](../adr/019-decision-provider-dialects.md) for the design and source
references.

| Upstream | `decision.protocol` | Default `api_base` (by provider prefix) |
| --- | --- | --- |
| OpenAI Decisions (`gpt-6-luna`) | `decisions` | `openai/` → `https://api.openai.com/v1` |
| Gemini Decisions (`gemini-decision-1`) | `gemini` | `gemini/` → `https://generativelanguage.googleapis.com/v1beta` |
| Ollama (Nimble, Tev, Clef) | `ollama` | `ollama/` → `http://localhost:11434/v1` |
| TypeSafe Jev, Laya, other System One servers | `systemone` | `typesafe/` → `https://api.typesafe.ai/v1` |
| Granite Switch on vLLM | `systemone` or `decisions` | none; set `api_base` |
| Cloudflare Workers AI (Clef) | `cloudflare` | none; set `api_base` and `path` |

Every row is reachable from both public routes.

## Declare deployments

Add `decision` to a `model_list` entry. Its protocol is independent of the prefix
in `litellm_params.model`; a compatible self-hosted server needs configuration
rather than a provider plugin. Use separate aliases for chat and decisions even
when both use the same underlying model. Mixing the two operations under one
alias is rejected at startup/reload.

```yaml
model_list:
  - model_name: luna-decisions
    litellm_params:
      model: openai/gpt-6-luna
      api_key: os.environ/OPENAI_API_KEY
    decision:
      protocol: decisions
      modalities: [text, image]
      pricing:
        input_cost_per_token: 0.0000001
        cache_read_input_token_cost: 0
        cache_creation_input_token_cost: 0
    rpm: 100

  - model_name: jev-vision
    litellm_params:
      model: typesafe/jev-1.14.0
      api_key: os.environ/TYPESAFE_API_KEY
      extra_headers:
        TypeSafe-Danger-Zone-Images: "true"
    decision:
      protocol: systemone
      modalities: [text, image]
      question_types: [predicate, choice, score, box]
      pricing:
        input_cost_per_token: 0.0000001 # example: set your contracted USD rate

  - model_name: gemini-decisions
    litellm_params:
      model: gemini/gemini-decision-1
      api_key: os.environ/GEMINI_API_KEY
    decision:
      protocol: gemini
      modalities: [text, image, audio, video]
      pricing:
        input_cost_per_token: 0.0000001 # output tokens are not charged

  - model_name: nimble-local
    litellm_params:
      model: ollama/nimble # no api_key: local Ollama needs none
    decision:
      protocol: ollama
      extra_body:
        keep_alive: 30m
      pricing: {}

  - model_name: clef-cloud
    litellm_params:
      model: ollama/clef
      api_base: https://ollama.com/v1
      api_key: os.environ/OLLAMA_API_KEY
    decision:
      protocol: ollama
      modalities: [text, image]
      pricing:
        input_cost_per_token: 0.0000001 # example: set your plan's USD rate

  - model_name: granite-decisions
    litellm_params:
      model: vllm/granite-switch # the server's SERVED_MODEL_NAME
      api_base: http://localhost:8000/v1
      # api_key: os.environ/VLLM_API_KEY # when vLLM runs with --api-key
    decision:
      protocol: systemone
      extra_body:
        cache_salt: os.environ/GRANITE_CACHE_SALT
      pricing: {} # explicitly free/self-hosted; does not estimate GPU rental costs

  - model_name: laya-local
    litellm_params:
      model: laya/english # use your server's checkpoint name; also works with compatible Ollaya servers
      api_base: http://localhost:8001/v1
      api_key: os.environ/LAYA_API_KEY
    decision:
      protocol: systemone
      pricing: {}

  - model_name: clef-omni
    litellm_params:
      model: cloudflare/clef-omni
      api_base: os.environ/CLOUDFLARE_AI_BASE # https://api.cloudflare.com/client/v4/accounts/<account-id>/ai/run
      api_key: os.environ/CLOUDFLARE_API_TOKEN
    decision:
      protocol: cloudflare
      path: /@cf/cloudflare/clef-omni
      modalities: [text, image, audio, video]
      pricing:
        input_cost_per_token: 0.00000015
```

The `openai`, `typesafe`, `gemini`, and `ollama` prefixes have the default base
URLs in the table above; other prefixes require `api_base`. The default path is
`/decisions` (`decisions`, `gemini`) or `/systemone` (`systemone`, `ollama`),
appended to that base. An optional `path` overrides it. The prefix only picks a
default origin: the protocol, not the prefix, decides the wire format. Cloudflare requires an explicit model path
and unwraps the Workers AI `success`/`result` envelope. It does not auto-select a
model from the prefix.

`modalities` defaults to `[text]`; supported entries are `text`, `image`, `audio`,
and `video`. `question_types` defaults to `[predicate, choice, score]`; use
`predicate` in configuration for both wire formats' yes/no type. Add `box` only
when the deployment supports it. Declarations describe the actual server's
capabilities; its own model-specific size/context limits still apply.

Prices above reflect the basic OpenAI and Cloudflare rates checked on
2026-10-21, without regional premiums or long-context multipliers. Jev's rate is
illustrative. Set actual contracted rates for every deployment. See
[OpenAI pricing for Decisions](https://developers.openai.com/api/docs/guides/decisions)
and [Clef-omni](https://developers.cloudflare.com/workers-ai/models/clef-omni/).

`extra_body` adds operator-owned JSON fields to every upstream request for that
deployment, on either public route, after translation. Use it for settings the
upstream reads from the body, such as Granite Switch's `cache_salt` or Ollama's
`keep_alive`. A deployment value replaces a client field of the same name.
Top-level string values support `os.environ/VAR`. Fields the proxy builds
(`model`, `input`, `state`, `questions`, media, `stream`, `background`) are
rejected at load, and the block is never returned by model discovery or
`/admin/config`.

### Gemini

`protocol: gemini` sends Google's native request and authenticates with
`x-goog-api-key`. From `/v1/systemone`, `state` becomes text (structured state as
compact JSON), followed by `images`, `audio`, and `videos` as typed blocks; noul
`{"true": ..., "false": ...}` criteria become `true_description` /
`false_description`. From `/v1/decisions`, user messages become ordered text and
image blocks and `input_json` parts become JSON text. A transcript with other
roles is sent as one JSON text block, and is rejected if it also carries images,
because Gemini input has no roles.

Embedded media needs a real type: use a data URL, or bare base64 that the proxy
can recognise from its leading bytes. An `https://` URL is forwarded as a `uri`
without being fetched when its extension identifies the type. Question names
must match `[A-Za-z0-9_-]{1,64}`. Responses gain `model` and `usage.input_tokens`
/ `usage.output_tokens`; Google's `total_*` and per-modality counts stay beside
them. PDFs and per-block `resolution` are not reachable through either public
format.

### Ollama

`protocol: ollama` is System One with Ollama's rules: `images` are sent as bare
base64 (data URLs are unwrapped, URLs return 400), `audio`, `videos`, and
`assets` are rejected, and no credentials are sent unless `api_key` is set. Use
`api_base: https://ollama.com/v1` with a key for cloud models, or a `:cloud`
model name on a signed-in local server. Ollama's `{"error": "..."}` failures
are returned in the proxy's usual error object.

### Granite Switch

Granite Switch 4.1 serves chat and its decision adapter from one vLLM server.
Run it with the
[granite-switch-decision](https://github.com/generative-computing/granite-switch-decision)
plugin, or vLLM 0.32+ which has the routes built in, and declare two aliases
against the same `api_base`: a normal `vllm/` chat entry and the
`granite-decisions` entry above. `protocol: systemone` returns the plugin's
per-question `diagnostics` (`label_mass`, `argmax_is_label`) unchanged;
`protocol: decisions` uses its OpenAI-shaped route instead. The decision routes
are text-only, so leave `modalities` at its default. The adapter (for example
Decision or Guardian) is chosen by the server's `DECISION_ADAPTER_NAME`, not per
request; run one server per adapter and give each its own alias.

## Grant key access

App keys require **both** `allow_decisions: true` and a matching requested alias
in `allowed_models` when that list is non-empty. An empty allowlist means all
aliases subject to the decision permission. A requested alias grants access to
its operator-configured pool/fallback deployments, as with chat routing.

Existing keys migrate with decision access disabled, and new keys default to
disabled on both SQLite and PostgreSQL. In the application workspace, edit the key and select **Allow decision
models** under **Models and limits**. The model allowlist, key RPM/RPD, hard/soft
budgets, revocation, and deployment RPM/TPM continue to apply. A permission edit
invalidates the key cache immediately. An API update that omits
`allow_decisions` preserves its current setting.

For example, an authenticated administrator can create a key using:

```http
POST /admin/applications/123/keys
Content-Type: application/json

{"name":"ticket-triage","allow_decisions":true,"allowed_models":["jev-vision","luna-decisions"],"max_rpm":100,"max_budget":5}
```

The admin routes require the browser's OIDC session. The master key retains its
existing enforcement bypass. Calling chat, embeddings, or Responses with a
decision alias cannot bypass the decision permission.

## Call either format

```bash
curl http://localhost:8080/v1/decisions \
  -H "Authorization: Bearer $PROXY_APP_KEY" \
  -H 'Content-Type: application/json' \
  -H 'X-Session-ID: ticket-4411' \
  -d '{
    "model":"jev-vision",
    "input":"I was charged twice. Please refund the extra charge.",
    "questions":[
      {"name":"refund","type":"predicate","instructions":"Is a refund requested?"},
      {"name":"team","type":"choice","instructions":"Who should handle it?",
       "choices":[{"value":"billing"},{"value":"support"}]}
    ]
  }'
```

Decisions returns an ordered `answers` array, preserving question names.
System One returns answers keyed by the names in the `questions` object:

```json
{
  "model": "laya-local",
  "state": {"ticket": "I was charged twice."},
  "questions": {
    "refund": {"type": "noul", "instructions": "Is a refund requested?"},
    "team": {"type": "choice", "criteria": {"billing": "Money", "support": "Other issues"}},
    "severity": {"type": "score", "criteria": ["low", "medium", "high"]}
  }
}
```

Common text requests translate between protocols. Predicate probabilities,
choices, score indices, probability distributions, confidence, refusals, and
extensions in answers retain their types. String and boolean choice values are
distinct; boolean choices require a native Decisions deployment.

Native calls retain request extensions, structured System One state,
instructions/criteria, and provider-specific fields. Cross-protocol translation
rejects unsupported extensions rather than dropping them. Structured System One
state/instructions, noul criteria, and rich score descriptions need the matching
native protocol. The proxy accepts up to 1,000 questions, and 255 score levels
on `/v1/systemone` (10 on `/v1/decisions`); each upstream applies its own lower
limits (OpenAI 400 questions, Ollama cloud 64). No text-generating chat emulation is used.

## Multimodal calls

For native OpenAI Decisions, use message `content` with `input_text` and
`input_image` parts. Image URLs and embedded data URLs are passed to OpenAI.
For a vision-capable Jev deployment, embedded PNG/JPEG `input_image` parts on
`/v1/decisions` become referenced TypeSafe assets; message roles and content order
are retained as structured System One state. Image `detail` and cache-breakpoint
extensions require native Decisions transport.

Native TypeSafe assets and box questions work on `/v1/systemone`:

```json
{
  "model": "jev-vision",
  "state": {"photo": "photo-1"},
  "assets": {"photo-1": {"type": "image", "format": "png", "data_base64": "<BASE64_PNG>"}},
  "questions": {"target": {"type": "box", "instructions": "Locate the red square in photo."}}
}
```

An asset without a `type` counts as an image, as TypeSafe allows, and is
forwarded unchanged. Configure TypeSafe's image opt-in header on the deployment. Clients cannot
override deployment credentials or opt-in headers. See
[TypeSafe's asset contract](https://docs.typesafe.ai/api/system-one).

Cloudflare accepts native System One `images`, `audio`, and `videos` data-URL
arrays according to the configured model's modalities. `/v1/decisions` embedded
images translate into Cloudflare's image list and numbered image references in
structured state. Cloudflare's protocol places images separately from the state;
audio/video currently require native System One calls.

The proxy never fetches user media URLs. Image conversion requires embedded data;
native providers may support remote references themselves. Incompatible media
and question types return 400 before an upstream call. Fallback selection filters
incompatible deployments, including sticky candidates. Requests/responses are
bounded to 32 MiB; a provider can impose smaller limits.

## Costs and observability

`decision.pricing` is mandatory; an empty block explicitly declares zero cost.
Supported USD fields are `input_cost_per_token`, `output_cost_per_token`,
`cache_read_input_token_cost`, `cache_creation_input_token_cost`, and
`cost_per_request`. Omitted cache prices inherit the input price; set them to
zero explicitly when cached input is free. Decision accounting uses these prices,
not generic chat cost-map overrides. Edit YAML and reload to change them.

Token usage is normalized into fresh input, cache read, cache write, and output
without double charging included cache tokens. Provider usage stays intact in the
response. Known cost is returned as `X-Request-Cost` in USD. Missing usage remains
unknown and token charges cannot be estimated as if measured; a configured fixed
request charge is still credited. Key/pool spend is updated before the response.
Budgets use the existing shared enforcement and may overshoot under concurrent
in-flight calls; this feature does not reserve dollars before inference.

Routed successes and failures appear in normal logs with canonical
`/v1/decisions` or `/v1/systemone` endpoints, key/app/team attribution, requested
alias, selected deployment, pool, latency, usage, and cost. Invalid or denied
requests remain in the HTTP application log. Session IDs use the existing
header/body/metadata precedence and are removed from upstream requests. Snippet
capture obeys `body_snippet_limit`, including 0 to disable it.

Use **Call type** in Request Logs, or:

- `GET /admin/logs?request_type=decision`: only decision calls.
- `GET /admin/logs?request_type=other`: hide decision calls.
- Omit `request_type`: include all calls.

Filters combine with model, provider, session, key, team, application, dates, and
pool, including pagination and counts. The spend dashboard includes decision
spend in the normal aggregates. Model discovery and the Models view show
capabilities and per-deployment prices. OpenTelemetry records a `decision`
operation using the existing request/provider attribution and cost attributes.

Streaming, background jobs, multipart uploads, batch inference, and a decision
playground are not part of this implementation. Automated tests use local
provider servers; paid live-provider conformance is a separate deployment check.
