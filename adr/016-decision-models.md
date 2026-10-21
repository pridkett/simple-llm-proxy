# ADR 016: Decision models as explicit deployments

Status: Accepted

## Context

Decision models return probabilities, typed choices, scores, and sometimes image
boxes without generating chat text. They are a separate operation even when the
same underlying model also serves chat. Provider names alone cannot identify a
model's protocol, modalities, or price.

Research checked on 2026-10-21:

- [OpenAI Decisions](https://developers.openai.com/api/reference/resources/decisions/methods/create)
  uses `input`, an ordered `questions` array, and ordered answers.
- [TypeSafe System One](https://docs.typesafe.ai/api/system-one) uses JSON `state`,
  named questions/answers, `noul` predicates, choice maps, and ordered score criteria.
  Images/assets and box questions require a deployment-supplied opt-in header.
- [Granite Switch 4.1](https://huggingface.co/blog/ibm-granite/granite-switch-4-1)
  exposes decision adapters through System One.
- [Laya](https://github.com/LeonaDavinci/laya-system-one) and its self-hosted servers
  also expose System One; hosted services have different origins and credentials.
- [Cloudflare Clef](https://developers.cloudflare.com/workers-ai/models/clef/)
  uses System One inside Workers AI's response envelope. Clef-omni also supports
  [audio and video](https://developers.cloudflare.com/workers-ai/models/clef-omni/).
- [LiteLLM's implementation](https://docs.litellm.ai/docs/decisions) provides both
  public formats over a shared deployment list, with normal key enforcement.

## Decisions

1. Declare a `decision` block on a normal `model_list` entry. It selects an
   upstream protocol (`decisions`, `systemone`, or `cloudflare`), optional path,
   supported modalities and question types, and mandatory endpoint-specific USD
   prices. Defaults are text and predicate/choice/score. Explicit `{}` pricing
   means free/self-hosted inference. No provider/model-name guessing enables a
   new capability. Separate aliases serve chat and decisions for the same model.
2. Expose authenticated `POST /v1/decisions` and `POST /v1/systemone` (plus the
   unversioned aliases). Preserve native requests, responses, and extensions.
   Translate the common question types across formats, with stable question IDs
   and order. Bridge embedded Decisions images into TypeSafe assets or Cloudflare
   image lists; retain roles and content order as structured state. Reject other
   translations that would lose information. Never emulate a
   decision by asking a chat model to generate JSON. No streaming or background
   requests in this implementation.
3. Use the existing router for retries, cooldown, backoff, sticky routing, pool
   budgets, and deployment RPM/TPM. Filter candidates by operation/capability
   before selection so a fallback cannot silently drop an image or call chat.
4. Require `allow_decisions=true` on app keys AND the existing `allowed_models`
   check on the requested alias. An empty allowlist still means all aliases,
   subject to the coarse permission. Existing and new keys default to false;
   omitted permission on key update preserves its current value. Master keys
   retain their existing bypass. Decision deployments cannot be used through
   chat, embeddings, or Responses to bypass permission checks.
5. Record successful and failed routed calls in normal usage logs, with canonical
   endpoint, session, key, deployment, pool, tokens, cost, status, and latency.
   `request_type=decision|other` filters logs, counts, and pagination together.
   The UI can include, show only, or hide decision calls. Classify from the
   persisted endpoint so historical logs stay stable after config reloads.
6. Use declared decision prices instead of a model's generic chat price. Support
   input/output, cache read/write, and fixed request charges. Normalize included
   cache counts into non-overlapping token categories for accounting. Missing
   usage remains unknown, rather than a measured zero. Credit key and pool spend
   before returning the result; unknown token usage cannot be invented.
7. Media stays structured. Forward remote references to a provider only when its
   protocol accepts them; the proxy never fetches user-supplied media URLs.
   Native System One `assets`, `images`, `audio`, and `videos` cover provider
   extensions without flattening media into text. Cross-format conversions are
   deliberately strict and can reject a request that the native endpoint accepts.
8. Both SQLite and PostgreSQL share permission CRUD and log filtering in
   `sqlstore`. Append migrations to both databases so existing keys stay disabled;
   do not change PostgreSQL's released baseline schema.

## Consequences

Adding a compatible decision server generally needs configuration, not a new
provider implementation. Capability and price declarations are operator-owned;
the server still enforces its model-specific limits. Tests use local HTTP servers
and fixtures; paid live-provider calls are a separate operational check.

Providers with unrelated wire formats need additional adapters. Batch endpoints,
automatic threshold policies, decision-specific playgrounds, and decision-driven
chat routing are outside this change.
