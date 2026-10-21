package api_test

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
)

func messagesRequest(t *testing.T, f *migrationFixture, body, key string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest("POST", f.url+"/v1/messages", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("x-api-key", key)
	req.Header.Set("X-Session-ID", "messages-session")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, response.Header, data
}

func TestMessagesRoutesAcrossProvidersAndAccounts(t *testing.T) {
	f := newMigrationFixtureWithCostMap(t, costmap.New())
	team := f.create(t, "/admin/teams", `{"name":"messages-apps"}`)
	app := f.create(t, "/admin/applications", fmt.Sprintf(`{"name":"Anthropic SDK","team_id":%.0f}`, team["id"]))
	key := f.create(t, fmt.Sprintf("/admin/applications/%.0f/keys", app["id"]), `{"name":"SDK key","allowed_models":["openai","openrouter","anthropic","ollama","unknown"]}`)
	token := key["key"].(string)
	for _, name := range []string{"openai", "openrouter", "anthropic", "ollama", "unknown"} {
		status, _, data := f.request(t, "PATCH", "/admin/models/"+name+"/costs", `{"input_cost_per_token":0.001,"output_cost_per_token":0.002}`, "", true)
		if status != 200 {
			t.Fatal(status, string(data))
		}
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", name, stream), func(t *testing.T) {
				body := fmt.Sprintf(`{"model":%q,"max_tokens":20,"system":"Be brief","messages":[{"role":"user","content":"hello"}],"metadata":{"user_id":"sdk-user","session_id":"body-session"},"stream":%t}`, name, stream)
				status, headers, data := messagesRequest(t, f, body, token)
				if status != 200 {
					t.Fatalf("messages: %d %s", status, data)
				}
				if headers.Get("request-id") == "" || headers.Get("request-id") != headers.Get("X-Request-ID") {
					t.Fatal("request ID missing", headers)
				}
				if headers.Get("X-Provider-Used") == "" {
					t.Fatal("route metadata missing", headers)
				}
				if stream {
					if headers.Get("Content-Type") != "text/event-stream" || !strings.Contains(string(data), "event: message_stop") || !strings.Contains(string(data), `"text":"hello"`) || strings.Contains(string(data), "[DONE]") {
						t.Fatalf("bad Messages SSE: %s", data)
					}
				} else {
					var response map[string]any
					if err := json.Unmarshal(data, &response); err != nil {
						t.Fatal(err)
					}
					if response["type"] != "message" || response["role"] != "assistant" || response["model"] != name || response["stop_reason"] != "end_turn" || response["content"].([]any)[0].(map[string]any)["text"] != "hello" {
						t.Fatal(response)
					}
				}
				row := f.log(t, headers.Get("request-id"))
				known := !(name == "unknown" && stream)
				if row.Endpoint != "/v1/messages" || row.IsStreaming != stream || row.APIKeyID == nil || row.SessionID != "messages-session" || row.UsageKnown != known {
					t.Fatalf("accounting: %+v", row)
				}
				if known && (row.InputTokens != 10 || row.OutputTokens != 3 || math.Abs(row.TotalCost-0.016) > 1e-9) {
					t.Fatalf("cost/usage lost: %+v", row)
				}
				if stream && row.TTFTMs == nil {
					t.Fatal("TTFT missing")
				}
				captured := <-f.captured
				if captured["model"] != "wire-"+name || captured["max_tokens"] != float64(20) {
					t.Fatal("routing or max_tokens lost", captured)
				}
				if captured["session_id"] != nil || captured["metadata"] != nil {
					t.Fatal("proxy metadata leaked", captured)
				}
			})
		}
	}
	// The same application key is restricted regardless of the inbound dialect.
	status, _, data := messagesRequest(t, f, `{"model":"error400","max_tokens":2,"messages":[{"role":"user","content":"hello"}]}`, token)
	if status != 403 || !strings.Contains(string(data), `"type":"permission_error"`) {
		t.Fatal(status, string(data))
	}
	status, _, _ = f.request(t, "DELETE", fmt.Sprintf("/admin/api-keys/%.0f", key["id"]), "", "", true)
	if status != 204 {
		t.Fatal(status)
	}
	status, _, data = messagesRequest(t, f, `{"model":"openai","max_tokens":2,"messages":[{"role":"user","content":"hello"}]}`, token)
	if status != 401 || !strings.Contains(string(data), `"type":"authentication_error"`) {
		t.Fatal(status, string(data))
	}
}

