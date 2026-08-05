package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestResponses_ProviderWithoutResponsesSupport(t *testing.T) {
	// admin_test.go's package init() registers a no-op fake provider under "openai"
	// that does not implement provider.ResponsesProvider.
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

	h := Responses(r, nil, nil, nil, nil, config.GeneralSettings{})
	body, _ := json.Marshal(map[string]any{"model": "no-responses-model", "input": "hi"})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", w.Code, w.Body.String())
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
	body, _ := json.Marshal(map[string]any{"model": "test-model", "input": "hi", "background": true})
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
	if job == nil || job.Status != "queued" {
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
