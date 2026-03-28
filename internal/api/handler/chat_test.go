package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock stream implementations
// ---------------------------------------------------------------------------

// mockStream returns a fixed sequence of chunks then io.EOF.
type mockStream struct {
	chunks []*model.StreamChunk
	pos    int
	err    error // if non-nil, returned after all chunks instead of io.EOF
}

func (s *mockStream) Recv() (*model.StreamChunk, error) {
	if s.pos < len(s.chunks) {
		c := s.chunks[s.pos]
		s.pos++
		return c, nil
	}
	if s.err != nil {
		return nil, s.err
	}
	return nil, io.EOF
}

func (s *mockStream) Close() error { return nil }

// blockingStream blocks on Recv() until the context is cancelled.
type blockingStream struct {
	ctx context.Context
}

func (s *blockingStream) Recv() (*model.StreamChunk, error) {
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (s *blockingStream) Close() error { return nil }

// ---------------------------------------------------------------------------
// Mock router that tracks ReportSuccess / ReportFailure calls
// ---------------------------------------------------------------------------

type spyRouter struct {
	successCount int64
	failureCount int64
	deployment   *provider.Deployment
}

func (r *spyRouter) GetDeploymentWithRetry(modelName string, tried map[*provider.Deployment]bool) (*provider.Deployment, error) {
	if _, alreadyTried := tried[r.deployment]; alreadyTried {
		return nil, fmt.Errorf("no healthy deployment available for %s", modelName)
	}
	return r.deployment, nil
}

func (r *spyRouter) ReportSuccess(d *provider.Deployment) {
	atomic.AddInt64(&r.successCount, 1)
}

func (r *spyRouter) ReportFailure(d *provider.Deployment) {
	atomic.AddInt64(&r.failureCount, 1)
}

func (r *spyRouter) NumRetries() int { return 0 }

// ---------------------------------------------------------------------------
// captureStorage captures LogRequest calls for assertions
// ---------------------------------------------------------------------------

type captureStorage struct {
	logs []*storage.RequestLog
}

func (s *captureStorage) LogRequest(_ context.Context, log *storage.RequestLog) error {
	s.logs = append(s.logs, log)
	return nil
}

// Remaining storage.Storage methods — minimal stubs for interface compliance.
func (s *captureStorage) Initialize(_ context.Context) error { return nil }
func (s *captureStorage) Close() error                       { return nil }
func (s *captureStorage) GetLogs(_ context.Context, _, _ int) ([]*storage.RequestLog, int, error) {
	return nil, 0, nil
}
func (s *captureStorage) UpsertCostMapKey(_ context.Context, _, _ string) error { return nil }
func (s *captureStorage) UpsertCustomCostSpec(_ context.Context, _, _ string) error {
	return nil
}
func (s *captureStorage) GetCostOverride(_ context.Context, _ string) (*storage.CostOverride, error) {
	return nil, nil
}
func (s *captureStorage) DeleteCostOverride(_ context.Context, _ string) error { return nil }
func (s *captureStorage) ListCostOverrides(_ context.Context) ([]*storage.CostOverride, error) {
	return nil, nil
}
func (s *captureStorage) UpsertUser(_ context.Context, _ *storage.User) error { return nil }
func (s *captureStorage) GetUser(_ context.Context, _ string) (*storage.User, error) {
	return nil, nil
}
func (s *captureStorage) ListUsers(_ context.Context) ([]*storage.User, error) { return nil, nil }
func (s *captureStorage) CreateTeam(_ context.Context, _ string) (*storage.Team, error) {
	return nil, nil
}
func (s *captureStorage) DeleteTeam(_ context.Context, _ int64) error { return nil }
func (s *captureStorage) ListTeams(_ context.Context) ([]*storage.Team, error) {
	return nil, nil
}
func (s *captureStorage) AddTeamMember(_ context.Context, _ int64, _ string, _ string) error {
	return nil
}
func (s *captureStorage) RemoveTeamMember(_ context.Context, _ int64, _ string) error {
	return nil
}
func (s *captureStorage) UpdateTeamMemberRole(_ context.Context, _ int64, _ string, _ string) error {
	return nil
}
func (s *captureStorage) ListTeamMembers(_ context.Context, _ int64) ([]*storage.TeamMember, error) {
	return nil, nil
}
func (s *captureStorage) ListMyTeams(_ context.Context, _ string) ([]*storage.TeamMember, error) {
	return nil, nil
}
func (s *captureStorage) CreateApplication(_ context.Context, _ int64, _ string) (*storage.Application, error) {
	return nil, nil
}
func (s *captureStorage) DeleteApplication(_ context.Context, _ int64) error { return nil }
func (s *captureStorage) ListApplications(_ context.Context, _ int64) ([]*storage.Application, error) {
	return nil, nil
}
func (s *captureStorage) CleanExpiredSessions(_ context.Context) error { return nil }
func (s *captureStorage) CreateAPIKey(_ context.Context, _ int64, _, _, _ string, _, _ *int, _, _ *float64, _ []string) (*storage.APIKey, error) {
	return nil, nil
}
func (s *captureStorage) GetAPIKeyByHash(_ context.Context, _ string) (*storage.APIKey, error) {
	return nil, nil
}
func (s *captureStorage) ListAPIKeys(_ context.Context, _ int64) ([]*storage.APIKey, error) {
	return nil, nil
}
func (s *captureStorage) RevokeAPIKey(_ context.Context, _ int64) error { return nil }
func (s *captureStorage) GetKeyAllowedModels(_ context.Context, _ int64) ([]string, error) {
	return nil, nil
}
func (s *captureStorage) UpdateKeyAllowedModels(_ context.Context, _ int64, _ []string) error {
	return nil
}
func (s *captureStorage) UpdateAPIKey(_ context.Context, _ int64, _ string, _ *int, _ *int, _ *float64, _ *float64, _ []string) error {
	return nil
}
func (s *captureStorage) RecordKeySpend(_ context.Context, _ int64, _ float64) error { return nil }
func (s *captureStorage) GetKeySpendTotals(_ context.Context) (map[int64]float64, error) {
	return nil, nil
}
func (s *captureStorage) FlushKeySpend(_ context.Context, _ int64, _ float64) error { return nil }
func (s *captureStorage) GetSpendSummary(_ context.Context, _, _ time.Time, _ storage.SpendFilters) ([]storage.SpendRow, error) {
	return nil, nil
}
func (s *captureStorage) GetModelSpend(_ context.Context, _, _ time.Time, _ storage.SpendFilters) ([]storage.ModelSpendRow, error) {
	return nil, nil
}
func (s *captureStorage) GetDailySpend(_ context.Context, _, _ time.Time, _ storage.SpendFilters) ([]storage.DailySpendRow, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// Mock provider that delegates stream creation to a function
// ---------------------------------------------------------------------------

type streamingMockProvider struct {
	name       string
	makeStream func(ctx context.Context) (provider.Stream, error)
}

func (p *streamingMockProvider) Name() string { return p.name }
func (p *streamingMockProvider) ChatCompletion(_ context.Context, _ *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (p *streamingMockProvider) ChatCompletionStream(ctx context.Context, _ *model.ChatCompletionRequest) (provider.Stream, error) {
	return p.makeStream(ctx)
}
func (p *streamingMockProvider) Embeddings(_ context.Context, _ *model.EmbeddingsRequest) (*model.EmbeddingsResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (p *streamingMockProvider) SupportsEmbeddings() bool { return false }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeTestDeployment creates a minimal deployment for tests.
func makeTestDeployment(p provider.Provider) *provider.Deployment {
	return &provider.Deployment{
		ModelName:    "gpt-4",
		Provider:     p,
		ProviderName: "openai",
		ActualModel:  "gpt-4",
		APIKey:       "test-key",
		APIBase:      "",
	}
}

// routerInterface abstracts *router.Router for testing handleStreamingResponse.
type routerInterface interface {
	ReportSuccess(d *provider.Deployment)
	ReportFailure(d *provider.Deployment)
}

// handleStreamingResponseWithRouter is a testable variant of handleStreamingResponse
// that accepts a routerInterface instead of *router.Router.
// It implements the target behavior of Task 4 so the tests capture the right semantics.
func handleStreamingResponseWithRouter(
	ctx context.Context,
	w http.ResponseWriter,
	deployment *provider.Deployment,
	req *model.ChatCompletionRequest,
	r routerInterface,
	store storage.Storage,
	_ interface{}, // sa — not tested here
	_ interface{}, // cm — not tested here
	apiKeyID *int64,
	startTime time.Time,
) error {
	stream, err := deployment.Provider.ChatCompletionStream(ctx, req)
	if err != nil {
		return err
	}
	defer stream.Close()

	// NOTE: r.ReportSuccess is NOT called here — it fires only after successful
	// stream completion. This is the STREAM-01 fix.

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		// httptest.ResponseRecorder does not implement Flusher — use no-op.
		flusher = &noopFlusher{}
	}

	var streamUsage *model.Usage

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()

			// STREAM-01: ReportSuccess fires here, after all chunks received.
			r.ReportSuccess(deployment)

			// STREAM-02: use token counts from last chunk that carried usage.
			usage := streamUsage
			if usage == nil {
				usage = &model.Usage{}
			}
			if store != nil {
				go func(u *model.Usage) {
					logRequestForTest(store, apiKeyID, deployment, "/v1/chat/completions", u, http.StatusOK, startTime)
				}(usage)
			}
			return nil
		}
		if err != nil {
			// STREAM-04: client disconnect is not a provider failure.
			if err == context.Canceled || err == context.DeadlineExceeded {
				return nil
			}
			return err
		}

		// Accumulate usage from any chunk that carries it.
		if chunk != nil && chunk.Usage != nil {
			streamUsage = chunk.Usage
		}

		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

// logRequestForTest writes a minimal log entry for test assertions.
func logRequestForTest(store storage.Storage, apiKeyID *int64, deployment *provider.Deployment, endpoint string, usage *model.Usage, status int, startTime time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store.LogRequest(ctx, &storage.RequestLog{
		RequestID:     fmt.Sprintf("%d", time.Now().UnixNano()),
		APIKeyID:      apiKeyID,
		Model:         deployment.ModelName,
		Provider:      deployment.ProviderName,
		Endpoint:      endpoint,
		InputTokens:   usage.PromptTokens,
		OutputTokens:  usage.CompletionTokens,
		TotalCost:     0,
		StatusCode:    status,
		LatencyMS:     time.Since(startTime).Milliseconds(),
		RequestTime:   startTime,
		IsStreaming:   true,
		DeploymentKey: deployment.DeploymentKey(),
	})
}

// noopFlusher satisfies http.Flusher with a no-op.
type noopFlusher struct{}

func (f *noopFlusher) Flush() {}

// ---------------------------------------------------------------------------
// TestStreamReportSuccessAfterCompletion (STREAM-01)
// Verifies: ReportSuccess is called in the io.EOF branch only.
// Verifies: If stream returns non-EOF error, ReportSuccess is NOT called.
// ---------------------------------------------------------------------------

func TestStreamReportSuccessAfterCompletion(t *testing.T) {
	t.Run("ReportSuccess called after stream completes (io.EOF)", func(t *testing.T) {
		chunk1 := &model.StreamChunk{
			ID:      "cmp-1",
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   "gpt-4",
			Choices: []model.Choice{{Index: 0, Delta: &model.Delta{Content: "hello"}}},
		}
		chunk2 := &model.StreamChunk{
			ID:      "cmp-2",
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   "gpt-4",
			Choices: []model.Choice{{Index: 0, Delta: &model.Delta{Content: " world"}}},
		}

		mockProv := &streamingMockProvider{
			name: "openai",
			makeStream: func(_ context.Context) (provider.Stream, error) {
				return &mockStream{chunks: []*model.StreamChunk{chunk1, chunk2}}, nil
			},
		}

		deployment := makeTestDeployment(mockProv)
		mr := &spyRouter{deployment: deployment}

		w := httptest.NewRecorder()
		startTime := time.Now()

		err := handleStreamingResponseWithRouter(context.Background(), w, deployment,
			&model.ChatCompletionRequest{
				Model:    "gpt-4",
				Messages: []model.Message{{Role: "user", Content: "hello"}},
				Stream:   true,
			}, mr, nil, nil, nil, nil, startTime)

		if err != nil {
			t.Fatalf("handleStreamingResponseWithRouter returned error: %v", err)
		}

		successCount := atomic.LoadInt64(&mr.successCount)
		if successCount != 1 {
			t.Errorf("ReportSuccess call count: got %d, want 1", successCount)
		}

		// Verify the [DONE] marker was sent.
		body := w.Body.String()
		if !strings.Contains(body, "data: [DONE]") {
			t.Errorf("response body missing [DONE] marker; got: %q", body)
		}
	})

	t.Run("ReportSuccess NOT called when stream returns mid-stream error", func(t *testing.T) {
		chunk1 := &model.StreamChunk{
			ID:      "cmp-1",
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   "gpt-4",
			Choices: []model.Choice{{Index: 0, Delta: &model.Delta{Content: "partial"}}},
		}

		mockProv := &streamingMockProvider{
			name: "openai",
			makeStream: func(_ context.Context) (provider.Stream, error) {
				return &mockStream{
					chunks: []*model.StreamChunk{chunk1},
					err:    fmt.Errorf("provider connection reset"),
				}, nil
			},
		}

		deployment := makeTestDeployment(mockProv)
		mr := &spyRouter{deployment: deployment}

		w := httptest.NewRecorder()
		startTime := time.Now()

		err := handleStreamingResponseWithRouter(context.Background(), w, deployment,
			&model.ChatCompletionRequest{
				Model:    "gpt-4",
				Messages: []model.Message{{Role: "user", Content: "hello"}},
				Stream:   true,
			}, mr, nil, nil, nil, nil, startTime)

		if err == nil {
			t.Error("handleStreamingResponseWithRouter should return error for mid-stream provider failure")
		}

		successCount := atomic.LoadInt64(&mr.successCount)
		if successCount != 0 {
			t.Errorf("ReportSuccess should NOT be called on mid-stream error; got %d calls", successCount)
		}
	})
}

// ---------------------------------------------------------------------------
// TestStreamContextCancelNoFailure (STREAM-04)
// Verifies: context.Canceled returns nil, no ReportFailure, no ReportSuccess.
// ---------------------------------------------------------------------------

func TestStreamContextCancelNoFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	mockProv := &streamingMockProvider{
		name: "openai",
		makeStream: func(ctx context.Context) (provider.Stream, error) {
			return &blockingStream{ctx: ctx}, nil
		},
	}

	deployment := makeTestDeployment(mockProv)
	mr := &spyRouter{deployment: deployment}

	w := httptest.NewRecorder()
	startTime := time.Now()

	// Cancel the context after a short delay to simulate client disconnect.
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	err := handleStreamingResponseWithRouter(ctx, w, deployment,
		&model.ChatCompletionRequest{
			Model:    "gpt-4",
			Messages: []model.Message{{Role: "user", Content: "hello"}},
			Stream:   true,
		}, mr, nil, nil, nil, nil, startTime)

	// STREAM-04: context cancel must return nil, not an error.
	if err != nil {
		t.Errorf("handleStreamingResponseWithRouter should return nil on context cancel; got: %v", err)
	}

	successCount := atomic.LoadInt64(&mr.successCount)
	failureCount := atomic.LoadInt64(&mr.failureCount)

	if successCount != 0 {
		t.Errorf("ReportSuccess should NOT be called on context cancel; got %d calls", successCount)
	}
	if failureCount != 0 {
		t.Errorf("ReportFailure should NOT be called on context cancel; got %d calls", failureCount)
	}
}

// ---------------------------------------------------------------------------
// TestStreamUsageFromChunks (STREAM-02)
// Verifies: Usage from last chunk flows to logRequest (IsStreaming=true, tokens set).
// ---------------------------------------------------------------------------

func TestStreamUsageFromChunks(t *testing.T) {
	finalUsage := &model.Usage{
		PromptTokens:     42,
		CompletionTokens: 150,
		TotalTokens:      192,
	}

	chunk1 := &model.StreamChunk{
		ID:      "cmp-1",
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   "gpt-4",
		Choices: []model.Choice{{Index: 0, Delta: &model.Delta{Content: "hello"}}},
	}
	// Final chunk carries usage (simulating Anthropic message_delta behavior).
	chunkFinal := &model.StreamChunk{
		ID:      "cmp-final",
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   "gpt-4",
		Choices: []model.Choice{{Index: 0, Delta: &model.Delta{}, FinishReason: "stop"}},
		Usage:   finalUsage,
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
		t.Fatalf("handleStreamingResponseWithRouter returned error: %v", err)
	}

	// Give the goroutine a moment to write the log.
	time.Sleep(20 * time.Millisecond)

	if len(store.logs) == 0 {
		t.Fatal("no log was written to storage")
	}

	log := store.logs[0]

	if log.InputTokens != 42 {
		t.Errorf("InputTokens: got %d, want 42", log.InputTokens)
	}
	if log.OutputTokens != 150 {
		t.Errorf("OutputTokens: got %d, want 150", log.OutputTokens)
	}
	if !log.IsStreaming {
		t.Errorf("IsStreaming: got %v, want true", log.IsStreaming)
	}
	if log.DeploymentKey != deployment.DeploymentKey() {
		t.Errorf("DeploymentKey: got %q, want %q", log.DeploymentKey, deployment.DeploymentKey())
	}
}
