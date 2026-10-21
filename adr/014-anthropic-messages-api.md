# ADR 014: Provider-independent Anthropic Messages API

Date: 2026-10-21
Status: Accepted
Design: https://github.com/pridkett/simple-llm-proxy/issues/113
Implementation: https://github.com/pridkett/simple-llm-proxy/issues/114

## Context

The proxy exposes OpenAI chat completions and has an outbound Anthropic adapter,
but Anthropic SDK clients cannot use it directly. Clients should be able to send
the Messages protocol while selecting any configured `model_name`, irrespective
of the provider behind that alias. Provider features still depend on the target
model (for example, vision and tool calling).

## Decision

Add `POST /v1/messages` as a second wire format for the shared chat execution
pipeline. Translate requests into `model.ChatCompletionRequest`; let the existing
router select deployments, substitute actual model names, and perform retries
and failover. Translate normalized responses and stream chunks back into the
Messages protocol. Do not implement a second router or an Anthropic-only bypass.

### Request and authentication contract

- Accept proxy keys in `x-api-key`, or the existing Bearer header. An explicitly
  supplied Authorization header takes precedence. Other endpoints keep their
  existing authentication contract. Reuse key activity, rate/budget enforcement,
  model allowlists, session attribution, and provider credential isolation.
- Translate string/text-block system prompts, text and image content, assistant
  tool-use blocks, user tool-result blocks, tool schemas, tool choice, maximum
  output tokens, sampling controls, stop sequences, and metadata user IDs.
- Convert base64 images to data URLs and URL images to normalized image parts.
  Preserve tool-result ordering before accompanying user text. Translate
  `disable_parallel_tool_use` to the shared parallel tool-call setting.
- Require a model, positive max_tokens, and nonempty valid messages. Reject
  unsupported semantic options and content types instead of silently dropping
  them. Accept cache-control hints without promising caching across providers.
- The model in a response is the requested proxy alias; routing headers identify
  the deployment actually used.

### Response and streaming contract

- Non-streaming responses have `type: message`, assistant role, typed content
  blocks, stop reason, nullable stop sequence, and Anthropic-shaped usage.
- Translate text and function calls, including JSON tool input. Invalid upstream
  tool input fails as a provider error instead of fabricating valid arguments.
- Streaming uses named `message_start`, `content_block_start`,
  `content_block_delta`, `content_block_stop`, `message_delta`, and `message_stop`
  events. Flush incrementally. Convert streamed tool arguments to
  `input_json_delta`, tracking upstream tool indices independently of content
  indices. Interleaved tool deltas remain incremental; starts have stable IDs
  and names, and every started block is closed once before message completion.
- Wait for successful upstream EOF before emitting message_stop. A midstream
  failure emits an Anthropic error event, never a successful completion marker.
  Client cancellation continues to avoid penalizing provider health.
- Map length to max_tokens, tool_calls to tool_use, stop to end_turn, and preserve
  stop_sequence when the upstream reports it. Do not invent a matched sequence
  when the normalized upstream result does not identify it.
- Use reported token usage, including cache counts. OpenAI-compatible streams
  request usage explicitly; final usage may arrive after the finish reason.
  message_start may have zero input usage until the provider reports counts;
  message_delta carries final counts. Unknown usage remains unknown in storage.
- Return Anthropic-shaped HTTP and SSE errors, preserve HTTP status/Retry-After,
  and expose request-id for SDK correlation.

### Shared execution

Parameterize the existing chat handler's input decoding and output encoding.
Keep routing, telemetry, spending, token reservations, and request logging in
one execution path. Attribute requests to `/v1/messages` in logs. Keep the
OpenAI wire format and its usage opt-in behavior unchanged.

## Implementation plan

1. Add protocol request/response types and conversion tests in a provider-neutral
   Anthropic package; cover system, image, tool round trips, and invalid input.
2. Add incremental SSE encoding with tests for fragmented/parallel tools, late
   usage, finish reasons, malformed streams, and error termination.
3. Extend shared chat execution with Messages decoding/encoding; wire the route,
   SDK authentication, error responses, CORS, and request IDs.
4. Add real HTTP integration tests against mock OpenAI-compatible and Anthropic
   upstreams, including restrictions, errors, streaming, and log attribution.
5. Document the endpoint, limitations, OpenAPI contract, curl and SDK examples.
   Run the backend suite, race checks on affected packages, vet, and build.

## Alternatives

An Anthropic passthrough would preserve vendor extensions but cannot route to
other providers. A response-writer wrapper around the OpenAI handler would avoid
some refactoring but would require parsing already serialized SSE and obscure
error handling. A separate full handler would duplicate policy and accounting.
The shared handler with independent protocol conversion keeps policy in one place.

## Consequences and limits

This is Messages compatibility for portable chat capabilities, not the complete
Anthropic platform. Token counting, batches, files, server tools, documents,
extended thinking, and beta-only options are outside this change. Cache hints
are accepted but are not forwarded as caching guarantees. Target models must
support requested vision/tools; translation cannot add those abilities. Exact
matched stop sequences cannot be reconstructed if a provider omits them.

## References

- https://platform.claude.com/docs/en/api/messages
- https://platform.claude.com/docs/en/build-with-claude/streaming
- https://platform.claude.com/docs/en/api/errors
