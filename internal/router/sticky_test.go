package router

import (
	"testing"
	"time"
)

func TestStickySession_SetAndGet(t *testing.T) {
	sm := NewStickySessionManager(nil)

	sm.Set("key-abc", "pool-1", "openai:gpt-4:")
	got := sm.Get("key-abc", "pool-1")
	if got != "openai:gpt-4:" {
		t.Errorf("expected 'openai:gpt-4:', got %q", got)
	}
}

func TestStickySession_DifferentPools(t *testing.T) {
	sm := NewStickySessionManager(nil)

	sm.Set("key-abc", "pool-alpha", "openai:gpt-4:")
	sm.Set("key-abc", "pool-beta", "anthropic:claude-3:")

	gotAlpha := sm.Get("key-abc", "pool-alpha")
	gotBeta := sm.Get("key-abc", "pool-beta")

	if gotAlpha != "openai:gpt-4:" {
		t.Errorf("pool-alpha: expected 'openai:gpt-4:', got %q", gotAlpha)
	}
	if gotBeta != "anthropic:claude-3:" {
		t.Errorf("pool-beta: expected 'anthropic:claude-3:', got %q", gotBeta)
	}
	if gotAlpha == gotBeta {
		t.Error("expected different deployment keys for different pools")
	}
}

func TestStickySession_TTLExpiry(t *testing.T) {
	sm := NewStickySessionManager(nil)

	// Manually inject an entry with a lastUsedAt in the past (beyond TTL).
	cacheKey := "key-expired:pool-1"
	sm.mu.Lock()
	sm.sessions[cacheKey] = &stickyEntry{
		deploymentKey: "openai:gpt-4:",
		lastUsedAt:    time.Now().Add(-2 * stickySessionTTL), // 2 hours ago
		dirty:         false,
	}
	sm.mu.Unlock()

	got := sm.Get("key-expired", "pool-1")
	if got != "" {
		t.Errorf("expected empty string for expired session, got %q", got)
	}

	// Verify the expired entry was evicted from cache.
	sm.mu.RLock()
	_, exists := sm.sessions[cacheKey]
	sm.mu.RUnlock()
	if exists {
		t.Error("expected expired entry to be removed from cache")
	}
}

func TestStickySession_UpdateOnAccess(t *testing.T) {
	sm := NewStickySessionManager(nil)

	// Set with a known past time.
	cacheKey := "key-refresh:pool-1"
	pastTime := time.Now().Add(-30 * time.Minute)
	sm.mu.Lock()
	sm.sessions[cacheKey] = &stickyEntry{
		deploymentKey: "openai:gpt-4:",
		lastUsedAt:    pastTime,
		dirty:         false,
	}
	sm.mu.Unlock()

	// Get should refresh lastUsedAt.
	got := sm.Get("key-refresh", "pool-1")
	if got != "openai:gpt-4:" {
		t.Fatalf("expected 'openai:gpt-4:', got %q", got)
	}

	sm.mu.RLock()
	entry := sm.sessions[cacheKey]
	sm.mu.RUnlock()

	if !entry.lastUsedAt.After(pastTime) {
		t.Error("expected lastUsedAt to be refreshed after Get")
	}
	if !entry.dirty {
		t.Error("expected entry to be marked dirty after Get refresh")
	}
}

func TestStickySession_Overwrite(t *testing.T) {
	sm := NewStickySessionManager(nil)

	sm.Set("key-abc", "pool-1", "deployment-A")
	sm.Set("key-abc", "pool-1", "deployment-B")

	got := sm.Get("key-abc", "pool-1")
	if got != "deployment-B" {
		t.Errorf("expected 'deployment-B' after overwrite, got %q", got)
	}
}

func TestStickySession_NilStore(t *testing.T) {
	sm := NewStickySessionManager(nil)

	// Set and Get should work without storage.
	sm.Set("key-abc", "pool-1", "openai:gpt-4:")
	got := sm.Get("key-abc", "pool-1")
	if got != "openai:gpt-4:" {
		t.Errorf("expected 'openai:gpt-4:', got %q", got)
	}

	// Flush with nil store should be a no-op (no panic).
	sm.flush()

	// Cleanup with nil store should be a no-op (no panic).
	sm.cleanup()
}

func TestSplitCacheKey(t *testing.T) {
	tests := []struct {
		input      string
		wantKey    string
		wantPool   string
	}{
		{"abc:pool-1", "abc", "pool-1"},
		{"abc123def:gpt-4-pool", "abc123def", "gpt-4-pool"},
		{"a:b:pool-name", "a:b", "pool-name"},
		{"nocolon", "nocolon", ""},
	}

	for _, tt := range tests {
		gotKey, gotPool := splitCacheKey(tt.input)
		if gotKey != tt.wantKey || gotPool != tt.wantPool {
			t.Errorf("splitCacheKey(%q) = (%q, %q), want (%q, %q)",
				tt.input, gotKey, gotPool, tt.wantKey, tt.wantPool)
		}
	}
}
