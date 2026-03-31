package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// mockStorage implements storage.Storage for handler tests.
type mockStorage struct {
	upsertedKeyModel string
	upsertedKey      string
	upsertedSpecModel string
	upsertedSpec      string
	upsertKeyErr      error
	upsertSpecErr     error
}

func (m *mockStorage) Initialize(_ context.Context) error { return nil }
func (m *mockStorage) Close() error                        { return nil }
func (m *mockStorage) LogRequest(_ context.Context, _ *storage.RequestLog) error { return nil }
func (m *mockStorage) GetLogs(_ context.Context, _, _ int) ([]*storage.RequestLog, int, error) {
	return nil, 0, nil
}
func (m *mockStorage) UpsertCostMapKey(_ context.Context, modelName, key string) error {
	m.upsertedKeyModel = modelName
	m.upsertedKey = key
	return m.upsertKeyErr
}
func (m *mockStorage) UpsertCustomCostSpec(_ context.Context, modelName, specJSON string) error {
	m.upsertedSpecModel = modelName
	m.upsertedSpec = specJSON
	return m.upsertSpecErr
}
func (m *mockStorage) GetCostOverride(_ context.Context, _ string) (*storage.CostOverride, error) {
	return nil, nil
}
func (m *mockStorage) DeleteCostOverride(_ context.Context, _ string) error { return nil }
func (m *mockStorage) ListCostOverrides(_ context.Context) ([]*storage.CostOverride, error) {
	return nil, nil
}

// Identity CRUD stubs — not exercised by handler tests but required by interface.
func (m *mockStorage) UpsertUser(_ context.Context, _ *storage.User) error { return nil }
func (m *mockStorage) GetUser(_ context.Context, _ string) (*storage.User, error) {
	return nil, nil
}
func (m *mockStorage) ListUsers(_ context.Context) ([]*storage.User, error) { return nil, nil }
func (m *mockStorage) CreateTeam(_ context.Context, _ string) (*storage.Team, error) {
	return nil, nil
}
func (m *mockStorage) DeleteTeam(_ context.Context, _ int64) error { return nil }
func (m *mockStorage) ListTeams(_ context.Context) ([]*storage.Team, error) { return nil, nil }
func (m *mockStorage) AddTeamMember(_ context.Context, _ int64, _ string, _ string) error {
	return nil
}
func (m *mockStorage) RemoveTeamMember(_ context.Context, _ int64, _ string) error { return nil }
func (m *mockStorage) UpdateTeamMemberRole(_ context.Context, _ int64, _ string, _ string) error {
	return nil
}
func (m *mockStorage) ListTeamMembers(_ context.Context, _ int64) ([]*storage.TeamMember, error) {
	return nil, nil
}
func (m *mockStorage) ListMyTeams(_ context.Context, _ string) ([]*storage.TeamMember, error) {
	return nil, nil
}
func (m *mockStorage) CreateApplication(_ context.Context, _ int64, _ string) (*storage.Application, error) {
	return nil, nil
}
func (m *mockStorage) DeleteApplication(_ context.Context, _ int64) error { return nil }
func (m *mockStorage) ListApplications(_ context.Context, _ int64) ([]*storage.Application, error) {
	return nil, nil
}
func (m *mockStorage) CleanExpiredSessions(_ context.Context) error { return nil }

// API Key CRUD stubs — required by interface, not exercised by handler tests.
func (m *mockStorage) CreateAPIKey(_ context.Context, _ int64, _, _, _ string, _, _ *int, _, _ *float64, _ []string) (*storage.APIKey, error) {
	return nil, nil
}
func (m *mockStorage) GetAPIKeyByHash(_ context.Context, _ string) (*storage.APIKey, error) {
	return nil, nil
}
func (m *mockStorage) ListAPIKeys(_ context.Context, _ int64) ([]*storage.APIKey, error) {
	return nil, nil
}
func (m *mockStorage) RevokeAPIKey(_ context.Context, _ int64) error { return nil }
func (m *mockStorage) GetKeyAllowedModels(_ context.Context, _ int64) ([]string, error) {
	return nil, nil
}
func (m *mockStorage) UpdateKeyAllowedModels(_ context.Context, _ int64, _ []string) error {
	return nil
}
func (m *mockStorage) UpdateAPIKey(_ context.Context, _ int64, _ string, _ *int, _ *int, _ *float64, _ *float64, _ []string) error {
	return nil
}
func (m *mockStorage) RecordKeySpend(_ context.Context, _ int64, _ float64) error { return nil }
func (m *mockStorage) GetKeySpendTotals(_ context.Context) (map[int64]float64, error) {
	return nil, nil
}
func (m *mockStorage) FlushKeySpend(_ context.Context, _ int64, _ float64) error { return nil }
func (m *mockStorage) GetSpendSummary(_ context.Context, _, _ time.Time, _ storage.SpendFilters) ([]storage.SpendRow, error) {
	return nil, nil
}
func (m *mockStorage) GetModelSpend(_ context.Context, _, _ time.Time, _ storage.SpendFilters) ([]storage.ModelSpendRow, error) {
	return nil, nil
}
func (m *mockStorage) GetDailySpend(_ context.Context, _, _ time.Time, _ storage.SpendFilters) ([]storage.DailySpendRow, error) {
	return nil, nil
}
func (m *mockStorage) GetPoolBudgetState(_ context.Context) ([]storage.PoolBudgetRow, error) {
	return nil, nil
}
func (m *mockStorage) UpsertPoolBudgetState(_ context.Context, _ string, _ float64, _ string) error {
	return nil
}

