# ADR 021: GenAI semantic conventions first, Langfuse extras opt-in

**Status:** Accepted
**Tracking:** [Issue #179](https://github.com/pridkett/simple-llm-proxy/issues/179)

## Context

ADR 011 chose the OpenTelemetry GenAI semantic conventions for span attributes, and ADR 018 added message capture "with Langfuse observation mappings". In practice the proxy wrote each captured chat exchange three times: `gen_ai.*` and `langfuse.observation.*` on the model span, plus `langfuse.observation.*` on the HTTP span. Session, user, and key metadata were duplicated into `langfuse.*` keys too. The copies tripled span size and overflowed downstream per-span limits (simple-agent-tracer dropped content past 64 KiB).

Langfuse's OTLP ingestion (`OtelIngestionProcessor.ts`) reads `gen_ai.input.messages` and `gen_ai.output.messages`. It adds `gen_ai.system_instructions` to the input as a system message, reads `gen_ai.conversation.id` as the session, and reads `user.id`. Its documentation table lags the code and lists only the older `gen_ai.prompt` and `gen_ai.completion`.

## Decision

The [GenAI semantic conventions](https://github.com/open-telemetry/semantic-conventions-genai) are the telemetry contract. Vendor-specific keys are added only when a backend needs something the conventions can't express, and only when asked for.

- Chat capture emits only `gen_ai.input.messages`, `gen_ai.system_instructions`, and `gen_ai.output.messages` on the model span.
- Sessions use `gen_ai.conversation.id` and users use `user.id`. `langfuse.session.id` and `langfuse.user.id` are removed.
- Key attribution uses `llmproxy.{team,application,api_key_name}` plus new `llmproxy.{team,application,api_key}_id`.
- `otel_settings.langfuse_compat` (default `false`, read at startup) adds the two Langfuse-only features:
  - filterable `langfuse.trace.metadata.*`;
  - `langfuse.observation.input`/`output` on the HTTP span, where Langfuse v4 reads trace-level input and output.
- Decisions and System One keep `langfuse.observation.input`/`output` on the model span. They are structured questions and answers, not chat messages, there is no GenAI attribute for them, and it is the only copy.

## Consequences

- Without compatibility mode, a model span carries about 2 × `max_bytes` of content instead of about 4 ×, and the HTTP span carries none.
- Langfuse users who relied on filterable trace metadata or trace-level input/output must set `langfuse_compat: true`. Generations still show their input and output without it.
- Consumers that read `langfuse.trace.metadata.application_id` (simple-agent-tracer) must also read `llmproxy.application_id`.

## References

- [ADR 011](011-otel-monitoring.md), [ADR 018](018-coding-agent-telemetry.md)
- [OpenTelemetry GenAI semantic conventions](https://github.com/open-telemetry/semantic-conventions-genai)
- [Langfuse OtelIngestionProcessor.ts](https://github.com/langfuse/langfuse/blob/main/packages/shared/src/server/otel/OtelIngestionProcessor.ts)
