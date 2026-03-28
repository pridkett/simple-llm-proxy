package sqlite

import (
	"context"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// TestLogRequestColumnNames verifies that LogRequest inserts into the
// renamed columns (input_tokens, output_tokens, is_streaming, deployment_key)
// and that GetLogs reads them back correctly.
// This test FAILS until Task 1 updates the INSERT/SELECT statements.
func TestLogRequestColumnNames(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	log := &storage.RequestLog{
		RequestID:     "test-req-001",
		APIKeyID:      nil,
		Model:         "gpt-4",
		Provider:      "openai",
		Endpoint:      "/v1/chat/completions",
		InputTokens:   10,
		OutputTokens:  20,
		TotalCost:     0.001,
		StatusCode:    200,
		LatencyMS:     150,
		IsStreaming:   true,
		DeploymentKey: "openai:gpt-4:",
	}

	if err := s.LogRequest(ctx, log); err != nil {
		t.Fatalf("LogRequest failed: %v", err)
	}

	// Query the raw columns to confirm the INSERT used the new names.
	var inputTokens, outputTokens int
	var isStreaming bool
	var deploymentKey string
	err := s.db.QueryRowContext(ctx,
		"SELECT input_tokens, output_tokens, is_streaming, deployment_key FROM usage_logs WHERE request_id = ?",
		"test-req-001",
	).Scan(&inputTokens, &outputTokens, &isStreaming, &deploymentKey)
	if err != nil {
		t.Fatalf("SELECT from usage_logs failed: %v", err)
	}

	if inputTokens != 10 {
		t.Errorf("input_tokens: got %d, want 10", inputTokens)
	}
	if outputTokens != 20 {
		t.Errorf("output_tokens: got %d, want 20", outputTokens)
	}
	if !isStreaming {
		t.Errorf("is_streaming: got %v, want true", isStreaming)
	}
	if deploymentKey != "openai:gpt-4:" {
		t.Errorf("deployment_key: got %q, want %q", deploymentKey, "openai:gpt-4:")
	}
}

// TestGetLogsColumnNames verifies that GetLogs reads back InputTokens, OutputTokens,
// IsStreaming, and DeploymentKey correctly from the usage_logs table.
func TestGetLogsColumnNames(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	log := &storage.RequestLog{
		RequestID:     "test-req-002",
		APIKeyID:      nil,
		Model:         "claude-3",
		Provider:      "anthropic",
		Endpoint:      "/v1/chat/completions",
		InputTokens:   42,
		OutputTokens:  150,
		TotalCost:     0.005,
		StatusCode:    200,
		LatencyMS:     300,
		IsStreaming:   true,
		DeploymentKey: "anthropic:claude-3-sonnet-20240229:",
	}

	if err := s.LogRequest(ctx, log); err != nil {
		t.Fatalf("LogRequest failed: %v", err)
	}

	logs, total, err := s.GetLogs(ctx, 10, 0)
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	if total < 1 {
		t.Fatalf("GetLogs total: got %d, want >= 1", total)
	}
	if len(logs) == 0 {
		t.Fatal("GetLogs returned no logs")
	}

	found := logs[0]
	if found.InputTokens != 42 {
		t.Errorf("InputTokens: got %d, want 42", found.InputTokens)
	}
	if found.OutputTokens != 150 {
		t.Errorf("OutputTokens: got %d, want 150", found.OutputTokens)
	}
	if !found.IsStreaming {
		t.Errorf("IsStreaming: got %v, want true", found.IsStreaming)
	}
	if found.DeploymentKey != "anthropic:claude-3-sonnet-20240229:" {
		t.Errorf("DeploymentKey: got %q, want %q", found.DeploymentKey, "anthropic:claude-3-sonnet-20240229:")
	}
}
