"""Check the live verifier's stream assembly and incomplete-response detection."""
import importlib.util
import io
import json
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("minimax_smoke", Path(__file__).with_name("minimax-smoke.py"))
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)


def events(*chunks, done=True):
    data = "".join("data: " + json.dumps(c) + "\n\n" for c in chunks)
    return io.BytesIO((data + ("data: [DONE]\n\n" if done else "")).encode())


class StreamAssemblyTest(unittest.TestCase):
    def test_reasoning_and_typed_tool_arguments_survive_fragmentation(self):
        result = events(
            {"choices": [{"delta": {"role": "assistant", "content": None, "reasoning_content": None}}]},
            {"choices": [{"delta": {"reasoning_content": "Use ", "reasoning_details": [{"index": 0, "type": "reasoning.text", "text": "Use "}]}}]},
            {"choices": [{"delta": {"reasoning_content": "tools.", "reasoning_details": [{"index": 0, "type": "reasoning.text", "text": "tools."}]}}]},
            {"choices": [{"delta": {"tool_calls": [{"index": 0, "id": "a", "type": "function", "function": {"name": "sum", "arguments": '{"numbers":[2,'}}]}}]},
            {"choices": [{"delta": {"tool_calls": [{"index": 0, "function": {"arguments": '3],"enabled":true}'}}]}, "finish_reason": "tool_calls"}]},
            {"choices": [], "usage": {"total_tokens": 40}},
        )
        message, usage, finish = smoke.stream_result(result)
        self.assertEqual(message["reasoning_content"], "Use tools.")
        self.assertEqual(message["reasoning_details"], [{"index": 0, "type": "reasoning.text", "text": "Use tools."}])
        call = message["tool_calls"][0]
        self.assertEqual(call["id"], "a")
        self.assertEqual(json.loads(call["function"]["arguments"]), {"numbers": [2, 3], "enabled": True})
        self.assertEqual((usage, finish), ({"total_tokens": 40}, "tool_calls"))

    def test_missing_downstream_done_is_a_failure_even_after_finish(self):
        with self.assertRaisesRegex(ValueError, "missing_done_sentinel"):
            smoke.stream_result(events({"choices": [{"delta": {"content": "partial"}, "finish_reason": "stop"}]}, done=False))

    def test_error_event_is_a_failure_even_after_output(self):
        with self.assertRaisesRegex(ValueError, "stream_error"):
            smoke.stream_result(events({"choices": [{"delta": {"content": "partial"}}]}, {"error": {"message": "upstream failed"}}))


if __name__ == "__main__":
    unittest.main()
