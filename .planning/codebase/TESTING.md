# Testing Patterns

**Analysis Date:** 2026-03-25

## Test Framework

**Runner:**
- Go's built-in `testing` package (Go 1.25.4)
- Config: No separate test config file; uses `go test` command directly
- Run commands via Makefile: `make test`, `make test-coverage`

**Assertion Library:**
- None used; manual assertions with `if condition { t.Errorf(...) }`
- Error reporting: `t.Fatalf()` for setup/critical failures, `t.Errorf()` for assertion failures

**Run Commands:**
```bash
make test              # Run all tests with verbose output (go test -v ./...)
make test-coverage     # Generate coverage report and HTML (go test -v -coverprofile=coverage.out ./...; go tool cover -html=coverage.out)
go test ./...          # Run tests from any directory
go test -v ./...       # Verbose test output
go test -run TestName  # Run specific test
```

## Test File Organization

**Location:**
- Co-located with source code (same package)
- Test files in same directory as implementation
- Example: `internal/api/handler/health.go` and `internal/api/handler/health_test.go`

**Naming:**
- Pattern: `{source_file}_test.go`
- Example test files:
  - `internal/api/handler/health_test.go`
  - `internal/api/handler/admin_test.go`
  - `internal/config/loader_test.go`
  - `internal/router/router_test.go`
  - `internal/costmap/costmap_test.go`
  - `internal/api/middleware/auth_test.go`

**Structure:**
```
internal/
  api/
    handler/
      health.go
      health_test.go
      admin.go
      admin_test.go
      chat.go          # no test file yet
      models.go
      models_test.go
    middleware/
      auth.go
      auth_test.go
      logging.go       # no test file yet
```

## Test Structure

**Suite Organization:**
```go
// Single test function (simple cases)
func TestHealth(t *testing.T) {
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()

	Health()(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var resp HealthResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got '%s'", resp.Status)
	}
}

// Table-driven test (multiple cases)
func TestAuth(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name       string
		masterKey  string
		authHeader string
		wantStatus int
	}{
		{
			name:       "no master key configured",
			masterKey:  "",
			authHeader: "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "valid bearer token",
			masterKey:  "secret",
			authHeader: "Bearer secret",
			wantStatus: http.StatusOK,
		},
		// ... more test cases
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			rr := httptest.NewRecorder()
			Auth(tt.masterKey)(handler).ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, rr.Code)
			}
		})
	}
}
```

**Patterns:**
- Setup: Create test data, mock dependencies, initialize test server/recorder
- Execution: Call function under test with test inputs
- Assertion: Check return values, status codes, response body content
- Cleanup: Explicit defers for resource cleanup (`defer srv.Close()`, `defer wg.Done()`)

## Mocking

**Framework:** Manual mock types defined in test files

**Patterns:**
```go
// Mock provider in router_test.go
type mockProvider struct{ name string }

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) ChatCompletion(_ context.Context, _ *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	return nil, nil
}
func (m *mockProvider) ChatCompletionStream(_ context.Context, _ *model.ChatCompletionRequest) (provider.Stream, error) {
	return nil, nil
}
func (m *mockProvider) Embeddings(_ context.Context, _ *model.EmbeddingsRequest) (*model.EmbeddingsResponse, error) {
	return nil, nil
}
func (m *mockProvider) SupportsEmbeddings() bool { return false }

func init() {
	provider.Register("mock", func(apiKey, apiBase string) provider.Provider {
		return &mockProvider{name: "mock"}
	})
}

// Usage in test
func TestShuffleStrategy(t *testing.T) {
	deployments := []*provider.Deployment{
		{ModelName: "model1"},
		{ModelName: "model2"},
	}
	// ... test code
}
```

