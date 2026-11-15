package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/provider/openai"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// registered under a name distinct from "openai": admin_test.go's package init()
// registers a no-op fake provider under "openai" for other handler tests, so
// Responses-specific tests use their own provider name to reach the real
// openai.Provider (and its ResponsesProvider implementation) pointed at an
// httptest server via APIBase.
func init() {
	provider.Register("responsestest", openai.New)
}

// jobStore is a mockStorage that additionally persists ResponsesJob rows in memory,
// used to exercise the create -> poll -> cancel lifecycle end to end.
type jobStore struct {
	mockStorage
	mu   sync.Mutex
	jobs map[string]*storage.ResponsesJob
}

func newJobStore() *jobStore {
	return &jobStore{jobs: make(map[string]*storage.ResponsesJob)}
}

func (s *jobStore) CreateResponsesJob(_ context.Context, job *storage.ResponsesJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *job
	s.jobs[job.ID] = &cp
	return nil
}

func (s *jobStore) GetResponsesJob(_ context.Context, id string) (*storage.ResponsesJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, nil
	}
	cp := *job
	return &cp, nil
}

func (s *jobStore) UpdateResponsesJob(_ context.Context, id, status string, responseJSON, errorJSON *string, completedAt *time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil
	}
	job.Status = status
	job.ResponseJSON = responseJSON
	job.ErrorJSON = errorJSON
	job.CompletedAt = completedAt
	return nil
}

func (s *jobStore) ListPendingResponsesJobs(_ context.Context) ([]*storage.ResponsesJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*storage.ResponsesJob
	for _, j := range s.jobs {
		if j.Status != "completed" && j.Status != "failed" && j.Status != "cancelled" && j.Status != "incomplete" {
			cp := *j
			out = append(out, &cp)
		}
	}
	return out, nil
}

// responsesRouterForTest builds a router with a single "test-model" deployment
// pointed at an httptest server standing in for the OpenAI Responses API.
func responsesRouterForTest(t *testing.T, apiBase string) *router.Router {
	t.Helper()
	cfg := &config.Config{
		ModelList: []config.ModelConfig{
			{
				ModelName: "test-model",
				LiteLLMParams: config.LiteLLMParams{
					Model:   "responsestest/gpt-5",
					APIKey:  "test-key",
					APIBase: apiBase,
				},
			},
		},
		RouterSettings: config.RouterSettings{
			RoutingStrategy: "simple-shuffle",
			NumRetries:      0,
			AllowedFails:    3,
			CooldownTime:    30 * time.Second,
		},
	}
	r, err := router.New(cfg, nil)
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	return r
}

func withIDParam(req *http.Request, id string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestResponses_Synchronous(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":     "resp_sync_1",
			"status": "completed",
			"usage":  map[string]any{"prompt_tokens": 3, "completion_tokens": 4, "total_tokens": 7},
		})
	}))
	defer ts.Close()

	r := responsesRouterForTest(t, ts.URL)
	store := newJobStore()
	sa := keystore.NewSpendAccumulator()
	cm := costmap.New()

	h := Responses(r, store, sa, cm, nil, config.GeneralSettings{})

	body, _ := json.Marshal(map[string]any{"model": "test-model", "input": "hello"})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var got map[string]any
	json.Unmarshal(w.Body.Bytes(), &got)
	if got["id"] != "resp_sync_1" || got["status"] != "completed" {
		t.Errorf("unexpected response: %+v", got)
	}

	// Synchronous calls must not create a responses_jobs row.
	if len(store.jobs) != 0 {
		t.Errorf("expected no persisted jobs for sync call, got %d", len(store.jobs))
	}
}

