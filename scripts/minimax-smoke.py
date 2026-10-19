#!/usr/bin/env python3
"""Verify hosted MiniMax chat and native tool round trips using synthetic data.

Inject PROXY_API_KEY with op run. Reports metadata only; credentials, prompts,
reasoning text, and model responses are never printed. Makes real API calls.
"""
import argparse
import json
import os
import sys
import urllib.error
import urllib.request


def stream_result(response):
    message = {"role": "assistant", "content": ""}
    calls, reasoning_details = {}, {}
    usage, finish, done = None, None, False
    for raw in response:
        line = raw.decode().strip()
        if not line.startswith("data:"):
            continue
        data = line[5:].strip()
        if data == "[DONE]":
            done = True
            break
        chunk = json.loads(data)
        if "error" in chunk:
            raise ValueError("stream_error")
        if chunk.get("usage") is not None:
            usage = chunk["usage"]
        for choice in chunk.get("choices", []):
            finish = choice.get("finish_reason") or finish
            delta = choice.get("delta", {})
            for key, value in delta.items():
                if key in ("content", "reasoning_content"):
                    if value is not None:
                        message[key] = (message.get(key) or "") + value
                elif key == "tool_calls":
                    for fragment in value:
                        call = calls.setdefault(fragment["index"], {"function": {"name": "", "arguments": ""}})
                        for field in ("id", "type"):
                            if fragment.get(field):
                                call[field] = fragment[field]
                        for field in ("name", "arguments"):
                            call["function"][field] += fragment.get("function", {}).get(field, "")
                elif key == "reasoning_details" and value:
                    for detail in value:
                        item = reasoning_details.setdefault(detail.get("index", 0), {})
                        for field, part in detail.items():
                            item[field] = item.get(field, "") + part if field in ("text", "signature") else part
                else:
                    message[key] = value
    if not done:
        raise ValueError("missing_done_sentinel")
    if calls:
        message["tool_calls"] = [calls[i] for i in sorted(calls)]
    if reasoning_details:
        message["reasoning_details"] = [reasoning_details[i] for i in sorted(reasoning_details)]
    return message, usage, finish


def request(base, key, payload):
    req = urllib.request.Request(
        base.rstrip("/") + "/chat/completions",
        data=json.dumps(payload).encode(),
        headers={"Authorization": "Bearer " + key, "Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=180) as response:
        if payload.get("stream"):
            return stream_result(response)
        body = json.load(response)
        choice = body["choices"][0]
        return choice["message"], body.get("usage"), choice.get("finish_reason")


def verify(base, key, model, stream, tools):
    payload = {
        "model": model,
        "messages": [{"role": "user", "content": "Calculate 17*23 + 19*29 carefully. Reply exactly MINIMAX_OK_942 once you have checked the calculation."}],
        "max_tokens": 4096,
        "reasoning_split": True,
        "stream": stream,
    }
    if model.endswith("MiniMax-M3.1-Flash"):
        payload["reasoning_effort"] = "low"  # Keep this synthetic check small; thinking stays enabled.
    if stream:
        payload["stream_options"] = {"include_usage": True}
    if tools:
        payload["messages"] = [
            {"role": "system", "content": "Use sum_numbers to calculate the requested sum. After its result reply exactly TOTAL=10."},
            {"role": "user", "content": "Call sum_numbers once with numbers [2,3,5] and label proxy-check. Do not calculate the sum yourself."},
        ]
        payload["tools"] = [{"type": "function", "function": {
            "name": "sum_numbers",
            "description": "Calculate a sum using an array of integers and a tracking label.",
            "parameters": {"type": "object", "properties": {
                "numbers": {"type": "array", "items": {"type": "integer"}},
                "label": {"type": "string"},
            }, "required": ["numbers", "label"], "additionalProperties": False},
        }}]
    message, usage, finish = request(base, key, payload)
    if not usage or usage.get("total_tokens", 0) <= 0:
        raise ValueError("missing_usage")
    reasoning = bool(message.get("reasoning_content") or message.get("reasoning_details") or "<think>" in (message.get("content") or ""))
    if not reasoning and not tools:
        raise ValueError("missing_reasoning")
    if tools:
        calls = message.get("tool_calls", [])
        if finish != "tool_calls" or len(calls) != 1:
            raise ValueError("missing_native_tool_call")
        call = calls[0]
        arguments = json.loads(call["function"]["arguments"])
        if call["function"]["name"] != "sum_numbers" or arguments != {"numbers": [2, 3, 5], "label": "proxy-check"}:
            raise ValueError("tool_arguments_changed")
        # Preserve the complete assistant message, including reasoning extensions.
        payload["messages"].extend([message, {
            "role": "tool", "tool_call_id": call["id"], "content": str(sum(arguments["numbers"])),
        }])
        payload["tool_choice"] = "none"
        final, final_usage, final_finish = request(base, key, payload)
        if "TOTAL=10" not in (final.get("content") or "") or final_finish != "stop" or not final_usage:
            raise ValueError("tool_round_trip_failed")
    elif "MINIMAX_OK" not in (message.get("content") or "") or finish != "stop":
        raise ValueError("chat_incomplete")
    return {"reasoning_present": reasoning, "usage_present": True}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default="http://localhost:5173/v1")
    parser.add_argument("--model", action="append")
    args = parser.parse_args()
    key = os.environ.get("PROXY_API_KEY")
    if not key:
        parser.error("inject PROXY_API_KEY with op run")
    failures = 0
    for model in args.model or ["minimax/MiniMax-M3.1-Flash", "minimax/MiniMax-M3"]:
        for tools in (False, True):
            for stream in (False, True):
                result = {"model": model, "stream": stream, "tools": tools}
                try:
                    result.update(verify(args.base_url, key, model, stream, tools), passed=True)
                except urllib.error.HTTPError as exc:
                    result.update(status=exc.code, passed=False)
                except ValueError as exc:
                    # Only report our fixed diagnostic codes, never provider text.
                    code = str(exc)
                    result.update(error=code if code in {
                        "stream_error", "missing_done_sentinel", "missing_usage", "missing_reasoning",
                        "missing_native_tool_call", "tool_arguments_changed", "tool_round_trip_failed", "chat_incomplete",
                    } else type(exc).__name__, passed=False)
                except Exception as exc:
                    result.update(error=type(exc).__name__, passed=False)
                failures += not result["passed"]
                print(json.dumps(result), flush=True)
    return int(failures > 0)


if __name__ == "__main__":
    sys.exit(main())
