package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestOpenAIRealtimeWebSocket(t *testing.T) {
	for _, tc := range []struct{ path, query, event, expected string }{
		{"/v1/realtime", "model=openai", `{"type":"session.update","session":{"model":"openai","instructions":"hello"}}`, `"model":"wire-openai"`},
		{"/v1/realtime", "intent=transcription&model=openai", `{"type":"session.update","session":{"type":"transcription","audio":{"input":{"transcription":{"model":"other","language":"en"}}}}}`, `"model":"wire-openai"`},
		{"/v1/realtime/translations", "model=openai", `{"type":"session.update","session":{"audio":{"output":{"language":"fr"}}}}`, `"language":"fr"`},
	} {
		t.Run(tc.path+"?"+tc.query, func(t *testing.T) {
			transcribe := strings.Contains(tc.query, "intent=transcription")
			f := newMigrationFixtureWithCostMap(t, nil, func(w http.ResponseWriter, r *http.Request) {
				model := r.URL.Query().Get("model")
				intent := r.URL.Query().Get("intent")
				if r.URL.Path != tc.path || r.Header.Get("Authorization") != "Bearer upstream-only" ||
					(!transcribe && (model != "wire-openai" || intent != "")) ||
					(transcribe && (model != "" || intent != "transcription")) {
					t.Errorf("upstream handshake %s %v", r.URL.String(), r.Header)
				}
				upgrader := websocket.Upgrader{}
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				conn.SetReadDeadline(time.Now().Add(5 * time.Second))
				if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"session.created","session":{"id":"s"}}`)); err != nil {
					t.Error(err)
					return
				}
				if transcribe {
					_, initial, err := conn.ReadMessage()
					if err != nil || !strings.Contains(string(initial), `"transcription":{"model":"wire-openai"}`) {
						t.Errorf("transcription initialization: %s %v", initial, err)
						return
					}
				}
				_, event, err := conn.ReadMessage()
				if err != nil || !strings.Contains(string(event), tc.expected) || strings.Contains(string(event), `"model":"other"`) {
					t.Errorf("upstream event: %s %v", event, err)
					return
				}
				_, audio, err := conn.ReadMessage()
				if err != nil || string(audio) != `{"audio":"eA==","type":"input_audio_buffer.append"}` {
					t.Errorf("upstream audio: %s %v", audio, err)
					return
				}
				for _, response := range []string{
					`{"type":"response.output_audio.delta","delta":"eQ=="}`,
					`{"type":"response.done","response":{"id":"r1","usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}`,
					`{"type":"response.done","response":{"id":"r2","usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}}`,
				} {
					if err := conn.WriteMessage(websocket.TextMessage, []byte(response)); err != nil {
						t.Error(err)
						return
					}
				}
				_, _, err = conn.ReadMessage()
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
					t.Errorf("upstream close: %v", err)
				}
			})
			team := f.create(t, "/admin/teams", `{"name":"voice"}`)
			app := f.create(t, "/admin/applications", fmt.Sprintf(`{"name":"voice","team_id":%.0f}`, team["id"]))
			token := f.create(t, fmt.Sprintf("/admin/applications/%.0f/keys", app["id"]), `{"name":"voice","allowed_models":["openai"]}`)["key"].(string)
			conn, resp, err := dialMedia(t, f, tc.path+"?"+tc.query, token)
			if err != nil {
				t.Fatalf("proxy WebSocket: %v (response %v)", err, resp)
			}
			defer conn.Close()
			if resp.StatusCode != http.StatusSwitchingProtocols || resp.Header.Get("X-LLM-Accounting-ID") == "" {
				t.Fatal("missing upgrade metadata", resp.Header)
			}
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			_, created, err := conn.ReadMessage()
			if err != nil || !strings.Contains(string(created), "session.created") {
				t.Fatalf("client session: %s %v", created, err)
			}
			for _, event := range []string{tc.event, `{"type":"input_audio_buffer.append","audio":"eA=="}`} {
				if err := conn.WriteMessage(websocket.TextMessage, []byte(event)); err != nil {
					t.Fatal(err)
				}
			}
			for _, expected := range []string{`"delta":"eQ=="`, `"id":"r1"`, `"id":"r2"`} {
				_, event, err := conn.ReadMessage()
				if err != nil || !strings.Contains(string(event), expected) {
					t.Fatalf("client event: %s %v", event, err)
				}
			}
			if err := conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			row := f.log(t, resp.Header.Get("X-Request-ID"))
			if row.StatusCode != 101 || !row.IsStreaming || row.APIKeyID == nil || !row.UsageKnown || row.InputTokens != 7 || row.OutputTokens != 3 {
				t.Errorf("session accounting: %+v", row)
			}
		})
	}
}

