package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// mockStorage is a minimal storage.Storage for testing key lookups.
// Only GetAPIKeyByHash and GetKeyAllowedModels are needed by the cache.
type mockStorage struct {
	storage.Storage
	keys   map[string]*storage.APIKey // keyed by SHA-256 hex hash of plaintext token
	models map[int64][]string
}

func (m *mockStorage) GetAPIKeyByHash(_ context.Context, hash string) (*storage.APIKey, error) {
	if k, ok := m.keys[hash]; ok {
		return k, nil
	}
	return nil, nil
}

func (m *mockStorage) GetKeyAllowedModels(_ context.Context, keyID int64) ([]string, error) {
	if models, ok := m.models[keyID]; ok {
		return models, nil
	}
	return []string{}, nil
}

// computeSHA256Hex returns the SHA-256 hex string of the given token.
// Mirrors the hash function used by keystore.Cache so tests can pre-populate mock storage.
func computeSHA256Hex(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// ptr helpers
func intPtr(v int) *int          { return &v }
func float64Ptr(v float64) *float64 { return &v }

// newTestComponents creates test-ready keystore dependencies with the given storage data.
func newTestComponents(
	keysByHash map[string]*storage.APIKey,
	models map[int64][]string,
) (storage.Storage, *keystore.Cache, *keystore.RateLimiter, *keystore.SpendAccumulator) {
	if keysByHash == nil {
		keysByHash = map[string]*storage.APIKey{}
	}
	if models == nil {
		models = map[int64][]string{}
	}
	store := &mockStorage{keys: keysByHash, models: models}
	cache := keystore.New(0) // default 60s TTL
	rl := keystore.NewRateLimiter()
	sa := keystore.NewSpendAccumulator()
	return store, cache, rl, sa
}

// TestKeyAuth_MissingAuthHeader verifies 401 when Authorization header is absent.
func TestKeyAuth_MissingAuthHeader(t *testing.T) {
	store, cache, rl, sa := newTestComponents(nil, nil)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	rr := httptest.NewRecorder()
	KeyAuth("master", store, cache, rl, sa)(handler).ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

// TestKeyAuth_MasterKeyBypass verifies master key passes through without enforcement
// and does NOT inject CachedKey into context.
func TestKeyAuth_MasterKeyBypass(t *testing.T) {
	store, cache, rl, sa := newTestComponents(nil, nil)
	var ctxKeyInjected bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if APIKeyFromContext(r.Context()) != nil {
			ctxKeyInjected = true
		}
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer master")
	rr := httptest.NewRecorder()
	KeyAuth("master", store, cache, rl, sa)(handler).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if ctxKeyInjected {
		t.Error("master key must not inject CachedKey into context")
	}
}

// TestKeyAuth_InvalidKey verifies 401 for an unknown token.
func TestKeyAuth_InvalidKey(t *testing.T) {
	store, cache, rl, sa := newTestComponents(nil, nil)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer sk-app-unknown")
	rr := httptest.NewRecorder()
	KeyAuth("master", store, cache, rl, sa)(handler).ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

// TestKeyAuth_RevokedKey verifies 401 for a key with is_active=false.
func TestKeyAuth_RevokedKey(t *testing.T) {
	revokedKey := &storage.APIKey{ID: 1, IsActive: false, KeyPrefix: "sk-app"}
	hash := computeSHA256Hex("sk-app-revoked")
	keys := map[string]*storage.APIKey{hash: revokedKey}

	store, cache, rl, sa := newTestComponents(keys, nil)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer sk-app-revoked")
	rr := httptest.NewRecorder()
	KeyAuth("master", store, cache, rl, sa)(handler).ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for revoked key, got %d", rr.Code)
	}
}

// TestKeyAuth_ValidKey verifies 200 and CachedKey injection for a valid active key.
func TestKeyAuth_ValidKey(t *testing.T) {
	validKey := &storage.APIKey{ID: 2, IsActive: true, KeyPrefix: "sk-app"}
	hash := computeSHA256Hex("sk-app-valid")
	keys := map[string]*storage.APIKey{hash: validKey}

	store, cache, rl, sa := newTestComponents(keys, nil)
	var injected *keystore.CachedKey
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		injected = APIKeyFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer sk-app-valid")
	rr := httptest.NewRecorder()
	KeyAuth("master", store, cache, rl, sa)(handler).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if injected == nil {
		t.Error("expected CachedKey injected into context, got nil")
	}
}

// TestKeyAuth_RPMExceeded verifies 429 with rate_limit_error type when RPM limit is hit.
func TestKeyAuth_RPMExceeded(t *testing.T) {
	maxRPM := 1
	rpmKey := &storage.APIKey{ID: 3, IsActive: true, MaxRPM: intPtr(maxRPM)}
	hash := computeSHA256Hex("sk-app-rpm")
	keys := map[string]*storage.APIKey{hash: rpmKey}

	store, cache, rl, sa := newTestComponents(keys, nil)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// First request: should pass (counter goes to 1 == maxRPM)
	req1 := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req1.Header.Set("Authorization", "Bearer sk-app-rpm")
	rr1 := httptest.NewRecorder()
	KeyAuth("master", store, cache, rl, sa)(handler).ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Errorf("first request: expected 200, got %d", rr1.Code)
	}

	// Second request in same minute window: counter goes to 2 > maxRPM=1, should be rejected
	req2 := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req2.Header.Set("Authorization", "Bearer sk-app-rpm")
	rr2 := httptest.NewRecorder()
	KeyAuth("master", store, cache, rl, sa)(handler).ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Errorf("second request: expected 429, got %d", rr2.Code)
	}
	var apiErr struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rr2.Body).Decode(&apiErr); err == nil {
		if apiErr.Error.Type != "rate_limit_error" {
			t.Errorf("expected type rate_limit_error, got %s", apiErr.Error.Type)
		}
	}
}

// TestKeyAuth_HardBudgetExceeded verifies 429 with budget_limit_error type when hard budget exceeded.
func TestKeyAuth_HardBudgetExceeded(t *testing.T) {
	budget := 1.0
	budgetKey := &storage.APIKey{ID: 4, IsActive: true, MaxBudget: float64Ptr(budget)}
	hash := computeSHA256Hex("sk-app-budget")
	keys := map[string]*storage.APIKey{hash: budgetKey}

	store, cache, rl, sa := newTestComponents(keys, nil)
	// Pre-credit the accumulator beyond budget
	sa.Credit(4, 2.0)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer sk-app-budget")
	rr := httptest.NewRecorder()
	KeyAuth("master", store, cache, rl, sa)(handler).ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 for budget exceeded, got %d", rr.Code)
	}
	var apiErr struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&apiErr); err == nil {
		if apiErr.Error.Type != "budget_limit_error" {
			t.Errorf("expected type budget_limit_error, got %s", apiErr.Error.Type)
		}
	}
}

// TestKeyAuth_APIKeyFromContext_NilWhenAbsent verifies APIKeyFromContext returns nil
// when no key was injected (e.g., on master key path or empty context).
func TestKeyAuth_APIKeyFromContext_NilWhenAbsent(t *testing.T) {
	ck := APIKeyFromContext(context.Background())
	if ck != nil {
		t.Error("expected nil from empty context, got non-nil")
	}
}
