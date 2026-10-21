package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
)

func TestRequestTranslation(t *testing.T) {
	var request Request
	err := json.Unmarshal([]byte(`{
 "model":"local-alias","max_tokens":23,"stream":true,"temperature":0,"top_p":0.5,"stop_sequences":["END"],
 "system":[{"type":"text","text":"first","cache_control":{"type":"ephemeral"}},{"type":"text","text":"second"}],
 "metadata":{"user_id":"user-1"},
 "messages":[
 {"role":"user","content":[{"type":"text","text":"look"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"YWJj"}},{"type":"image","source":{"type":"url","url":"https://example.test/image.png"}}]},
 {"role":"assistant","content":[{"type":"text","text":"checking"},{"type":"tool_use","id":"call-a","name":"lookup","input":{"city":"NYC"}},{"type":"tool_use","id":"call-b","name":"lookup","input":{}}]},
 {"role":"user","content":[{"type":"tool_result","tool_use_id":"call-a","content":[{"type":"text","text":"sunny"}]},{"type":"tool_result","tool_use_id":"call-b","content":"unavailable","is_error":true},{"type":"text","text":"continue"}]}
 ],
 "tools":[{"name":"lookup","description":"Weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}}}}],
 "tool_choice":{"type":"tool","name":"lookup","disable_parallel_tool_use":true}
}`), &request)
	if err != nil {
		t.Fatal(err)
	}
	chat, err := request.ChatRequest()
	if err != nil {
		t.Fatal(err)
	}
	if chat.Model != "local-alias" || *chat.MaxTokens != 23 || !chat.Stream || !chat.StreamOptions.IncludeUsage || chat.User != "user-1" || *chat.Temperature != 0 || *chat.TopP != 0.5 || chat.Stop[0] != "END" {
		t.Fatalf("request options lost: %+v", chat)
	}
	if len(chat.Messages) != 6 || chat.Messages[0].Content != "first\n\nsecond" || chat.Messages[3].Role != "tool" || chat.Messages[3].ToolCallID != "call-a" || chat.Messages[3].Content != "sunny" || chat.Messages[4].Content != "Tool error: unavailable" || chat.Messages[5].Content != "continue" {
		t.Fatalf("conversation lost: %+v", chat.Messages)
	}
	parts := chat.Messages[1].Content.([]model.ContentPart)
	if len(parts) != 3 || parts[1].ImageURL.URL != "data:image/png;base64,YWJj" || parts[2].ImageURL.URL != "https://example.test/image.png" {
		t.Fatal(parts)
	}
	if len(chat.Messages[2].ToolCalls) != 2 || chat.Messages[2].ToolCalls[0].Function.Arguments != `{"city":"NYC"}` {
		t.Fatal(chat.Messages[2])
	}
	if chat.ParallelToolCalls == nil || *chat.ParallelToolCalls || chat.Tools[0].Function.Name != "lookup" {
		t.Fatal("tool options lost")
	}
	choice := chat.ToolChoice.(map[string]any)
	if choice["function"].(map[string]any)["name"] != "lookup" {
		t.Fatal(choice)
	}
}

