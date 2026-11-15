# ADR 019: Decision provider dialects (Gemini, Ollama, Granite Switch)

Status: Accepted

Amends [ADR 016](016-decision-models.md). Everything not mentioned here is unchanged.

## Context

ADR 016 gave decision models two public formats (`/v1/decisions`, `/v1/systemone`)
and three upstream protocols (`decisions`, `systemone`, `cloudflare`). Research
checked on 2026-11-15 found upstreams those three cannot reach:

- [Gemini Decisions](https://aistudio.google.com/docs/decisions-api)
  (`POST /v1beta/decisions`, `gemini-decision-1`) has System One-shaped questions
  and answers, but its own typed input blocks (`text`, `image`, `audio`, `video`,
  `document`), `x-goog-api-key` authentication, `true_description` /
  `false_description` noul criteria, no `model` in the response, and
  `total_input_tokens` / `total_output_tokens` usage. It accepts 1,000 questions
  and 255 score levels.
- [Ollama](https://docs.ollama.com/api/systemone) serves `POST /v1/systemone`
  for Nimble, Tev, and Clef, locally without credentials and on ollama.com with
  a Bearer key. `images` must be bare base64; data URLs and URLs are refused.
  Errors are `{"error": "<string>"}` rather than an error object.
- [Granite Switch 4.1](https://huggingface.co/blog/ibm-granite/granite-switch-4-1)
  is served by vLLM. The
  [granite-switch-decision](https://github.com/generative-computing/granite-switch-decision)
  plugin (and vLLM itself from 0.32) exposes `/v1/systemone` and `/v1/decisions`
  beside `/v1/chat/completions` on one server and one prefix cache. Decision
  routes are text-only, the adapter is chosen by the server, `/v1/systemone`
  answers carry `diagnostics`, and both routes accept a `cache_salt` field.
- [LiteLLM](https://docs.litellm.ai/docs/decisions) and
  [Bifrost](https://docs.getbifrost.ai/quickstart/gateway/decisions) both send
  each provider its native body from either public route. Bifrost can also
  emulate decisions through a chat model; ADR 016 already rejected that.

## Decisions

1. Add `decision.protocol: gemini`. The transport posts to `<api_base>/decisions`
   with `x-goog-api-key`; the `gemini` provider prefix defaults `api_base` to
   `https://generativelanguage.googleapis.com/v1beta`. Gemini is treated as a
   System One dialect: the request is rewritten on the way out and the response
   is normalised to System One (adding `model` and `input_tokens` /
   `output_tokens`, keeping Google's usage fields) before the existing answer
   validation and Decisions translation run. No second answer translator exists.
2. Gemini input mapping keeps order and never fetches media. System One `state`
   becomes text (structured state as compact JSON, which is what System One
   servers do themselves); `images`, `audio`, and `videos` become typed blocks
   after it. Decisions input made only of user messages becomes ordered text and
   image blocks, with `input_json` as compact JSON text. Mixed-role transcripts
   are sent as a JSON transcript when they are text-only and rejected when they
   carry media, because Gemini blocks have no role. Embedded media needs a
   concrete MIME type: data URLs carry one, bare base64 is sniffed. HTTPS URLs
   are forwarded as `uri` when the type is known from the file extension.
   TypeSafe `assets`, tool calls, unknown request extensions, and question keys
   outside Gemini's `[A-Za-z0-9_-]{1,64}` return 400 before routing.
3. Add `decision.protocol: ollama`. It is System One on the wire with three
   differences: the `ollama` prefix defaults `api_base` to
   `http://localhost:11434/v1` and sends no credentials unless `api_key` is set;
   `images` data URLs are unwrapped to bare base64 and URLs are rejected; and
   string error bodies are wrapped as `{"error": {"message": ...}}` so clients
   always see the proxy's error shape. `assets`, `audio`, and `videos` are
   rejected. A separate protocol, rather than a flag on `systemone`, keeps the
   rule from ADR 016 that capability never comes from guessing a provider name.
4. Granite Switch needs no protocol of its own. It is declared with `systemone`
   or `decisions` against the vLLM server, next to a separate chat alias for the
   same `api_base`. Pass-through already preserves `diagnostics`.
5. Add `decision.extra_body`: operator-owned JSON fields merged into every
   upstream request for that deployment, after translation. This is how a
   deployment sets Granite's `cache_salt` or Ollama's `keep_alive` for callers
   on either route. Deployment values win over a client field of the same name,
   matching the existing rule for deployment headers. Fields the proxy owns
   (`model`, `input`, `state`, `questions`, media, `stream`, `background`) are
   rejected at load. The block is not exposed through model discovery or admin
   config, since a cache salt is an isolation boundary.
6. Proxy-side limits become the widest any supported upstream accepts: 1,000
   questions, and 255 score levels on System One-shaped requests. Decisions-
   format requests keep OpenAI's 10 levels. Upstreams still enforce their own.
7. Accept `input_json` parts in Decisions input. OpenAI documents them and the
   ADR 016 parser refused them.

## Consequences

Either public route now reaches OpenAI, Gemini, Ollama, TypeSafe, Cloudflare,
and Granite Switch deployments, and they can share a pool: the existing
eligibility filter skips a deployment whose dialect cannot carry a request.

Translation stays strict. A request that works natively against one dialect can
return 400 through another (level descriptions, noul criteria on `/v1/decisions`,
remote media for Ollama, PDFs for Gemini from either public format). Gemini's
`document` blocks and per-block `resolution` have no public-format equivalent
and are not reachable in this change.

`extra_body` is a small escape hatch. It can break a deployment if misused, but
only its operator can set it. Tests use local fixture servers that follow the
documented wire formats; live-provider conformance remains a deployment check.
