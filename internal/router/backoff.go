package router

import (
	"math/rand"
	"sync"
	"time"
)

const (
	backoffBaseDelay = 1 * time.Second
	backoffMaxDelay  = 60 * time.Second
)

// BackoffManager tracks per-deployment 429 rate-limit state using full-jitter
// exponential backoff. It is keyed by deployment string identity (DeploymentKey())
// so state survives config reloads.
//
// BackoffManager is separate from CooldownManager: 429s do not trigger cooldown.
// A deployment can be in backoff, cooldown, or both simultaneously.
type BackoffManager struct {
	mu    sync.Mutex
	state map[string]*backoffState
}

type backoffState struct {
	attempts    int
	nextRetryAt time.Time
}

// NewBackoffManager creates a new BackoffManager.
func NewBackoffManager() *BackoffManager {
	return &BackoffManager{
		state: make(map[string]*backoffState),
	}
}

// InBackoff returns true if the deployment identified by key is currently in
// a rate-limit backoff period.
func (b *BackoffManager) InBackoff(key string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.state[key]
	if !ok {
		return false
	}
	return time.Now().Before(s.nextRetryAt)
}

// ReportRateLimit records a 429 response for the deployment and computes the
// next retry time using full-jitter exponential backoff. If retryAfter > 0
// (from the provider's Retry-After header), the computed backoff is floored
// to at least that duration.
func (b *BackoffManager) ReportRateLimit(key string, retryAfter time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()

	s, ok := b.state[key]
	if !ok {
		s = &backoffState{}
		b.state[key] = s
	}
	s.attempts++

	var delay time.Duration
	if retryAfter > 0 {
		// Server explicitly told us when to retry — honor it exactly.
		delay = retryAfter
	} else {
		// No Retry-After: use full-jitter exponential backoff.
		// sleep = rand(0, min(maxDelay, base * 2^attempts))
		cap := backoffBaseDelay * (1 << s.attempts) // base * 2^attempts
		if cap > backoffMaxDelay || cap <= 0 {
			cap = backoffMaxDelay
		}
		delay = time.Duration(rand.Int63n(int64(cap)))
	}

	s.nextRetryAt = time.Now().Add(delay)
}

// Reset clears the backoff state for the given key. Called on ReportSuccess
// so a deployment that recovers from rate limiting starts fresh.
func (b *BackoffManager) Reset(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.state, key)
}

// getState returns the backoff state for a key (for testing only).
func (b *BackoffManager) getState(key string) backoffState {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s, ok := b.state[key]; ok {
		return *s
	}
	return backoffState{}
}
