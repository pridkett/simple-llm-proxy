# Direct MiniMax Coding Plan

Use the existing `minimax` provider with explicit model IDs and inject
`MINIMAX_API_KEY` into the proxy process. The commented examples in
`config.yaml.example` configure `minimax/MiniMax-M3.1-Flash` and `minimax/MiniMax-M3`.
These aliases use MiniMax directly; existing `openrouter/minimax/...` aliases
continue to use OpenRouter and its credentials.

The hosted OpenAI-compatible base URL is `https://api.minimax.io/v1`. An
Anthropic endpoint ending in `/messages` is a different protocol and cannot be
copied into this provider's base URL. See [MiniMax's API documentation](https://platform.minimax.io/docs/api-reference/text-openai-api).

Set `extra_params.xml_tool_calls: false` for the hosted API's native JSON tool
calls. The older XML conversion remains available for endpoints that require
it. A Coding Plan key's model discovery response does not establish access to
every listed model; verify each configured model with an actual completion.

## Compatibility and validation

MiniMax may finish its SSE stream without `[DONE]`. Its provider accepts a clean
EOF only after receiving a terminal `finish_reason`, then the proxy supplies the
usual downstream `[DONE]`. Missing finish reasons, malformed/error events, and
transport errors remain failures. Other providers still require their usual
completion marker.

Preserve the complete assistant message when returning tool results, including
`reasoning_content`, `reasoning_details`, or `<think>` content. Request
`reasoning_split: true` when separate reasoning fields are desired. Stripping
these fields can break the model's multi-turn reasoning chain.

Run the optional live check against an already running proxy, injecting
`PROXY_API_KEY` through an existing op environment file:

```sh
op run --env-file=client.op.env -- python3 scripts/minimax-smoke.py \
  --base-url http://localhost:8080/v1
```

The key must allow both configured MiniMax aliases; use repeatable `--model`
arguments to select different ones. The script makes real requests using
synthetic text and checks sync/streamed chat, reasoning, usage, typed tool
arguments, and complete tool-result conversations. It prints metadata only.
It requires a downstream `[DONE]` and refuses incomplete streams. The synthetic
M3.1-Flash check uses `reasoning_effort: low` to limit token consumption while
keeping thinking enabled. Older model aliases can be selected explicitly with
`--model`.

M3.1-Flash can return a native tool call without a separate reasoning field.
The basic calculation check explicitly exercises reasoning; tool round trips
preserve the full assistant message and report whether reasoning was emitted.

Offline regression checks:

```sh
go test -race ./internal/provider/minimax ./internal/provider/openaicompat
python3 -m unittest discover -s scripts -p 'test_minimax_smoke.py'
```

Ansible deployment should inject the key by reference, preserve the existing
database volume, and pin the proxy image to a revision containing the completed
stream fix. Add the new aliases to each intended application's existing key
allowlist. Rollback consists of removing those aliases from the deployment and
allowlist and restoring the previous pinned image; retain the database volume.