**Mock HTTP Servers:**
```go
func newTestServer(statusCode int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		w.Write([]byte(body)) //nolint:errcheck
	}))
}

// Usage
func TestLoad_HTTPError(t *testing.T) {
	srv := newTestServer(http.StatusInternalServerError, "error")
	defer srv.Close()

	m := New()
	m.sourceURL = srv.URL

	if err := m.Load(context.Background()); err == nil {
		t.Fatal("expected error on HTTP 500")
	}
}
```

**What to Mock:**
- External HTTP services (use `httptest.NewServer`)
- Provider implementations (custom mock struct)
- Database calls (pass `nil` for storage when not testing storage)
- Time-based operations (directly test with real `time.Sleep` for cooldown tests)

**What NOT to Mock:**
- Core business logic: let router and provider logic run
- Config parsing and validation: test with real YAML strings
- Storage operations: test SQLite operations directly when testing storage
- Type conversions and marshaling: test with real JSON encoding/decoding

## Fixtures and Factories

**Test Data:**
```go
// Config factory in router_test.go
func makeMockConfig(models []string, strategy string) *config.Config {
	mc := make([]config.ModelConfig, 0, len(models))
	for _, name := range models {
		mc = append(mc, config.ModelConfig{
			ModelName: name,
			LiteLLMParams: config.LiteLLMParams{
				Model:  "mock/" + name,
				APIKey: "test-key",
			},
		})
	}
	return &config.Config{
		ModelList: mc,
		RouterSettings: config.RouterSettings{
			RoutingStrategy: strategy,
			NumRetries:      2,
			AllowedFails:    3,
			CooldownTime:    30 * time.Second,
		},
	}
}

// Handler test config factory in admin_test.go
func configForTest() *config.Config {
	return &config.Config{
		ModelList: []config.ModelConfig{
			{
				ModelName: "gpt-4",
				LiteLLMParams: config.LiteLLMParams{
					Model:  "openai/gpt-4",
					APIKey: "test-key",
				},
				RPM: 100,
			},
		},
		RouterSettings: config.RouterSettings{
			RoutingStrategy: "simple-shuffle",
			NumRetries:      2,
			AllowedFails:    3,
			CooldownTime:    30 * time.Second,
		},
		GeneralSettings: config.GeneralSettings{
			MasterKey:   "master-key",
			DatabaseURL: "./test.db",
			Port:        8080,
		},
	}
}

// Static test JSON in costmap_test.go
const testJSON = `{
	"gpt-4": {
		"max_tokens": 8192,
		"input_cost_per_token": 0.00003,
		"output_cost_per_token": 0.00006,
		"litellm_provider": "openai",
		"mode": "chat"
	}
}`
```

**Location:**
- Fixtures defined in same test file at top-level (constants, functions)
- Factory functions: `makeMockConfig()`, `configForTest()`, `newTestServer()`
- Test JSON constants: `testJSON`, `initialYAML`, `updatedYAML` defined inline

## Coverage

**Requirements:** None enforced (no `coverage.out` in `.gitignore`)

**View Coverage:**
```bash
make test-coverage    # Generates coverage.out and opens coverage.html in browser
```

Coverage targets: Not specified in tooling; no minimum enforced. Coverage.out and coverage.html generated but not committed.

## Test Types

**Unit Tests:**
- Scope: Individual functions and methods
- Approach: Mock external dependencies (HTTP, storage, providers)
- Examples:
  - `TestHealth()` - tests handler with httptest.Recorder
  - `TestAuth()` - tests middleware with mock master key
  - `TestParse()` - tests YAML parsing with string input
  - `TestShuffleStrategy()` - tests strategy selection logic

**Integration Tests:**
- Scope: Multiple components working together
- Approach: Real implementations, test full flow
- Examples:
  - `TestRouterReload_UpdatesDeployments()` - tests router reloading config changes
  - `TestAdminReload_Success()` - tests config reload via handler with real YAML file
  - `TestLoad_Concurrency()` - tests concurrent access to costmap with goroutines
  - `TestCooldownManager()` - tests failure tracking and cooldown state

