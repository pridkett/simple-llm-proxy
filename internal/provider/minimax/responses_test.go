package minimax

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

func TestMiniMaxImplementsResponsesProvider(t *testing.T) {
	p := New(provider.ProviderOptions{APIKey: "test-key"})
	if _, ok := p.(provider.ResponsesProvider); !ok {
		t.Fatal("MiniMax provider must implement provider.ResponsesProvider")
	}
}

func TestMiniMaxCreateResponse(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody model.ResponsesRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(model.ResponsesResponse{ID: "resp_mm", Status: "completed", Output: []model.ResponseOutputItem{
			{Type: "message", Role: "assistant", Content: []model.ResponseContentPart{{Type: "output_text", Text: "hi"}}},
		}})
	}))
	defer ts.Close()

	p := New(provider.ProviderOptions{APIKey: "test-key", APIBase: ts.URL + "/v1/"}).(provider.ResponsesProvider)
	resp, err := p.CreateResponse(context.Background(), &model.ResponsesRequest{Model: "MiniMax-M3.1-Flash", Input: "hello", Stream: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/v1/responses" {
		t.Errorf("path = %q, want /v1/responses", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotBody.Model != "MiniMax-M3.1-Flash" || gotBody.Stream {
		t.Errorf("unexpected upstream body: model=%q stream=%v", gotBody.Model, gotBody.Stream)
	}
	if resp.ID != "resp_mm" || resp.Status != "completed" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestMiniMaxCreateResponseStream(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path = %q, want /v1/responses", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n")
		fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_mm\",\"status\":\"completed\"}}\n\n")
	}))
	defer ts.Close()

	p := New(provider.ProviderOptions{APIKey: "test-key", APIBase: ts.URL + "/v1"}).(provider.ResponsesProvider)
	stream, err := p.CreateResponseStream(context.Background(), &model.ResponsesRequest{Model: "MiniMax-M3.1-Flash", Input: "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	var types []string
	for {
		ev, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected stream error: %v", err)
		}
		types = append(types, ev.Type)
	}
	if len(types) != 2 || types[0] != "response.output_text.delta" || types[1] != "response.completed" {
		t.Errorf("event types = %v", types)
	}
}

func TestMiniMaxResponsesJobsUnsupported(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected upstream call: %s %s", r.Method, r.URL.Path)
	}))
	defer ts.Close()

	p := New(provider.ProviderOptions{APIKey: "test-key", APIBase: ts.URL}).(provider.ResponsesProvider)
	if _, err := p.CreateResponse(context.Background(), &model.ResponsesRequest{Model: "MiniMax-M3.1-Flash", Input: "hi", Background: true}); !errors.Is(err, errNoBackgroundResponses) {
		t.Errorf("background CreateResponse error = %v, want errNoBackgroundResponses", err)
	}
	if _, err := p.GetResponse(context.Background(), "resp_mm"); !errors.Is(err, errNoBackgroundResponses) {
		t.Errorf("GetResponse error = %v, want errNoBackgroundResponses", err)
	}
	if _, err := p.CancelResponse(context.Background(), "resp_mm"); !errors.Is(err, errNoBackgroundResponses) {
		t.Errorf("CancelResponse error = %v, want errNoBackgroundResponses", err)
	}
}
