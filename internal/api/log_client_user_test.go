package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// X-User-ID reaches the request log and its filter even with tracing disabled.
func TestClientUserIDLoggedAndFilterable(t *testing.T) {
	f := newDecisionFixture(t)
	for _, call := range []struct{ path, body, user string }{
		{"/v1/decisions", decisionBody("luna"), "owui-user-1"},
		{"/v1/decisions", decisionBody("luna"), "owui-user-2"},
		// Chat completions are logged through the accounting collector, not logRequest.
		{"/v1/chat/completions", `{"model":"chat","messages":[{"role":"user","content":"hi"}]}`, "owui-user-1"},
	} {
		req, err := http.NewRequest("POST", f.url+call.path, bytes.NewBufferString(call.body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer master-only")
		req.Header.Set("X-User-ID", call.user)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status %d", call.path, resp.StatusCode)
		}
	}
	var page struct {
		Total int `json:"total"`
		Logs  []struct {
			RequestID    string `json:"request_id"`
			Endpoint     string `json:"endpoint"`
			ClientUserID string `json:"client_user_id"`
		} `json:"logs"`
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		status, _, body := f.request(t, "GET", "/admin/logs?client_user_id=owui-user-1", "", "", true)
		if status != http.StatusOK {
			t.Fatalf("logs: %d %s", status, body)
		}
		if err := json.Unmarshal(body, &page); err != nil {
			t.Fatal(err)
		}
		if page.Total == 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	endpoints := map[string]bool{}
	for _, l := range page.Logs {
		if l.ClientUserID != "owui-user-1" {
			t.Fatalf("filtered logs: %+v", page)
		}
		endpoints[l.Endpoint] = true
	}
	if page.Total != 2 || !endpoints["/v1/decisions"] || !endpoints["/v1/chat/completions"] {
		t.Fatalf("filtered logs: %+v", page)
	}
	status, _, body := f.request(t, "GET", "/admin/logs/"+page.Logs[0].RequestID, "", "", true)
	var detail struct {
		ClientUserID string `json:"client_user_id"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &detail) != nil || detail.ClientUserID != "owui-user-1" {
		t.Fatalf("detail: %d %s", status, body)
	}
}
