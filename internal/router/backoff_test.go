package router

import (
	"testing"
	"time"
)

func TestBackoffManagerNotInBackoffInitially(t *testing.T) {
	bm := NewBackoffManager()
	if bm.InBackoff("openai:gpt-4:") {
		t.Error("deployment should not be in backoff initially")
	}
}

func TestBackoffManagerInBackoffAfterRateLimit(t *testing.T) {
	bm := NewBackoffManager()
	bm.ReportRateLimit("openai:gpt-4:", 5*time.Second)
	if !bm.InBackoff("openai:gpt-4:") {
		t.Error("deployment should be in backoff after rate limit report")
	}
}

func TestBackoffManagerHonorsRetryAfter(t *testing.T) {
	bm := NewBackoffManager()
	// RetryAfter of 2 seconds: deployment should be in backoff for at least 2 seconds
	bm.ReportRateLimit("anthropic:claude-3-5-sonnet-20241022:", 2*time.Second)
	if !bm.InBackoff("anthropic:claude-3-5-sonnet-20241022:") {
		t.Error("deployment should be in backoff honoring Retry-After=2s")
	}
}

func TestBackoffManagerExpires(t *testing.T) {
	bm := NewBackoffManager()
	// Use a very short retry-after to test expiry
	bm.ReportRateLimit("openai:gpt-4:", 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	// After expiry, should not be in backoff
	if bm.InBackoff("openai:gpt-4:") {
		t.Error("backoff should expire after retry-after duration passes")
	}
}

func TestBackoffManagerReset(t *testing.T) {
	bm := NewBackoffManager()
	bm.ReportRateLimit("openai:gpt-4:", 60*time.Second)
	bm.Reset("openai:gpt-4:")
	if bm.InBackoff("openai:gpt-4:") {
		t.Error("deployment should not be in backoff after Reset")
	}
}

func TestBackoffManagerAttemptsIncrement(t *testing.T) {
	bm := NewBackoffManager()
	// Multiple reports should use exponential backoff
	bm.ReportRateLimit("openai:gpt-4:", 0) // no Retry-After
	state1 := bm.getState("openai:gpt-4:")
	bm.ReportRateLimit("openai:gpt-4:", 0)
	state2 := bm.getState("openai:gpt-4:")
	if state2.attempts <= state1.attempts {
		t.Errorf("attempts should increase: got state1.attempts=%d, state2.attempts=%d", state1.attempts, state2.attempts)
	}
}

func TestBackoffManagerDifferentKeysIndependent(t *testing.T) {
	bm := NewBackoffManager()
	bm.ReportRateLimit("openai:gpt-4:", 60*time.Second)
	if bm.InBackoff("anthropic:claude-3-5-sonnet-20241022:") {
		t.Error("different deployment keys should have independent backoff state")
	}
}