func TestOpenAIRealtimeRejectsUnscopedAccess(t *testing.T) {
	f := newMigrationFixtureWithCostMap(t, nil, func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, data, err := conn.ReadMessage()
		if err == nil {
			t.Errorf("forbidden event reached upstream: %s", data)
		}
	})
	team := f.create(t, "/admin/teams", `{"name":"voice"}`)
	app := f.create(t, "/admin/applications", fmt.Sprintf(`{"name":"voice","team_id":%.0f}`, team["id"]))
	keyPath := fmt.Sprintf("/admin/applications/%.0f/keys", app["id"])
	blocked := f.create(t, keyPath, `{"name":"blocked","allowed_models":["openrouter"]}`)["key"].(string)
	capped := f.create(t, keyPath, `{"name":"capped","max_budget":1}`)["key"].(string)
	for _, tc := range []struct {
		query, key string
		status     int
	}{
		{"model=openai", "", 401},
		{"model=openai", blocked, 403},
		{"model=openai", capped, 403},
		{"model=openai&call_id=someone-elses-call", "master-only", 400},
		{"model=openai&model=openrouter", "master-only", 400},
	} {
		_, resp, err := dialMedia(t, f, "/v1/realtime?"+tc.query, tc.key)
		if err == nil || resp == nil || resp.StatusCode != tc.status {
			t.Errorf("%s: response %v error %v, want %d", tc.query, resp, err, tc.status)
		}
	}
	for _, event := range []string{
		`{"type":"session.update","session":{"model":"unauthorized"}}`,
		`{"type":"session.update","TYPE":"response.create","session":{"model":"unauthorized"}}`,
	} {
		conn, _, err := dialMedia(t, f, "/v1/realtime?model=openai", "master-only")
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if err := conn.WriteMessage(websocket.TextMessage, []byte(event)); err != nil {
			t.Fatal(err)
		}
		_, _, err = conn.ReadMessage()
		if !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
			t.Fatal("expected policy violation", err)
		}
	}

}

func dialMedia(t *testing.T, f *migrationFixture, path, key string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	headers := make(http.Header)
	if key != "" {
		headers.Set("Authorization", "Bearer "+key)
	}
	conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(f.url, "http")+path, headers)
	if err != nil && resp != nil {
		resp.Body.Close()
	}
	return conn, resp, err
}

func TestOpenAIResponsesImagesAndChatAudio(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprintf("streaming=%t", streaming), func(t *testing.T) {
			f := newMigrationFixtureWithCostMap(t, nil, func(w http.ResponseWriter, r *http.Request) {
				var request map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				if string(request["model"]) != `"wire-openai"` {
					t.Error("wrong upstream model", request)
				}
				if r.URL.Path == "/v1/responses" {
					var tools []map[string]json.RawMessage
					if err := json.Unmarshal(request["tools"], &tools); err != nil || len(tools) != 2 ||
						string(tools[0]["model"]) != `"gpt-image-2.6-sunburst"` || tools[0]["function"] != nil || tools[1]["function"] != nil ||
						string(request["safety_identifier"]) != `"test-user"` {
						t.Errorf("image tool wire request changed: %s", request["tools"])
					}
					const result = `{"id":"r1","object":"response","status":"completed","output":[{"id":"ig","type":"image_generation_call","result":"aW1hZ2U=","revised_prompt":"circle"}]}`
					if streaming {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, "data: {\"type\":\"response.image_generation_call.partial_image\",\"partial_image_b64\":\"cHJldmlldw==\",\"partial_image_index\":0}\n\n")
						fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":"+result+"}\n\n")
						return
					}
					fmt.Fprint(w, result)
					return
				}
				if string(request["modalities"]) != `["text","audio"]` ||
					!strings.Contains(string(request["audio"]), `"voice":"alloy"`) ||
					!strings.Contains(string(request["messages"]), `"type":"input_audio"`) {
					t.Error("chat audio request changed", request)
				}
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"id\":\"chat\",\"choices\":[{\"index\":0,\"delta\":{\"audio\":{\"data\":\"b3V0\"}}}]}\n\ndata: [DONE]\n\n")
					return
				}
				fmt.Fprint(w, `{"id":"chat","choices":[{"index":0,"message":{"role":"assistant","content":null,"audio":{"id":"a","data":"b3V0"}}}]}`)
			})
			request := fmt.Sprintf(`{"model":"openai","input":"draw","stream":%t,"safety_identifier":"test-user","tools":[{"type":"image_generation","model":"gpt-image-2.6-sunburst","partial_images":1},{"type":"function","name":"get_name","parameters":{"type":"object"}}]}`, streaming)
			status, _, body := f.request(t, http.MethodPost, "/v1/responses", request, "master-only", false)
			if status != 200 || !strings.Contains(string(body), `"result":"aW1hZ2U="`) || !strings.Contains(string(body), `"revised_prompt":"circle"`) ||
				(streaming && !strings.Contains(string(body), `"partial_image_b64":"cHJldmlldw=="`)) {
				t.Fatalf("image payload lost: %d %s", status, body)
			}
			audio := fmt.Sprintf(`{"model":"openai","modalities":["text","audio"],"audio":{"voice":"alloy","format":"wav"},"messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"aW4=","format":"wav"}}]}],"stream":%t}`, streaming)
			status, _, body = f.request(t, http.MethodPost, "/v1/chat/completions", audio, "master-only", false)
			if status != 200 || !strings.Contains(string(body), `"data":"b3V0"`) {
				t.Fatalf("chat audio payload lost: %d %s", status, body)
			}
		})
	}
}
