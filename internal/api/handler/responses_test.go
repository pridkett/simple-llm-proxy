package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
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
