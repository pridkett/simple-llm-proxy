package keystore

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// RateLimiter tracks per-key request counts using in-memory atomic counters.
// Window keys are time-bucketed so counters naturally become stale as time advances.
// Counter resets on server restart — acceptable for single-instance deployment.
type RateLimiter struct {
	counts sync.Map // map[string]*int64
}

// NewRateLimiter creates a new RateLimiter.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{}
}

// CheckAndIncrementRPM increments the RPM counter for the key's current minute window.
// Returns true if the request is allowed (count after increment <= maxRPM).
// If maxRPM <= 0, always returns true (no limit).
func (rl *RateLimiter) CheckAndIncrementRPM(keyID int64, maxRPM int) bool {
	if maxRPM <= 0 {
		return true
	}
	key := rpmKey(keyID)
	n := rl.increment(key)
	return n <= int64(maxRPM)
}

// CheckAndIncrementRPD increments the RPD counter for the key's current day window.
// Returns true if the request is allowed (count after increment <= maxRPD).
// If maxRPD <= 0, always returns true (no limit).
func (rl *RateLimiter) CheckAndIncrementRPD(keyID int64, maxRPD int) bool {
	if maxRPD <= 0 {
		return true
	}
	key := rpdKey(keyID)
	n := rl.increment(key)
	return n <= int64(maxRPD)
}

func (rl *RateLimiter) increment(key string) int64 {
	actual, _ := rl.counts.LoadOrStore(key, new(int64))
	counter := actual.(*int64)
	return atomic.AddInt64(counter, 1)
}

func rpmKey(keyID int64) string {
	minute := time.Now().UTC().Truncate(time.Minute).Unix()
	return fmt.Sprintf("rpm:%d:%d", keyID, minute)
}

func rpdKey(keyID int64) string {
	day := time.Now().UTC().Truncate(24 * time.Hour).Unix()
	return fmt.Sprintf("rpd:%d:%d", keyID, day)
}