func TestMessagesHTTPErrorContract(t *testing.T) {
	f := newMigrationFixture(t)
	for _, test := range []struct {
		model, key string
		status     int
		errorType  string
	}{
		{"openai", "", 401, "authentication_error"},
		{"missing", "master-only", 404, "not_found_error"},
		{"error400", "master-only", 400, "invalid_request_error"},
		{"error429", "master-only", 429, "rate_limit_error"},
	} {
		status, headers, data := messagesRequest(t, f, fmt.Sprintf(`{"model":%q,"max_tokens":2,"messages":[{"role":"user","content":"hello"}]}`, test.model), test.key)
		var response map[string]any
		if err := json.Unmarshal(data, &response); err != nil {
			t.Fatal(err)
		}
		if status != test.status || response["type"] != "error" || response["error"].(map[string]any)["type"] != test.errorType || response["request_id"] != headers.Get("request-id") {
			t.Fatal(status, string(data))
		}
		if strings.HasPrefix(test.model, "error") && headers.Get("Retry-After") != "7" {
			t.Fatal("Retry-After lost")
		}
	}
	for _, body := range []string{`{`, `{"model":"openai","max_tokens":1,"messages":[{"role":"user","content":"hi"}]} {}`, `{"model":"openai","messages":[{"role":"user","content":"hi"}]}`, `{"model":"openai","max_tokens":10,"thinking":{"type":"enabled"},"messages":[{"role":"user","content":"hi"}]}`} {
		status, _, data := messagesRequest(t, f, body, "master-only")
		if status != 400 || !strings.Contains(string(data), `"type":"invalid_request_error"`) {
			t.Fatal(status, string(data))
		}
	}
	// Existing Bearer clients can use the endpoint too.
	status, _, data := f.request(t, "POST", "/v1/messages", `{"model":"openai","max_tokens":2,"messages":[{"role":"user","content":"hello"}]}`, "master-only", false)
	if status != 200 {
		t.Fatal(status, string(data))
	}
}

func TestMessagesToolsAndImagesReachProviders(t *testing.T) {
	f := newMigrationFixture(t)
	for _, name := range []string{"openai", "openrouter", "anthropic", "ollama"} {
		t.Run(name, func(t *testing.T) {
			body := fmt.Sprintf(`{
 "model":%q,"max_tokens":20,
 "messages":[
 {"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"YWJj"}},{"type":"text","text":"Describe this"}]},
 {"role":"assistant","content":[{"type":"tool_use","id":"call-a","name":"lookup","input":{"x":1}}]},
 {"role":"user","content":[{"type":"tool_result","tool_use_id":"call-a","content":"result"},{"type":"text","text":"continue"}]}
 ],
 "tools":[{"name":"lookup","input_schema":{"type":"object","properties":{"x":{"type":"integer"}}}}],
 "tool_choice":{"type":"any","disable_parallel_tool_use":true}
}`, name)
			status, _, data := messagesRequest(t, f, body, "master-only")
			if status != 200 {
				t.Fatal(status, string(data))
			}
			captured := <-f.captured
			messages := captured["messages"].([]any)
			first := messages[0].(map[string]any)["content"].([]any)[0].(map[string]any)
			tools := captured["tools"].([]any)
			if name == "anthropic" {
				if first["type"] != "image" || first["source"].(map[string]any)["data"] != "YWJj" || tools[0].(map[string]any)["input_schema"] == nil || captured["tool_choice"].(map[string]any)["disable_parallel_tool_use"] != true {
					t.Fatal(captured)
				}
				tool := messages[1].(map[string]any)["content"].([]any)[0].(map[string]any)
				result := messages[2].(map[string]any)["content"].([]any)[0].(map[string]any)
				if tool["type"] != "tool_use" || result["type"] != "tool_result" || result["tool_use_id"] != "call-a" {
					t.Fatal(captured)
				}
			} else {
				if first["type"] != "image_url" || first["image_url"].(map[string]any)["url"] != "data:image/png;base64,YWJj" || captured["parallel_tool_calls"] != false || captured["tool_choice"] != "required" {
					t.Fatal(captured)
				}
				if messages[2].(map[string]any)["role"] != "tool" || messages[2].(map[string]any)["tool_call_id"] != "call-a" {
					t.Fatal(captured)
				}
			}
		})
	}
}