**E2E Tests:**
- Framework: Not implemented
- Rationale: Integration tests cover main user flows; E2E would require full server startup and external service calls

## Common Patterns

**Async Testing:**
```go
// WaitGroup pattern in costmap_test.go
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

// Time-based testing in router_test.go (cooldown)
func TestCooldownManager(t *testing.T) {
	cm := NewCooldownManager(100*time.Millisecond, 2)

	d := &provider.Deployment{ModelName: "test"}

	cm.ReportFailure(d)
	if cm.InCooldown(d) {
		t.Error("Expected not in cooldown after 1 failure")
	}

	cm.ReportFailure(d)
	if !cm.InCooldown(d) {
		t.Error("Expected in cooldown after 2 failures")
	}

	// Wait for cooldown to expire
	time.Sleep(150 * time.Millisecond)
	if cm.InCooldown(d) {
		t.Error("Expected cooldown to expire")
	}
}
```

**Error Testing:**
```go
// Testing error conditions in config_test.go
func TestParseEnvExpansion(t *testing.T) {
	os.Setenv("TEST_API_KEY", "secret-from-env")
	os.Setenv("TEST_MASTER_KEY", "master-from-env")
	defer os.Unsetenv("TEST_API_KEY")
	defer os.Unsetenv("TEST_MASTER_KEY")

	yaml := `
model_list:
  - model_name: test-model
    litellm_params:
      model: openai/gpt-4
      api_key: os.environ/TEST_API_KEY
general_settings:
  master_key: os.environ/TEST_MASTER_KEY
`

	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if cfg.ModelList[0].LiteLLMParams.APIKey != "secret-from-env" {
		t.Errorf("Expected expanded API key 'secret-from-env', got '%s'", cfg.ModelList[0].LiteLLMParams.APIKey)
	}
}

// Testing invalid HTTP responses in costmap_test.go
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
```

**Response Decoding:**
```go
// JSON response validation in health_test.go
var resp HealthResponse
if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
	t.Fatalf("Failed to decode response: %v", err)
}

if resp.Status != "healthy" {
	t.Errorf("Expected status 'healthy', got '%s'", resp.Status)
}

// String content verification in admin_test.go
raw := rr.Body.String()
if contains(raw, "master-key") {
	t.Error("Master key must not be returned in config response")
}
```

## Test File Inventory

**Tested Modules:**
- `internal/api/handler/health_test.go` - Health check endpoint
- `internal/api/handler/admin_test.go` - Admin endpoints (status, config, reload)
- `internal/api/handler/models_test.go` - Model listing endpoint
- `internal/api/handler/costmap_test.go` - Cost endpoint
- `internal/api/middleware/auth_test.go` - Authentication middleware
- `internal/config/loader_test.go` - Config parsing and env expansion
- `internal/config/reloader_test.go` - Config reload via file watch
- `internal/router/router_test.go` - Router, strategies, cooldown
- `internal/costmap/costmap_test.go` - Cost model loading and caching
- `internal/openapi/openapi_test.go` - OpenAPI spec generation

**Untested Modules:**
- `internal/api/handler/chat.go` - Chat completion handler (integration flow complex; tested via integration)
- `internal/api/handler/embeddings.go` - Embeddings handler
- `internal/api/handler/completions.go` - Completions handler
- `internal/api/middleware/logging.go` - Logging middleware (manual verification via Makefile run)
- `internal/api/middleware/recovery.go` - Recovery middleware
- `internal/api/middleware/cors.go` - CORS middleware
- `internal/provider/openai/openai.go` - OpenAI provider (requires API key; tested via integration)
- `internal/provider/anthropic/anthropic.go` - Anthropic provider (requires API key; tested via integration)
- `internal/storage/sqlite/sqlite.go` - SQLite operations (storage implementation)
- `cmd/proxy/main.go` - Server startup (tested via make run)

---

*Testing analysis: 2026-03-25*
