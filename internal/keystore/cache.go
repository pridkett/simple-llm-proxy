package keystore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

const defaultCacheTTL = 60 * time.Second

// CachedKey is the value stored in the key cache.
type CachedKey struct {
	Key           *storage.APIKey
	AllowedModels []string // empty slice = all models allowed
}

type cacheEntry struct {
	value     CachedKey
	expiresAt time.Time
	// keyID is stored separately for Invalidate() scans
	keyID int64
}

// Cache is an in-memory TTL cache for API key lookups.
// Thread-safe via sync.Map. Designed so the storage backend can be swapped
// (e.g., Redis) without changing the calling interface.
type Cache struct {
	ttl   time.Duration
	store sync.Map // map[string]*cacheEntry  (key = SHA-256 hex of token)
}

// New creates a new Cache. If ttl is 0, uses the 60-second default.
func New(ttl time.Duration) *Cache {
	if ttl == 0 {
		ttl = defaultCacheTTL
	}
	return &Cache{ttl: ttl}
}

// Get returns the CachedKey for the given plaintext token.
// On cache miss or expiry, fetches from storage and caches the result.
// Returns (nil, nil) if the key does not exist in storage.
func (c *Cache) Get(ctx context.Context, token string, store storage.Storage) (*CachedKey, error) {
	hash := hashKey(token)

	if v, ok := c.store.Load(hash); ok {
		entry := v.(*cacheEntry)
		if time.Now().Before(entry.expiresAt) {
			ck := entry.value
			return &ck, nil
		}
		// Expired — delete and fall through to storage
		c.store.Delete(hash)
	}

	key, err := store.GetAPIKeyByHash(ctx, hash)
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, nil
	}

	models, err := store.GetKeyAllowedModels(ctx, key.ID)
	if err != nil {
		return nil, err
	}

	ck := CachedKey{Key: key, AllowedModels: models}
	c.store.Store(hash, &cacheEntry{
		value:     ck,
		expiresAt: time.Now().Add(c.ttl),
		keyID:     key.ID,
	})
	return &ck, nil
}

// Invalidate removes all cache entries for the given key ID.
// Called immediately when a key is revoked — does not wait for TTL expiry.
func (c *Cache) Invalidate(keyID int64) {
	c.store.Range(func(k, v any) bool {
		if v.(*cacheEntry).keyID == keyID {
			c.store.Delete(k)
		}
		return true
	})
}

// hashKey returns the SHA-256 hex of a plaintext API key token.
func hashKey(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