func TestRequestValidation(t *testing.T) {
	valid := `{"model":"any-alias","max_tokens":1,"messages":[{"role":"user","content":"hello"}]}`
	for _, test := range []struct{ name, body string }{
		{"missing model", strings.Replace(valid, `"any-alias"`, `""`, 1)},
		{"missing max", strings.Replace(valid, `"max_tokens":1,`, "", 1)},
		{"negative max", strings.Replace(valid, `"max_tokens":1`, `"max_tokens":-1`, 1)},
		{"missing messages", `{"model":"alias","max_tokens":1}`},
		{"bad role", strings.Replace(valid, `"role":"user"`, `"role":"system"`, 1)},
		{"null content", strings.Replace(valid, `"content":"hello"`, `"content":null`, 1)},
		{"empty blocks", strings.Replace(valid, `"content":"hello"`, `"content":[]`, 1)},
		{"unknown block", strings.Replace(valid, `"content":"hello"`, `"content":[{"type":"thinking","text":"secret"}]`, 1)},
		{"bad image", strings.Replace(valid, `"content":"hello"`, `"content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"bad"}}]`, 1)},
		{"bad URL", strings.Replace(valid, `"content":"hello"`, `"content":[{"type":"image","source":{"type":"url","url":"file:///etc/passwd"}}]`, 1)},
		{"bad tool input", strings.Replace(valid, `"role":"user","content":"hello"`, `"role":"assistant","content":[{"type":"tool_use","id":"a","name":"foo","input":null}]`, 1)},
		{"result requires user", strings.Replace(valid, `"role":"user","content":"hello"`, `"role":"assistant","content":[{"type":"tool_result","tool_use_id":"a"}]`, 1)},
		{"thinking", strings.Replace(valid, `"max_tokens":1`, `"max_tokens":1,"thinking":{"type":"enabled","budget_tokens":100}`, 1)},
		{"top k", strings.Replace(valid, `"max_tokens":1`, `"max_tokens":1,"top_k":3`, 1)},
		{"temperature", strings.Replace(valid, `"max_tokens":1`, `"max_tokens":1,"temperature":2`, 1)},
		{"unknown tool", strings.Replace(valid, `"max_tokens":1`, `"max_tokens":1,"tool_choice":{"type":"tool","name":"missing"}`, 1)},
		{"server tool", strings.Replace(valid, `"max_tokens":1`, `"max_tokens":1,"tools":[{"type":"web_search_20250305","name":"web_search","input_schema":{"type":"object"}}]`, 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			var req Request
			err := json.Unmarshal([]byte(test.body), &req)
			if err == nil {
				_, err = req.ChatRequest()
			}
			if err == nil {
				t.Fatalf("accepted invalid request: %s", test.body)
			}
		})
	}
	for _, choice := range []string{`{"type":"auto"}`, `{"type":"none"}`, `{"type":"any"}`} {
		body := strings.Replace(valid, `"max_tokens":1`, `"max_tokens":1,"tools":[{"name":"tool","input_schema":{"type":"object"}}],"tool_choice":`+choice, 1)
		var req Request
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			t.Fatal(err)
		}
		if _, err := req.ChatRequest(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResponseTranslation(t *testing.T) {
	chat := &model.ChatCompletionResponse{Choices: []model.Choice{{Message: &model.Message{Content: "hello", ToolCalls: []model.ToolCall{{ID: "call", Function: model.FunctionCall{Name: "lookup", Arguments: `{"x":1}`}}}}, FinishReason: "tool_calls"}}, Usage: &model.Usage{PromptTokens: 10, CompletionTokens: 3, CacheReadTokens: 4, CacheWriteTokens: 2}}
	response, err := FromChat(chat, "public-alias")
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != "public-alias" || response.Type != "message" || !strings.HasPrefix(response.ID, "msg_") || *response.StopReason != "tool_use" || response.StopSequence != nil || len(response.Content) != 2 || *response.Content[0].Text != "hello" || string(response.Content[1].Input) != `{"x":1}` || response.Usage.CacheReadInputTokens != 4 || response.Usage.CacheCreationInputTokens != 2 {
		t.Fatalf("bad response: %+v", response)
	}
	for _, bad := range []string{`not json`, `[]`, `null`, `"text"`} {
		chat.Choices[0].Message.ToolCalls[0].Function.Arguments = bad
		if _, err := FromChat(chat, "alias"); err == nil {
			t.Fatalf("accepted bad upstream JSON %q", bad)
		}
	}
	for input, want := range map[string]string{"stop": "end_turn", "length": "max_tokens", "tool_calls": "tool_use", "stop_sequence": "stop_sequence", "content_filter": "refusal"} {
		if got := stopReason(input); got != want {
			t.Errorf("%s: %s", input, got)
		}
	}
	if _, err := FromChat(nil, "alias"); err == nil {
		t.Fatal("nil upstream accepted")
	}
}