// newRouterForTest creates a router loaded with the gpt-4 config from configForTest().
func newRouterForTest(t *testing.T) *router.Router {
	t.Helper()
	r, err := router.New(configForTest())
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	return r
}

// withModelParam returns a copy of req with the chi "model" URL param set.
func withModelParam(req *http.Request, model string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("model", model)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// --- ModelDetail tests ---

func TestModelDetail_Found_AutoMapping(t *testing.T) {
	r := newRouterForTest(t)

	// Load a cost map with the canonical "openai/gpt-4" key.
	srv := newCostMapTestServer(http.StatusOK, `{"openai/gpt-4":{"max_tokens":8192,"input_cost_per_token":0.00003,"output_cost_per_token":0.00006,"litellm_provider":"openai","mode":"chat"}}`)
	defer srv.Close()
	cm := loadManager(t, srv)

	handler := ModelDetail(r, cm)
	req := withModelParam(httptest.NewRequest(http.MethodGet, "/v1/models/gpt-4", nil), "gpt-4")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp model.ModelDetailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ID != "gpt-4" {
		t.Errorf("expected ID=gpt-4, got %q", resp.ID)
	}
	if resp.Costs.InputCostPerToken == 0 {
		t.Error("expected non-zero InputCostPerToken")
	}
	if resp.Costs.Source != "auto" {
		t.Errorf("expected Source=auto, got %q", resp.Costs.Source)
	}
	if resp.Costs.CostMapKey != "openai/gpt-4" {
		t.Errorf("expected CostMapKey=openai/gpt-4, got %q", resp.Costs.CostMapKey)
	}
}

func TestModelDetail_Found_NoCostMap(t *testing.T) {
	r := newRouterForTest(t)
	cm := costmap.New() // not loaded

	handler := ModelDetail(r, cm)
	req := withModelParam(httptest.NewRequest(http.MethodGet, "/v1/models/gpt-4", nil), "gpt-4")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp model.ModelDetailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Costs.InputCostPerToken != 0 {
		t.Error("expected zero InputCostPerToken when cost map not loaded")
	}
	if resp.Costs.Source != "" {
		t.Errorf("expected empty Source, got %q", resp.Costs.Source)
	}
}

func TestModelDetail_NotFound(t *testing.T) {
	r := newRouterForTest(t)
	cm := costmap.New()

	handler := ModelDetail(r, cm)
	req := withModelParam(httptest.NewRequest(http.MethodGet, "/v1/models/nonexistent", nil), "nonexistent")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestModelDetail_OverrideKey(t *testing.T) {
	r := newRouterForTest(t)

	srv := newCostMapTestServer(http.StatusOK, `{"my-key":{"max_tokens":4096,"input_cost_per_token":0.00001,"output_cost_per_token":0.00002}}`)
	defer srv.Close()
	cm := loadManager(t, srv)

	// Set an override key so "gpt-4" resolves to "my-key" in the cost map.
	cm.SetOverrideKey("gpt-4", "my-key")

	handler := ModelDetail(r, cm)
	req := withModelParam(httptest.NewRequest(http.MethodGet, "/v1/models/gpt-4", nil), "gpt-4")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp model.ModelDetailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Costs.Source != "override" {
		t.Errorf("expected Source=override, got %q", resp.Costs.Source)
	}
	if resp.Costs.CostMapKey != "my-key" {
		t.Errorf("expected CostMapKey=my-key, got %q", resp.Costs.CostMapKey)
	}
	if resp.Costs.InputCostPerToken == 0 {
		t.Error("expected non-zero InputCostPerToken")
	}
}

func TestModelDetail_CustomSpec(t *testing.T) {
	r := newRouterForTest(t)
	cm := costmap.New()

	custom := costmap.ModelSpec{
		MaxTokens:          16384,
		InputCostPerToken:  0.00005,
		OutputCostPerToken: 0.00015,
		LiteLLMProvider:    "my-provider",
	}
	cm.SetCustomSpec("gpt-4", custom)

	handler := ModelDetail(r, cm)
	req := withModelParam(httptest.NewRequest(http.MethodGet, "/v1/models/gpt-4", nil), "gpt-4")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp model.ModelDetailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Costs.Source != "custom" {
		t.Errorf("expected Source=custom, got %q", resp.Costs.Source)
	}
	if resp.Costs.InputCostPerToken != custom.InputCostPerToken {
		t.Errorf("expected InputCostPerToken=%v, got %v", custom.InputCostPerToken, resp.Costs.InputCostPerToken)
	}
	if resp.Costs.MaxTokens != custom.MaxTokens {
		t.Errorf("expected MaxTokens=%d, got %d", custom.MaxTokens, resp.Costs.MaxTokens)
	}
}

// --- PatchModelMapping tests ---

func TestPatchModelMapping_Valid(t *testing.T) {
	cm := costmap.New()
	store := &mockStorage{}

	handler := PatchModelMapping(cm, store)
	body := `{"cost_map_key":"openai/gpt-4-turbo"}`
	req := withModelParam(httptest.NewRequest(http.MethodPatch, "/v1/models/gpt-4/cost_map_key", bytes.NewBufferString(body)), "gpt-4")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if store.upsertedKeyModel != "gpt-4" {
		t.Errorf("expected storage to receive model=gpt-4, got %q", store.upsertedKeyModel)
	}
	if store.upsertedKey != "openai/gpt-4-turbo" {
		t.Errorf("expected storage to receive key=openai/gpt-4-turbo, got %q", store.upsertedKey)
	}
	// Verify the Manager was also updated.
	result := cm.GetEffectiveSpec("gpt-4", nil)
	if result.Source != "override" {
		t.Errorf("expected Manager source=override after patch, got %q", result.Source)
	}
}

func TestPatchModelMapping_EmptyKey(t *testing.T) {
	handler := PatchModelMapping(costmap.New(), &mockStorage{})
	req := withModelParam(httptest.NewRequest(http.MethodPatch, "/v1/models/gpt-4/cost_map_key", bytes.NewBufferString(`{"cost_map_key":""}`)), "gpt-4")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestPatchModelMapping_MalformedJSON(t *testing.T) {
	handler := PatchModelMapping(costmap.New(), &mockStorage{})
	req := withModelParam(httptest.NewRequest(http.MethodPatch, "/v1/models/gpt-4/cost_map_key", strings.NewReader("not json")), "gpt-4")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// --- PatchModelCosts tests ---

func TestPatchModelCosts_Valid(t *testing.T) {
	cm := costmap.New()
	store := &mockStorage{}

	handler := PatchModelCosts(cm, store)
	body := `{"max_tokens":8192,"input_cost_per_token":0.00001,"output_cost_per_token":0.00002}`
	req := withModelParam(httptest.NewRequest(http.MethodPatch, "/v1/models/gpt-4/costs", bytes.NewBufferString(body)), "gpt-4")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp model.ModelDetailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Costs.Source != "custom" {
		t.Errorf("expected Source=custom, got %q", resp.Costs.Source)
	}
	if resp.Costs.InputCostPerToken != 0.00001 {
		t.Errorf("expected InputCostPerToken=0.00001, got %v", resp.Costs.InputCostPerToken)
	}
	if store.upsertedSpecModel != "gpt-4" {
		t.Errorf("expected storage to receive model=gpt-4, got %q", store.upsertedSpecModel)
	}
}

func TestPatchModelCosts_NovelModel(t *testing.T) {
	// The model "novel-model" does not exist in any router, but PatchModelCosts should
	// still succeed per ADR 002 (supports novel/unmapped models).
	cm := costmap.New()
	store := &mockStorage{}

	handler := PatchModelCosts(cm, store)
	body := `{"input_cost_per_token":0.00001,"output_cost_per_token":0.00003}`
	req := withModelParam(httptest.NewRequest(http.MethodPatch, "/v1/models/novel-model/costs", bytes.NewBufferString(body)), "novel-model")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if store.upsertedSpecModel != "novel-model" {
		t.Errorf("expected storage model=novel-model, got %q", store.upsertedSpecModel)
	}
}

func TestPatchModelCosts_MalformedJSON(t *testing.T) {
	handler := PatchModelCosts(costmap.New(), &mockStorage{})
	req := withModelParam(httptest.NewRequest(http.MethodPatch, "/v1/models/gpt-4/costs", strings.NewReader("not json")), "gpt-4")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// --- Models (list) sanity test ---

func TestModels_ListUnchanged(t *testing.T) {
	r, err := router.New(&config.Config{
		ModelList: []config.ModelConfig{
			{
				ModelName:     "gpt-4",
				LiteLLMParams: config.LiteLLMParams{Model: "openai/gpt-4", APIKey: "key"},
			},
		},
		RouterSettings: config.RouterSettings{
			CooldownTime: 30 * time.Second,
		},
	})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}

	handler := Models(r)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp model.ModelsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Object != "list" {
		t.Errorf("expected object=list, got %q", resp.Object)
	}
	if len(resp.Data) != 1 || resp.Data[0].ID != "gpt-4" {
		t.Errorf("unexpected data: %+v", resp.Data)
	}
}
