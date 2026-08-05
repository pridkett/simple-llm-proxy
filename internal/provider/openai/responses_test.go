package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

func testProvider(baseURL string) *Provider {
	return newProvider(provider.ProviderOptions{APIKey: "test-key", APIBase: baseURL})
}

func TestCreateResponse_PostsToResponsesPath(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody model.ResponsesRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(model.ResponsesResponse{ID: "resp_1", Status: "completed", Output: []model.ResponseOutputItem{
			{Type: "message", Role: "assistant", Content: []model.ResponseContentPart{{Type: "output_text", Text: "hi"}}},
		}})
	}))
	defer ts.Close()

	p := testProvider(ts.URL)
	resp, err := p.CreateResponse(context.Background(), &model.ResponsesRequest{Model: "gpt-5", Input: "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/responses" {
		t.Errorf("path = %q, want /responses", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotBody.Stream {
		t.Error("expected stream=false to be forced on non-streaming CreateResponse")
	}
	if resp.ID != "resp_1" || resp.Status != "completed" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestCreateResponse_RateLimited(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()

	p := testProvider(ts.URL)
	_, err := p.CreateResponse(context.Background(), &model.ResponsesRequest{Model: "gpt-5", Input: "hi"})
	var rlErr *provider.RateLimitError
	if !errors.As(err, &rlErr) {
		t.Fatalf("expected RateLimitError, got %v", err)
	}
}

func TestCreateResponse_ErrorResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(model.APIError{Error: model.ErrorDetail{Message: "bad input", Type: "invalid_request_error"}})
	}))
	defer ts.Close()

	p := testProvider(ts.URL)
	_, err := p.CreateResponse(context.Background(), &model.ResponsesRequest{Model: "gpt-5", Input: "hi"})
	if err == nil || !strings.Contains(err.Error(), "bad input") {
		t.Fatalf("expected error containing 'bad input', got %v", err)
	}
}

func TestGetResponse(t *testing.T) {
	var gotPath, gotMethod string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		json.NewEncoder(w).Encode(model.ResponsesResponse{ID: "resp_1", Status: "in_progress"})
	}))
	defer ts.Close()

	p := testProvider(ts.URL)
	resp, err := p.GetResponse(context.Background(), "resp_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %s, want GET", gotMethod)
	}
	if gotPath != "/responses/resp_1" {
		t.Errorf("path = %q, want /responses/resp_1", gotPath)
	}
	if resp.Status != "in_progress" {
		t.Errorf("unexpected status: %s", resp.Status)
	}
}

func TestCancelResponse(t *testing.T) {
	var gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(model.ResponsesResponse{ID: "resp_1", Status: "cancelled"})
	}))
	defer ts.Close()

	p := testProvider(ts.URL)
	resp, err := p.CancelResponse(context.Background(), "resp_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/responses/resp_1/cancel" {
		t.Errorf("path = %q, want /responses/resp_1/cancel", gotPath)
	}
	if resp.Status != "cancelled" {
		t.Errorf("unexpected status: %s", resp.Status)
	}
}

func TestCreateResponseStream(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		events := []model.ResponsesStreamEvent{
			{Type: "response.created"},
			{Type: "response.output_text.delta", Delta: "hi"},
			{Type: "response.completed", Response: &model.ResponsesResponse{ID: "resp_1", Status: "completed", Usage: &model.Usage{PromptTokens: 1, CompletionTokens: 2}}},
		}
		for _, e := range events {
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
	}))
	defer ts.Close()

	p := testProvider(ts.URL)
	stream, err := p.CreateResponseStream(context.Background(), &model.ResponsesRequest{Model: "gpt-5", Input: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	var types []string
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected stream error: %v", err)
		}
		types = append(types, event.Type)
	}

	want := []string{"response.created", "response.output_text.delta", "response.completed"}
	if len(types) != len(want) {
		t.Fatalf("got %d events, want %d: %v", len(types), len(want), types)
	}
	for i, w := range want {
		if types[i] != w {
			t.Errorf("event[%d] = %q, want %q", i, types[i], w)
		}
	}
}

func TestCreateResponseStream_RateLimited(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()

	p := testProvider(ts.URL)
	_, err := p.CreateResponseStream(context.Background(), &model.ResponsesRequest{Model: "gpt-5", Input: "hi"})
	var rlErr *provider.RateLimitError
	if !errors.As(err, &rlErr) {
		t.Fatalf("expected RateLimitError, got %v", err)
	}
}

func TestCreateResponseStream_ErrorStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(model.APIError{Error: model.ErrorDetail{Message: "boom"}})
	}))
	defer ts.Close()

	p := testProvider(ts.URL)
	_, err := p.CreateResponseStream(context.Background(), &model.ResponsesRequest{Model: "gpt-5", Input: "hi"})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected error containing 'boom', got %v", err)
	}
}

func TestOpenAIProviderImplementsResponsesProvider(t *testing.T) {
	var _ provider.ResponsesProvider = testProvider("http://example.invalid")
}
