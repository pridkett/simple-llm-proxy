package router

import (
	"context"
	"sync"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

const (
	stickySessionTTL      = 1 * time.Hour
	stickyFlushInterval   = 30 * time.Second
	stickyCleanupInterval = 20 * time.Minute
)

// stickyEntry is the in-memory representation of a sticky session mapping.
type stickyEntry struct {
	deploymentKey string
	lastUsedAt    time.Time
	dirty         bool
}

// StickySessionManager provides in-memory sticky session cache with periodic
// flush to storage and background cleanup of expired entries. Follows the
// SpendAccumulator pattern: in-memory is the hot-path, storage is the durable
// backing store flushed periodically.
type StickySessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*stickyEntry // key: "sessionKey:poolName"
	store    storage.Storage         // nil = no persistence (tests)
	stopCh   chan struct{}
}

// NewStickySessionManager creates a new manager. If store is nil, persistence
// is disabled (useful for tests and when storage is wired later).
func NewStickySessionManager(store storage.Storage) *StickySessionManager {
	return &StickySessionManager{
		sessions: make(map[string]*stickyEntry),
		store:    store,
	}
}

// Start launches background flush and cleanup goroutines. The goroutines
// respect context cancellation and the Stop() signal.
func (sm *StickySessionManager) Start(ctx context.Context) {
	sm.stopCh = make(chan struct{})

	go sm.flushLoop(ctx)
	go sm.cleanupLoop(ctx)
}

// Stop signals background goroutines to stop and performs a final flush.
func (sm *StickySessionManager) Stop() {
	if sm.stopCh != nil {
		close(sm.stopCh)
	}
	sm.flush()
}

// Get returns the deployment key for the given session and pool.
// Returns "" if the session does not exist or has expired.
// On a hit, lastUsedAt is refreshed (lazy TTL renewal).
func (sm *StickySessionManager) Get(sessionKey, poolName string) string {
	cacheKey := sessionKey + ":" + poolName

	sm.mu.RLock()
	entry, ok := sm.sessions[cacheKey]
	sm.mu.RUnlock()

	if !ok {
		return ""
	}

	// Check TTL expiry.
	if time.Since(entry.lastUsedAt) > stickySessionTTL {
		// Expired — remove from cache.
		sm.mu.Lock()
		delete(sm.sessions, cacheKey)
		sm.mu.Unlock()
		return ""
	}

	// Lazy refresh: update lastUsedAt on read hit.
	sm.mu.Lock()
	entry.lastUsedAt = time.Now()
	entry.dirty = true
	sm.mu.Unlock()

	return entry.deploymentKey
}

// Set stores or updates a sticky session mapping.
func (sm *StickySessionManager) Set(sessionKey, poolName, deploymentKey string) {
	cacheKey := sessionKey + ":" + poolName

	sm.mu.Lock()
	sm.sessions[cacheKey] = &stickyEntry{
		deploymentKey: deploymentKey,
		lastUsedAt:    time.Now(),
		dirty:         true,
	}
	sm.mu.Unlock()
}

// flushLoop runs periodically to persist dirty entries to storage.
func (sm *StickySessionManager) flushLoop(ctx context.Context) {
	ticker := time.NewTicker(stickyFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sm.stopCh:
			return
		case <-ticker.C:
			sm.flush()
		}
	}
}

// cleanupLoop runs periodically to evict expired entries from cache and storage.
func (sm *StickySessionManager) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(stickyCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sm.stopCh:
			return
		case <-ticker.C:
			sm.cleanup()
		}
	}
}

// flush collects dirty entries and writes them to storage in a single batch.
func (sm *StickySessionManager) flush() {
	if sm.store == nil {
		return
	}

	sm.mu.Lock()
	var dirty []storage.StickySession
	for key, entry := range sm.sessions {
		if !entry.dirty {
			continue
		}
		sessionKey, poolName := splitCacheKey(key)
		dirty = append(dirty, storage.StickySession{
			SessionKey:    sessionKey,
			PoolName:      poolName,
			DeploymentKey: entry.deploymentKey,
			LastUsedAt:    entry.lastUsedAt,
		})
		entry.dirty = false
	}
	sm.mu.Unlock()

	if len(dirty) == 0 {
		return
	}

	// Best-effort: log errors but don't crash.
	_ = sm.store.BulkUpsertStickySessions(context.Background(), dirty)
}

// cleanup evicts expired entries from the in-memory cache and storage.
func (sm *StickySessionManager) cleanup() {
	now := time.Now()
	cutoff := now.Add(-stickySessionTTL)

	// Remove expired entries from in-memory cache.
	sm.mu.Lock()
	for key, entry := range sm.sessions {
		if entry.lastUsedAt.Before(cutoff) {
			delete(sm.sessions, key)
		}
	}
	sm.mu.Unlock()

	// Remove expired entries from storage.
	if sm.store != nil {
		_, _ = sm.store.DeleteExpiredStickySessions(context.Background(), cutoff)
	}
}

// splitCacheKey reverses the "sessionKey:poolName" concatenation.
// The session key may itself contain colons (it's a SHA-256 hex hash, but
// we use the first colon from the right as the delimiter would be fragile).
// Instead, we search for the last colon since pool names are simple identifiers.
func splitCacheKey(cacheKey string) (sessionKey, poolName string) {
	for i := len(cacheKey) - 1; i >= 0; i-- {
		if cacheKey[i] == ':' {
			return cacheKey[:i], cacheKey[i+1:]
		}
	}
	return cacheKey, ""
}
