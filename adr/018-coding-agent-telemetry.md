# ADR 018: Capture coding-agent runs with OpenTelemetry

**Status:** Proposed
**Tracking:** [Design issue #135](https://github.com/pridkett/simple-llm-proxy/issues/135) and [implementation issue #136](https://github.com/pridkett/simple-llm-proxy/issues/136)

## Context

ADR 011 established opt-in OTLP trace export. The proxy emits an HTTP span and a child model span, with routing retries represented as events. Request logs support explicit session IDs and short body snippets. Coding-agent experiments need ordered messages and tool exchanges across multiple requests and a way to isolate each run.

## Decision

### Session correlation

Reuse the existing validated session attribution. Add the same `gen_ai.conversation.id` and `langfuse.session.id` to the HTTP span and the model span. Scope the exported identifier to the authenticated application so key rotation preserves grouping and independent applications stay separate. Use a digest of the client ID to accommodate the full UTF-8 input range within Langfuse's session ID limits. The proxy request ID remains available for joining exported spans to logs.

### Structured capture

Opt in by listing authenticated application IDs in `otel_settings.message_capture`. Capture runs only on recording LLM spans and covers Chat Completions, Anthropic Messages, and Responses creation. Normalize ordered messages, instructions, and tool calls and results from the client-facing protocol. Reassemble SSE deltas and mark partial and truncated output. Keep provider retries as metadata on the same model span.

Use GenAI input, system instruction, and output attributes with Langfuse observation mappings for direct OTLP export. Apply byte limits and redact recognized credential patterns and secret fields before export. Omit binary data and reasoning blocks. Free text can still contain source code or unrecognized secrets, so operators should restrict capture to dedicated test applications and use a destination appropriate for the test data.

The pilot exports a bounded copy of the observed exchange. Session grouping stays independent of capture. Offline analysis can join tool calls with their results, retain replayed history as context, and count new message parts separately when clustering.

### Distributed run traces

Accept W3C Trace Context only from explicitly configured immediate peer CIDRs. Extract it before creating the HTTP server span. With a trusted, valid header, that span joins the harness trace under the supplied parent. An empty allowlist leaves the HTTP request as the root. Deployments behind a trusted relay must have the relay set or sanitize incoming trace headers. The harness exports its `agent.run` span to the same trace destination.

## Verification

Exercise two runs with different session IDs, repeated requests within one run, key rotation, and application isolation. Exercise streamed tools, ordinary and oversized inputs, partial output, credential redaction, and disabled capture. Verify that trusted calls join the harness trace and that untrusted or malformed headers start independent traces.

## References

- [ADR 011](011-otel-monitoring.md)
- [OpenTelemetry GenAI client spans](https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/client-inference.md)
- [Langfuse OpenTelemetry mappings](https://langfuse.com/integrations/native/opentelemetry)
- [W3C Trace Context](https://www.w3.org/TR/trace-context/)
