package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

func TestGetLogByID(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	if err := s.LogRequest(ctx, &storage.RequestLog{
		RequestID: "req-abc123", Model: "gpt-4", Provider: "openai",
		Endpoint: "/v1/chat/completions", StatusCode: 200, LatencyMS: 100,
		RequestTime: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("LogRequest: %v", err)
	}

	log, err := s.GetLogByID(ctx, "req-abc123")
	if err != nil {
		t.Fatalf("GetLogByID: %v", err)
	}
	if log == nil {
		t.Fatal("GetLogByID returned nil, want entry")
	}
	if log.RequestID != "req-abc123" {
		t.Errorf("RequestID: got %q, want req-abc123", log.RequestID)
	}
	if log.Provider != "openai" {
		t.Errorf("Provider: got %q, want openai", log.Provider)
	}
}

func TestGetLogByIDNotFound(t *testing.T) {
	s := newTestStorage(t)
	log, err := s.GetLogByID(context.Background(), "does-not-exist")
	if err != nil {
		t.Fatalf("GetLogByID unexpected error: %v", err)
	}
	if log != nil {
		t.Errorf("GetLogByID: got non-nil, want nil for unknown id")
	}
}

func TestGetLogsMeta_EmptySlices(t *testing.T) {
	s := newTestStorage(t)
	meta, err := s.GetLogsMeta(context.Background())
	if err != nil {
		t.Fatalf("GetLogsMeta: %v", err)
	}
	if meta == nil {
		t.Fatal("GetLogsMeta returned nil")
	}
	if meta.Teams == nil {
		t.Error("Teams: got nil, want empty slice")
	}
	if meta.Apps == nil {
		t.Error("Apps: got nil, want empty slice")
	}
	if meta.Keys == nil {
		t.Error("Keys: got nil, want empty slice")
	}
	if meta.Providers == nil {
		t.Error("Providers: got nil, want empty slice")
	}
	if meta.Models == nil {
		t.Error("Models: got nil, want empty slice")
	}
	if meta.Pools == nil {
		t.Error("Pools: got nil, want empty slice")
	}
}

func TestGetLogsMeta_Populated(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	for _, r := range []struct {
		id, provider, pool string
	}{
		{"meta-001", "openai", "fast-pool"},
		{"meta-002", "anthropic", "backup"},
		{"meta-003", "openai", "fast-pool"}, // duplicate — should appear once
	} {
		if err := s.LogRequest(ctx, &storage.RequestLog{
			RequestID: r.id, Model: "gpt-4", Provider: r.provider, PoolName: r.pool,
			Endpoint: "/v1/chat/completions", StatusCode: 200, LatencyMS: 10,
			RequestTime: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("LogRequest %s: %v", r.id, err)
		}
	}

	meta, err := s.GetLogsMeta(ctx)
	if err != nil {
		t.Fatalf("GetLogsMeta: %v", err)
	}
	if len(meta.Providers) != 2 {
		t.Errorf("Providers: got %d, want 2 (openai, anthropic)", len(meta.Providers))
	}
	if len(meta.Pools) != 2 {
		t.Errorf("Pools: got %d, want 2 (fast-pool, backup)", len(meta.Pools))
	}
	// Model "gpt-4" appears 3 times but should deduplicate to 1.
	if len(meta.Models) != 1 {
		t.Errorf("Models: got %d, want 1", len(meta.Models))
	}
}
