package costmap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

const testJSON = `{
	"gpt-4": {
		"max_tokens": 8192,
		"max_input_tokens": 8192,
		"max_output_tokens": 8192,
		"input_cost_per_token": 0.00003,
		"output_cost_per_token": 0.00006,
		"litellm_provider": "openai",
		"mode": "chat",
		"supports_function_calling": true
	},
	"claude-3-opus": {
		"max_tokens": 4096,
		"max_input_tokens": 200000,
		"max_output_tokens": 4096,
		"input_cost_per_token": 0.000015,
		"output_cost_per_token": 0.000075,
		"litellm_provider": "anthropic",
		"mode": "chat",
		"supports_function_calling": true
	}
}`

func newTestServer(statusCode int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		w.Write([]byte(body)) //nolint:errcheck
	}))
}

func TestNew_Defaults(t *testing.T) {
	m := New()
	if m.sourceURL != DefaultURL {
		t.Errorf("expected DefaultURL, got %q", m.sourceURL)
	}
	if m.models != nil {
		t.Error("expected nil models before first load")
	}
	if m.loadedAt != nil {
		t.Error("expected nil loadedAt before first load")
	}
}

func TestLoad_Success(t *testing.T) {
	srv := newTestServer(http.StatusOK, testJSON)
	defer srv.Close()

	m := New()
	m.sourceURL = srv.URL

	if err := m.Load(context.Background()); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	s := m.Status()
	if !s.Loaded {
		t.Error("expected Loaded=true after successful load")
	}
	if s.LoadedAt == nil {
		t.Error("expected non-nil LoadedAt after successful load")
	}
	if s.ModelCount != 2 {
		t.Errorf("expected ModelCount=2, got %d", s.ModelCount)
	}
}

func TestLoad_HTTPError(t *testing.T) {
	srv := newTestServer(http.StatusInternalServerError, "error")
	defer srv.Close()

	m := New()
	m.sourceURL = srv.URL

	if err := m.Load(context.Background()); err == nil {
		t.Fatal("expected error on HTTP 500")
	}

	s := m.Status()
	if s.Loaded {
		t.Error("state should be unchanged after failed load")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	srv := newTestServer(http.StatusOK, "not valid json {{{")
	defer srv.Close()

	m := New()
	m.sourceURL = srv.URL

	if err := m.Load(context.Background()); err == nil {
		t.Fatal("expected error on invalid JSON")
	}

	if m.Status().Loaded {
		t.Error("state should be unchanged after failed parse")
	}
}

func TestLoad_EmptyJSON(t *testing.T) {
	srv := newTestServer(http.StatusOK, "{}")
	defer srv.Close()

	m := New()
	m.sourceURL = srv.URL

	if err := m.Load(context.Background()); err != nil {
		t.Fatalf("unexpected error on empty JSON: %v", err)
	}

	s := m.Status()
	if !s.Loaded {
		t.Error("expected Loaded=true even for empty map")
	}
	if s.ModelCount != 0 {
		t.Errorf("expected ModelCount=0, got %d", s.ModelCount)
	}
}

func TestLoad_NetworkError(t *testing.T) {
	m := New()
	m.sourceURL = "http://127.0.0.1:1" // nothing listening

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := m.Load(ctx); err == nil {
		t.Fatal("expected network error")
	}
}

func TestReload_IsAliasForLoad(t *testing.T) {
	srv := newTestServer(http.StatusOK, testJSON)
	defer srv.Close()

	m := New()
	m.sourceURL = srv.URL

	if err := m.Reload(context.Background()); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	if !m.Status().Loaded {
		t.Error("expected Loaded=true after Reload")
	}
}

func TestSetURL_Valid(t *testing.T) {
	m := New()
	newURL := "https://example.com/models.json"
	if err := m.SetURL(newURL); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.GetURL() != newURL {
		t.Errorf("expected %q, got %q", newURL, m.GetURL())
	}
}

func TestSetURL_ValidHTTP(t *testing.T) {
	m := New()
	if err := m.SetURL("http://internal.host/models.json"); err != nil {
		t.Fatalf("unexpected error for http scheme: %v", err)
	}
}

func TestSetURL_Empty(t *testing.T) {
	m := New()
	if err := m.SetURL(""); err == nil {
		t.Fatal("expected error for empty URL")
	}
	if m.GetURL() != DefaultURL {
		t.Error("URL should be unchanged after error")
	}
}

func TestSetURL_InvalidScheme(t *testing.T) {
	m := New()
	if err := m.SetURL("ftp://example.com/models.json"); err == nil {
		t.Fatal("expected error for ftp scheme")
	}
	if m.GetURL() != DefaultURL {
		t.Error("URL should be unchanged after error")
	}
}

func TestGetModel_NotLoaded(t *testing.T) {
	m := New()
	_, ok := m.GetModel("gpt-4")
	if ok {
		t.Error("expected false before first load")
	}
}

func TestGetModel_Found(t *testing.T) {
	srv := newTestServer(http.StatusOK, testJSON)
	defer srv.Close()

	m := New()
	m.sourceURL = srv.URL
	m.Load(context.Background()) //nolint:errcheck

	spec, ok := m.GetModel("gpt-4")
	if !ok {
		t.Fatal("expected gpt-4 to be found")
	}
	if spec.LiteLLMProvider != "openai" {
		t.Errorf("expected provider openai, got %q", spec.LiteLLMProvider)
	}
	if spec.InputCostPerToken != 0.00003 {
		t.Errorf("unexpected input cost: %v", spec.InputCostPerToken)
	}
}

func TestGetModel_NotFound(t *testing.T) {
	srv := newTestServer(http.StatusOK, testJSON)
	defer srv.Close()

	m := New()
	m.sourceURL = srv.URL
	m.Load(context.Background()) //nolint:errcheck

	_, ok := m.GetModel("nonexistent-model")
	if ok {
		t.Error("expected false for unknown model")
	}
}

func TestStatus_BeforeLoad(t *testing.T) {
	m := New()
	s := m.Status()
	if s.Loaded {
		t.Error("expected Loaded=false before load")
	}
	if s.LoadedAt != nil {
		t.Error("expected nil LoadedAt before load")
	}
	if s.URL != DefaultURL {
		t.Errorf("expected DefaultURL, got %q", s.URL)
	}
	if s.ModelCount != 0 {
		t.Errorf("expected ModelCount=0, got %d", s.ModelCount)
	}
}

func TestStatus_AfterLoad(t *testing.T) {
	srv := newTestServer(http.StatusOK, testJSON)
	defer srv.Close()

	m := New()
	m.sourceURL = srv.URL
	before := time.Now()
	m.Load(context.Background()) //nolint:errcheck

	s := m.Status()
	if !s.Loaded {
		t.Error("expected Loaded=true")
	}
	if s.LoadedAt == nil || s.LoadedAt.Before(before) {
		t.Error("LoadedAt should be set to a time after test start")
	}
	if s.ModelCount != 2 {
		t.Errorf("expected ModelCount=2, got %d", s.ModelCount)
	}
}

func TestLoad_Concurrency(t *testing.T) {
	srv := newTestServer(http.StatusOK, testJSON)
	defer srv.Close()

	m := New()
	m.sourceURL = srv.URL

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Load(context.Background()) //nolint:errcheck
		}()
	}
	wg.Wait()

	if !m.Status().Loaded {
		t.Error("expected Loaded=true after concurrent loads")
	}
}
