#!/usr/bin/env python3
"""Exercise an already-running proxy without printing credentials or responses."""
import argparse
import json
import os
import sys
import urllib.error
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default="http://localhost:8080/v1")
    parser.add_argument("--model", action="append", required=True)
    parser.add_argument("--max-tokens", type=int, default=128)
    args = parser.parse_args()
    key = os.environ.get("PROXY_API_KEY")
    if not key:
        parser.error("inject PROXY_API_KEY with op run")
    failures = 0
    for model in args.model:
        for stream in (False, True):
            payload = {
                "model": model,
                "messages": [{"role": "user", "content": "Reply with OK."}],
                "max_completion_tokens": args.max_tokens,
                "stream": stream,
            }
            if stream:
                payload["stream_options"] = {"include_usage": True}
            request = urllib.request.Request(
                args.base_url.rstrip("/") + "/chat/completions",
                data=json.dumps(payload).encode(),
                headers={"Authorization": "Bearer " + key, "Content-Type": "application/json"},
            )
            result = {"model": model, "stream": stream}
            try:
                with urllib.request.urlopen(request, timeout=180) as response:
                    data = response.read().decode()
                    result["status"] = response.status
                if stream:
                    records = [line[5:].strip() for line in data.splitlines() if line.startswith("data:")]
                    chunks = [json.loads(record) for record in records if record != "[DONE]"]
                    result["complete"] = "[DONE]" in records
                    result["usage_present"] = any(chunk.get("usage") is not None for chunk in chunks)
                    result["has_output"] = any(chunk.get("choices") for chunk in chunks)
                else:
                    body = json.loads(data)
                    result.update(complete=True, has_output=bool(body.get("choices")), usage_present=body.get("usage") is not None)
                result["passed"] = result["complete"] and result["has_output"]
            except urllib.error.HTTPError as exc:
                result.update(status=exc.code, passed=False)
            except Exception as exc:
                result.update(error=type(exc).__name__, passed=False)
            failures += not result["passed"]
            print(json.dumps(result), flush=True)
    return int(failures > 0)


if __name__ == "__main__":
    sys.exit(main())