func TestResponses_MissingModel(t *testing.T) {
	r := responsesRouterForTest(t, "http://unused")
	h := Responses(r, nil, nil, nil, nil, config.GeneralSettings{})

	body, _ := json.Marshal(map[string]any{"input": "hi"})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestResponses_MissingInput(t *testing.T) {
	r := responsesRouterForTest(t, "http://unused")
	h := Responses(r, nil, nil, nil, nil, config.GeneralSettings{})

	body, _ := json.Marshal(map[string]any{"model": "test-model"})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// chatOnlyProvider speaks only chat completions, like Anthropic or Ollama.
type chatOnlyProvider struct{}

func (chatOnlyProvider) Name() string { return "chatonlytest" }
func (chatOnlyProvider) ChatCompletion(_ context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	return &model.ChatCompletionResponse{
		Model:   req.Model,
		Choices: []model.Choice{{Message: &model.Message{Role: "assistant", Content: "echo: " + req.Messages[len(req.Messages)-1].Content.(string)}, FinishReason: "stop"}},
		Usage:   &model.Usage{PromptTokens: 4, CompletionTokens: 2, TotalTokens: 6},
	}, nil
}
func (chatOnlyProvider) ChatCompletionStream(context.Context, *model.ChatCompletionRequest) (provider.Stream, error) {
	chunks := make(chan *model.StreamChunk, 3)
	chunks <- &model.StreamChunk{Choices: []model.Choice{{Delta: &model.Delta{Content: "hi "}}}}
	chunks <- &model.StreamChunk{Choices: []model.Choice{{Delta: &model.Delta{Content: "there"}, FinishReason: "stop"}}}
	chunks <- &model.StreamChunk{Usage: &model.Usage{PromptTokens: 4, CompletionTokens: 2, TotalTokens: 6}}
	close(chunks)
	return provider.NewStream(chunks, make(chan error), nil), nil
}
func (chatOnlyProvider) Embeddings(context.Context, *model.EmbeddingsRequest) (*model.EmbeddingsResponse, error) {
	return nil, fmt.Errorf("unsupported")
}
func (chatOnlyProvider) SupportsEmbeddings() bool { return false }

func init() {
	provider.Register("chatonlytest", func(provider.ProviderOptions) provider.Provider { return chatOnlyProvider{} })
}

func chatOnlyResponses(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	cfg := &config.Config{
		ModelList:      []config.ModelConfig{{ModelName: "chat-only", LiteLLMParams: config.LiteLLMParams{Model: "chatonlytest/real-model"}}},
		RouterSettings: config.RouterSettings{RoutingStrategy: "simple-shuffle", CooldownTime: 30 * time.Second, AllowedFails: 1},
	}
	r, err := router.New(cfg, nil)
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	h := Responses(r, nil, nil, nil, nil, config.GeneralSettings{})
	// Two calls: a rejected request must leave the deployment available.
	var w *httptest.ResponseRecorder
	for range 2 {
		w = httptest.NewRecorder()
		h(w, httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(body)))
	}
	return w
}

// ADR 020: a provider without the Responses API answers through translation.
func TestResponses_ChatOnlyProviderIsTranslated(t *testing.T) {
	w := chatOnlyResponses(t, `{"model":"chat-only","instructions":"be kind","input":"ping"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var got struct {
		Object string `json:"object"`
		Status string `json:"status"`
		Model  string `json:"model"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage map[string]any `json:"usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Object != "response" || got.Status != "completed" || got.Model != "real-model" {
		t.Fatalf("unexpected envelope: %s", w.Body.String())
	}
	if len(got.Output) != 1 || got.Output[0].Type != "message" || got.Output[0].Content[0].Text != "echo: ping" {
		t.Fatalf("unexpected output: %s", w.Body.String())
	}
	if got.Usage["input_tokens"] != 4.0 || got.Usage["output_tokens"] != 2.0 {
		t.Fatalf("unexpected usage: %s", w.Body.String())
	}
}

func TestResponses_ChatOnlyProviderStreamsResponsesEvents(t *testing.T) {
	w := chatOnlyResponses(t, `{"model":"chat-only","input":"ping","stream":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		"event: response.created\n",
		`"sequence_number":0`,
		"event: response.output_text.delta\n",
		`"delta":"hi "`,
		"event: response.completed\n",
		`"text":"hi there"`,
		`"input_tokens":4`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("stream is missing %q\n%s", want, body)
		}
	}
}

func TestResponses_ChatOnlyProviderRejectsStatefulRequests(t *testing.T) {
	// chatOnlyResponses sends the request twice with allowed_fails=1: the second
	// 400 proves the first did not put the deployment in cooldown.
	w := chatOnlyResponses(t, `{"model":"chat-only","input":"ping","background":true}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "background responses are not available") {
		t.Fatalf("unexpected error body: %s", w.Body.String())
	}
}

// fakeFailingResponsesProvider is a provider.Provider + provider.ResponsesProvider
// whose CreateResponseStream always returns a stream that fails immediately on
// Recv() with a genuine (non-io.EOF) error — used to deterministically exercise
// the mid-stream failure path without depending on HTTP/chunked-encoding framing
// quirks (a closed connection can itself look like a clean stream end).
type fakeFailingResponsesProvider struct{}

func (fakeFailingResponsesProvider) Name() string { return "fakefailing" }
func (fakeFailingResponsesProvider) ChatCompletion(context.Context, *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	return nil, nil
}
func (fakeFailingResponsesProvider) ChatCompletionStream(context.Context, *model.ChatCompletionRequest) (provider.Stream, error) {
	return nil, nil
}
func (fakeFailingResponsesProvider) Embeddings(context.Context, *model.EmbeddingsRequest) (*model.EmbeddingsResponse, error) {
	return nil, nil
}
func (fakeFailingResponsesProvider) SupportsEmbeddings() bool { return false }
func (fakeFailingResponsesProvider) CreateResponse(context.Context, *model.ResponsesRequest) (*model.ResponsesResponse, error) {
	return nil, nil
}
func (fakeFailingResponsesProvider) CreateResponseStream(context.Context, *model.ResponsesRequest) (provider.ResponsesStream, error) {
	return &failingResponsesStream{}, nil
}
func (fakeFailingResponsesProvider) GetResponse(context.Context, string) (*model.ResponsesResponse, error) {
	return nil, nil
}
func (fakeFailingResponsesProvider) CancelResponse(context.Context, string) (*model.ResponsesResponse, error) {
	return nil, nil
}

type failingResponsesStream struct{}

func (failingResponsesStream) Recv() (*model.ResponsesStreamEvent, error) {
	return nil, errStreamBoom
}
func (failingResponsesStream) Close() error { return nil }

var errStreamBoom = fmt.Errorf("simulated mid-stream transport failure")

func init() {
	provider.Register("fakefailing", func(provider.ProviderOptions) provider.Provider {
		return fakeFailingResponsesProvider{}
	})
}

// TestResponses_StreamFailureReportsFailureNotSuccess is a regression test for a
// bug found in external code review: ReportSuccess was called unconditionally
// before branching into the streaming path, so a stream that failed mid-flight
// still left the deployment looking healthy — the same STREAM-01 class of bug
// ADR-006 fixed in chat.go. With AllowedFails=2, two consecutive mid-stream
// failures must trip cooldown; with the bug present, ReportSuccess resets the
// failure counter to 0 before each ReportFailure increments it back to 1, so
// cooldown never triggers no matter how many requests fail.
func TestResponses_StreamFailureReportsFailureNotSuccess(t *testing.T) {
	cfg := &config.Config{
		ModelList: []config.ModelConfig{
			{ModelName: "test-model", LiteLLMParams: config.LiteLLMParams{Model: "fakefailing/gpt-5"}},
		},
		RouterSettings: config.RouterSettings{RoutingStrategy: "simple-shuffle", CooldownTime: time.Hour, AllowedFails: 2},
	}
	r, err := router.New(cfg, nil)
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}

	h := Responses(r, nil, nil, nil, nil, config.GeneralSettings{})
	makeRequest := func() {
		body, _ := json.Marshal(map[string]any{"model": "test-model", "input": "hi", "stream": true})
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
		w := httptest.NewRecorder()
		h(w, req)
	}

	makeRequest()
	makeRequest()

	statuses := r.GetStatus()
	var found bool
	for _, m := range statuses {
		if m.ModelName != "test-model" {
			continue
		}
		found = true
		for _, d := range m.Deployments {
			if d.FailureCount < 2 {
				t.Errorf("expected failure count >= 2 after two mid-stream failures with AllowedFails=2, got %d (ReportSuccess is likely still firing before the stream completes)", d.FailureCount)
			}
			if d.Status != "cooldown" {
				t.Errorf("expected deployment in cooldown after two mid-stream failures, got status=%q", d.Status)
			}
		}
	}
	if !found {
		t.Fatal("test-model not found in router status")
	}
}

func TestCancelResponseJob_ProviderWithoutResponsesSupport(t *testing.T) {
	cfg := &config.Config{
		ModelList: []config.ModelConfig{
			{ModelName: "no-responses-model", LiteLLMParams: config.LiteLLMParams{Model: "openai/gpt-4"}},
		},
		RouterSettings: config.RouterSettings{RoutingStrategy: "simple-shuffle", CooldownTime: 30 * time.Second, AllowedFails: 3},
	}
	r, err := router.New(cfg, nil)
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}

	store := newJobStore()
	d, err := r.GetDeployment("no-responses-model")
	if err != nil {
		t.Fatalf("GetDeployment: %v", err)
	}
	store.jobs["resp_x"] = &storage.ResponsesJob{
		ID: "resp_x", ModelName: "no-responses-model", DeploymentKey: d.DeploymentKey(), Status: "queued",
	}

	h := CancelResponseJob(r, store)
	req := withIDParam(httptest.NewRequest(http.MethodDelete, "/v1/responses/resp_x", nil), "resp_x")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", w.Code, w.Body.String())
	}
}

// failingGetJobStore always fails GetResponsesJob, to exercise the DB-error
// branches in GetResponseJob/CancelResponseJob distinct from the "not found" path.
type failingGetJobStore struct {
	jobStore
}

func (s *failingGetJobStore) GetResponsesJob(context.Context, string) (*storage.ResponsesJob, error) {
	return nil, fmt.Errorf("simulated db failure")
}

func TestCancelResponseJob_NilStore(t *testing.T) {
	r := responsesRouterForTest(t, "http://unused")
	h := CancelResponseJob(r, nil)
	req := withIDParam(httptest.NewRequest(http.MethodDelete, "/v1/responses/resp_x", nil), "resp_x")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestCancelResponseJob_GetJobError(t *testing.T) {
	r := responsesRouterForTest(t, "http://unused")
	store := &failingGetJobStore{jobStore: *newJobStore()}
	h := CancelResponseJob(r, store)
	req := withIDParam(httptest.NewRequest(http.MethodDelete, "/v1/responses/resp_x", nil), "resp_x")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500, body = %s", w.Code, w.Body.String())
	}
}

func TestGetResponseJob_GetJobError(t *testing.T) {
	store := &failingGetJobStore{jobStore: *newJobStore()}
	h := GetResponseJob(store)
	req := withIDParam(httptest.NewRequest(http.MethodGet, "/v1/responses/resp_x", nil), "resp_x")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500, body = %s", w.Code, w.Body.String())
	}
}

func TestCancelResponseJob_DeploymentNotFound(t *testing.T) {
	r := responsesRouterForTest(t, "http://unused")
	store := newJobStore()
	store.jobs["resp_missing_dep"] = &storage.ResponsesJob{
		ID: "resp_missing_dep", ModelName: "test-model", DeploymentKey: "responsestest:missing:", Status: "queued",
	}

	h := CancelResponseJob(r, store)
	req := withIDParam(httptest.NewRequest(http.MethodDelete, "/v1/responses/resp_missing_dep", nil), "resp_missing_dep")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500, body = %s", w.Code, w.Body.String())
	}
}

func TestCancelResponseJob_UpstreamError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "upstream cancel failed"}})
	}))
	defer ts.Close()

	r := responsesRouterForTest(t, ts.URL)
	store := newJobStore()
	d, err := r.GetDeployment("test-model")
	if err != nil {
		t.Fatalf("GetDeployment: %v", err)
	}
	store.jobs["resp_upstream_err"] = &storage.ResponsesJob{
		ID: "resp_upstream_err", ModelName: "test-model", DeploymentKey: d.DeploymentKey(), Status: "queued",
	}

	h := CancelResponseJob(r, store)
	req := withIDParam(httptest.NewRequest(http.MethodDelete, "/v1/responses/resp_upstream_err", nil), "resp_upstream_err")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502, body = %s", w.Code, w.Body.String())
	}
}

func TestGetResponseJob_ScopedToOwningAPIKey(t *testing.T) {
	store := newJobStore()
	otherKeyID := int64(99)
	store.jobs["resp_scoped"] = &storage.ResponsesJob{ID: "resp_scoped", APIKeyID: &otherKeyID, Status: "queued"}

	h := GetResponseJob(store)
	req := withIDParam(httptest.NewRequest(http.MethodGet, "/v1/responses/resp_scoped", nil), "resp_scoped")
	w := httptest.NewRecorder()
	h(w, req)

	// No API key in context => treated as master key => full access is expected here,
	// so this exercises the "master key always accessible" branch explicitly.
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (master key access), body = %s", w.Code, w.Body.String())
	}
}

func TestResponses_UnknownModel(t *testing.T) {
	r := responsesRouterForTest(t, "http://unused")
	h := Responses(r, nil, nil, nil, nil, config.GeneralSettings{})

	body, _ := json.Marshal(map[string]any{"model": "does-not-exist", "input": "hi"})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404, body=%s", w.Code, w.Body.String())
	}
}

// failingCreateJobStore always fails CreateResponsesJob, to exercise the path
// where an upstream background job was created but the local row never got
// persisted — the client must see an error, not a response_id that 404s forever.
type failingCreateJobStore struct {
	jobStore
}

func (s *failingCreateJobStore) CreateResponsesJob(context.Context, *storage.ResponsesJob) error {
	return fmt.Errorf("simulated storage failure")
}

func TestResponses_BackgroundPersistFailureReturnsError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": "resp_orphan", "status": "queued"})
	}))
	defer ts.Close()

	r := responsesRouterForTest(t, ts.URL)
	store := &failingCreateJobStore{jobStore: *newJobStore()}
	sa := keystore.NewSpendAccumulator()
	cm := costmap.New()

	h := Responses(r, store, sa, cm, nil, config.GeneralSettings{})
	body, _ := json.Marshal(map[string]any{"model": "test-model", "input": "hi", "background": true})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("expected an error status when persisting the background job fails, got 200 with body: %s", w.Body.String())
	}
	if w.Code < 500 {
		t.Errorf("expected a 5xx status (the upstream job exists but the client can never retrieve it), got %d", w.Code)
	}
}

func TestResponses_BackgroundCreateThenPollThenCancel(t *testing.T) {
	var callCount int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/responses":
			callCount++
			json.NewEncoder(w).Encode(map[string]any{"id": "resp_bg_1", "status": "queued"})
		case r.Method == http.MethodPost && r.URL.Path == "/responses/resp_bg_1/cancel":
			json.NewEncoder(w).Encode(map[string]any{"id": "resp_bg_1", "status": "cancelled"})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	r := responsesRouterForTest(t, ts.URL)
	store := newJobStore()
	sa := keystore.NewSpendAccumulator()
	cm := costmap.New()

	h := Responses(r, store, sa, cm, nil, config.GeneralSettings{})
	body, _ := json.Marshal(map[string]any{"model": "test-model", "input": "hi", "background": true, "session_id": "play-background"})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", w.Code, w.Body.String())
	}
	var created map[string]any
	json.Unmarshal(w.Body.Bytes(), &created)
	if created["status"] != "queued" {
		t.Fatalf("expected queued status, got %+v", created)
	}

	if len(store.jobs) != 1 {
		t.Fatalf("expected 1 persisted job, got %d", len(store.jobs))
	}
	job := store.jobs["resp_bg_1"]
	if job == nil || job.Status != "queued" || job.SessionID != "play-background" {
		t.Fatalf("unexpected persisted job: %+v", job)
	}

	// Poll.
	getHandler := GetResponseJob(store)
	getReq := withIDParam(httptest.NewRequest(http.MethodGet, "/v1/responses/resp_bg_1", nil), "resp_bg_1")
	getW := httptest.NewRecorder()
	getHandler(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("poll status = %d, body = %s", getW.Code, getW.Body.String())
	}

	// Cancel.
	cancelHandler := CancelResponseJob(r, store)
	cancelReq := withIDParam(httptest.NewRequest(http.MethodDelete, "/v1/responses/resp_bg_1", nil), "resp_bg_1")
	cancelW := httptest.NewRecorder()
	cancelHandler(cancelW, cancelReq)
	if cancelW.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, body = %s", cancelW.Code, cancelW.Body.String())
	}
	var cancelled map[string]any
	json.Unmarshal(cancelW.Body.Bytes(), &cancelled)
	if cancelled["status"] != "cancelled" {
		t.Errorf("expected cancelled status, got %+v", cancelled)
	}
	if store.jobs["resp_bg_1"].Status != "cancelled" {
		t.Errorf("expected stored job to be cancelled, got %s", store.jobs["resp_bg_1"].Status)
	}
}

func TestGetResponseJob_NotFound(t *testing.T) {
	store := newJobStore()
	h := GetResponseJob(store)
	req := withIDParam(httptest.NewRequest(http.MethodGet, "/v1/responses/nope", nil), "nope")
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestGetResponseJob_NilStore(t *testing.T) {
	h := GetResponseJob(nil)
	req := withIDParam(httptest.NewRequest(http.MethodGet, "/v1/responses/x", nil), "x")
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestResponses_Streaming(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmtEvents := []string{
			`{"type":"response.created"}`,
			`{"type":"response.output_text.delta","delta":"hi"}`,
			`{"type":"response.completed","response":{"id":"resp_stream_1","status":"completed","usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}}`,
		}
		for _, e := range fmtEvents {
			w.Write([]byte("data: " + e + "\n\n"))
			flusher.Flush()
		}
	}))
	defer ts.Close()

	r := responsesRouterForTest(t, ts.URL)
	store := newJobStore()
	sa := keystore.NewSpendAccumulator()
	cm := costmap.New()
	h := Responses(r, store, sa, cm, nil, config.GeneralSettings{})

	body, _ := json.Marshal(map[string]any{"model": "test-model", "input": "hi", "stream": true})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	body2 := w.Body.String()
	for _, want := range []string{"response.created", "response.output_text.delta", "response.completed"} {
		if !bytes.Contains([]byte(body2), []byte(want)) {
			t.Errorf("expected body to contain %q, got: %s", want, body2)
		}
	}
	// OpenAI's Responses API SSE contract pairs each data line with an `event:`
	// line naming the event type; the official SDK dispatches on it. Regression
	// test for a bug found in external code review where only `data:` was emitted.
	for _, wantLine := range []string{
		"event: response.created\ndata:",
		"event: response.output_text.delta\ndata:",
		"event: response.completed\ndata:",
	} {
		if !bytes.Contains([]byte(body2), []byte(wantLine)) {
			t.Errorf("expected an %q line pairing, got: %s", wantLine, body2)
		}
	}
}

func TestCancelResponseJob_AlreadyTerminal(t *testing.T) {
	store := newJobStore()
	respJSON := `{"id":"resp_done","status":"completed"}`
	store.jobs["resp_done"] = &storage.ResponsesJob{
		ID: "resp_done", ModelName: "test-model", Status: "completed", ResponseJSON: &respJSON,
	}

	r := responsesRouterForTest(t, "http://unused")
	h := CancelResponseJob(r, store)
	req := withIDParam(httptest.NewRequest(http.MethodDelete, "/v1/responses/resp_done", nil), "resp_done")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if w.Body.String() != respJSON {
		t.Errorf("expected verbatim stored response, got %s", w.Body.String())
	}
}
