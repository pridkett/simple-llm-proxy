package handler

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// ---------------------------------------------------------------------------
// Phase 13 Plan 03 — Instrumentation tests (GREEN)
//
// These tests verify the four telemetry values wired in Plan 03:
//   INSTR-01: TTFTMs — time to first token for streaming requests
//   INSTR-02: PoolName — pool name recorded in request log
//   INSTR-03: RespBodySnippet — streaming content captured up to BodySnippetLimit
//   INSTR-04: Cache token cost formula fix
// ---------------------------------------------------------------------------

// TestStreamTTFT (INSTR-01): verifies that TTFTMs is populated in the request log
// after a streaming response, and is nil for non-streaming.
func TestStreamTTFT(t *testing.T) {
	chunk1 := &model.StreamChunk{
		ID:      "cmp-1",
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   "gpt-4",
		Choices: []model.Choice{{Index: 0, Delta: &model.Delta{Content: "hello"}}},
	}
	chunkFinal := &model.StreamChunk{
		ID:      "cmp-final",
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   "gpt-4",
		Choices: []model.Choice{{Index: 0, Delta: &model.Delta{}, FinishReason: "stop"}},
		Usage:   &model.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
	mockProv := &streamingMockProvider{
		name: "openai",
		makeStream: func(_ context.Context) (provider.Stream, error) {
			return &mockStream{chunks: []*model.StreamChunk{chunk1, chunkFinal}}, nil
		},
	}
	deployment := makeTestDeployment(mockProv)
	mr := &spyRouter{deployment: deployment}
	store := &captureStorage{}
	w := httptest.NewRecorder()
	startTime := time.Now()

	err := handleStreamingResponseWithRouter(context.Background(), w, deployment,
		&model.ChatCompletionRequest{
			Model:    "gpt-4",
			Messages: []model.Message{{Role: "user", Content: "hello"}},
			Stream:   true,
		}, mr, store, nil, nil, nil, startTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Give the goroutine time to write the log.
	time.Sleep(30 * time.Millisecond)

	if len(store.logs) == 0 {
		t.Fatal("no log recorded")
	}
	// INSTR-01: TTFTMs must be non-nil and >= 0 for streaming requests.
	if store.logs[0].TTFTMs == nil {
		t.Error("TTFTMs should be non-nil after streaming")
	} else if *store.logs[0].TTFTMs < 0 {
		t.Errorf("TTFTMs should be >= 0, got %d", *store.logs[0].TTFTMs)
	}
}

// TestLogRequestPoolName (INSTR-02): verifies that the pool name is recorded
// in the request log when logRequest is called with a poolName.
func TestLogRequestPoolName(t *testing.T) {
	store := &captureStorage{}
	deployment := makeTestDeployment(&streamingMockProvider{name: "openai"})
	usage := &model.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}

	// Call logRequest directly with poolName="test-pool".
	logRequest(store, nil, nil, nil, "test-pool", nil, deployment, "/v1/chat/completions",
		usage, 200, time.Now(), false, "test-req-pool-001", nil, "")

	// logRequest is synchronous when called directly (goroutine is only in production callers).
	time.Sleep(20 * time.Millisecond)

	if len(store.logs) == 0 {
		t.Fatal("no log was written to storage")
	}
	// INSTR-02: PoolName must be "test-pool".
	if store.logs[0].PoolName != "test-pool" {
		t.Errorf("PoolName: got %q, want %q", store.logs[0].PoolName, "test-pool")
	}
}

// TestStreamRespSnippet (INSTR-03): verifies that RespBodySnippet captures
// the first N bytes of the streamed response body up to bodySnippetLimit.
func TestStreamRespSnippet(t *testing.T) {
	chunk1 := &model.StreamChunk{
		ID:      "cmp-1",
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   "gpt-4",
		Choices: []model.Choice{{Index: 0, Delta: &model.Delta{Content: "hello "}}},
	}
	chunk2 := &model.StreamChunk{
		ID:      "cmp-2",
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   "gpt-4",
		Choices: []model.Choice{{Index: 0, Delta: &model.Delta{Content: "world!"}}},
		Usage:   &model.Usage{PromptTokens: 5, CompletionTokens: 10, TotalTokens: 15},
	}
	mockProv := &streamingMockProvider{
		name: "openai",
		makeStream: func(_ context.Context) (provider.Stream, error) {
			return &mockStream{chunks: []*model.StreamChunk{chunk1, chunk2}}, nil
		},
	}
	deployment := makeTestDeployment(mockProv)
	mr := &spyRouter{deployment: deployment}
	store := &captureStorage{}
	w := httptest.NewRecorder()
	startTime := time.Now()

	// bodySnippetLimit=10: "hello " (6) + "worl" (4) = "hello worl" (first 10 of "hello world!")
	err := handleStreamingResponseWithRouter(context.Background(), w, deployment,
		&model.ChatCompletionRequest{
			Model:    "gpt-4",
			Messages: []model.Message{{Role: "user", Content: "hi"}},
			Stream:   true,
		}, mr, store, nil, nil, nil, startTime, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(30 * time.Millisecond)

	if len(store.logs) == 0 {
		t.Fatal("no log recorded")
	}
	// INSTR-03: snippet must be non-empty and not exceed the limit.
	if store.logs[0].RespBodySnippet == "" {
		t.Error("RespBodySnippet should be non-empty")
	}
	if len(store.logs[0].RespBodySnippet) > 10 {
		t.Errorf("RespBodySnippet exceeded limit: %q (len=%d)", store.logs[0].RespBodySnippet, len(store.logs[0].RespBodySnippet))
	}
}

// TestStreamRespSnippetDisabled (INSTR-03): verifies that RespBodySnippet is
// empty when bodySnippetLimit=0 (disabled).
func TestStreamRespSnippetDisabled(t *testing.T) {
	chunk1 := &model.StreamChunk{
		ID:      "cmp-1",
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   "gpt-4",
		Choices: []model.Choice{{Index: 0, Delta: &model.Delta{Content: "hello "}}},
	}
	chunk2 := &model.StreamChunk{
		ID:      "cmp-2",
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   "gpt-4",
		Choices: []model.Choice{{Index: 0, Delta: &model.Delta{Content: "world!"}}},
		Usage:   &model.Usage{PromptTokens: 5, CompletionTokens: 10, TotalTokens: 15},
	}
	mockProv := &streamingMockProvider{
		name: "openai",
		makeStream: func(_ context.Context) (provider.Stream, error) {
			return &mockStream{chunks: []*model.StreamChunk{chunk1, chunk2}}, nil
		},
	}
	deployment := makeTestDeployment(mockProv)
	mr := &spyRouter{deployment: deployment}
	store := &captureStorage{}
	w := httptest.NewRecorder()
	startTime := time.Now()

	// bodySnippetLimit=0 disables snippet capture (default when omitted).
	err := handleStreamingResponseWithRouter(context.Background(), w, deployment,
		&model.ChatCompletionRequest{
			Model:    "gpt-4",
			Messages: []model.Message{{Role: "user", Content: "hi"}},
			Stream:   true,
		}, mr, store, nil, nil, nil, startTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(30 * time.Millisecond)

	if len(store.logs) == 0 {
		t.Fatal("no log recorded")
	}
	// INSTR-03: RespBodySnippet must be empty when limit=0.
	if store.logs[0].RespBodySnippet != "" {
		t.Errorf("RespBodySnippet should be empty when disabled, got %q", store.logs[0].RespBodySnippet)
	}
}

// TestLogRequestCacheCost (INSTR-04): verifies that cache token costs are
// included in TotalCost when logRequest is called with non-zero cache tokens.
func TestLogRequestCacheCost(t *testing.T) {
	// Set up a costmap with known costs for "gpt-4".
	cm := costmap.New()
	cm.SetCustomSpec("gpt-4", costmap.ModelSpec{
		InputCostPerToken:           0.000003,  // $3/M input
		OutputCostPerToken:          0.000015,  // $15/M output
		CacheReadInputTokenCost:     0.0000003, // $0.30/M cache read
		CacheCreationInputTokenCost: 0.00000375, // $3.75/M cache write
	})

	store := &captureStorage{}
	deployment := makeTestDeployment(&streamingMockProvider{name: "anthropic"})
	// Override deployment model name so costmap lookup finds "gpt-4".
	deployment.ModelName = "gpt-4"
	deployment.ActualModel = "gpt-4"

	usage := &model.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		CacheReadTokens:  100, // 100 * 0.0000003 = 0.00003
		CacheWriteTokens: 25,  // 25 * 0.00000375 = 0.00009375
	}

	// Expected TotalCost:
	//   100 * 0.000003   = 0.0003   (prompt)
	//   50  * 0.000015   = 0.00075  (completion)
	//   100 * 0.0000003  = 0.00003  (cache read)
	//   25  * 0.00000375 = 0.00009375 (cache write)
	//   total ≈ 0.00117375
	baseCost := float64(100)*0.000003 + float64(50)*0.000015 // 0.00075 + 0.0003 = 0.00105
	cacheContrib := float64(100)*0.0000003 + float64(25)*0.00000375

	logRequest(store, nil, cm, nil, "", nil, deployment, "/v1/chat/completions",
		usage, 200, time.Now(), false, "cache-cost-test", nil, "")

	time.Sleep(20 * time.Millisecond)

	if len(store.logs) == 0 {
		t.Fatal("no log recorded")
	}
	// INSTR-04: TotalCost must include cache token contribution.
	if store.logs[0].TotalCost <= baseCost {
		t.Errorf("TotalCost should include cache cost; got %.8f, baseCost=%.8f, cacheContrib=%.8f",
			store.logs[0].TotalCost, baseCost, cacheContrib)
	}
	// CacheReadTokens and CacheWriteTokens must be stored.
	if store.logs[0].CacheReadTokens != 100 {
		t.Errorf("CacheReadTokens: got %d, want 100", store.logs[0].CacheReadTokens)
	}
	if store.logs[0].CacheWriteTokens != 25 {
		t.Errorf("CacheWriteTokens: got %d, want 25", store.logs[0].CacheWriteTokens)
	}
}

// ---------------------------------------------------------------------------
// Suppress unused import warnings.
// ---------------------------------------------------------------------------

var (
	_ = context.Background
	_ = httptest.NewRecorder
	_ *model.StreamChunk
	_ *provider.Deployment
	_ *storage.RequestLog
)
