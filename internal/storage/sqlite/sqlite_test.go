package sqlite

import (
	"context"
	"testing"
	"time"

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

	logs, total, err := s.GetLogs(ctx, 10, 0, storage.LogsFilter{})
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

// TestGetLogsEnrichedFields verifies that GetLogs resolves key, app, and team names
// via LEFT JOIN when an api_key_id is present.
func TestGetLogsEnrichedFields(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Set up identity chain: team -> app -> key.
	team, err := s.CreateTeam(ctx, "engineering")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	app, err := s.CreateApplication(ctx, team.ID, "chatbot")
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	key, err := s.CreateAPIKey(ctx, app.ID, "dev-key", "sk-abcde", "hash123", nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	// Insert a log with the key.
	now := time.Now().UTC().Round(0)
	keyID := key.ID
	logEntry := &storage.RequestLog{
		RequestID:     "enriched-001",
		APIKeyID:      &keyID,
		Model:         "gpt-4",
		Provider:      "openai",
		Endpoint:      "/v1/chat/completions",
		InputTokens:   100,
		OutputTokens:  200,
		TotalCost:     0.05,
		StatusCode:    200,
		LatencyMS:     500,
		RequestTime:   now,
		IsStreaming:   false,
		DeploymentKey: "openai:gpt-4:",
	}
	if err := s.LogRequest(ctx, logEntry); err != nil {
		t.Fatalf("LogRequest: %v", err)
	}

	// Also insert a master key log (no api_key_id).
	masterLog := &storage.RequestLog{
		RequestID:     "enriched-002",
		APIKeyID:      nil,
		Model:         "claude-3",
		Provider:      "anthropic",
		Endpoint:      "/v1/chat/completions",
		InputTokens:   50,
		OutputTokens:  75,
		TotalCost:     0.01,
		StatusCode:    200,
		LatencyMS:     200,
		RequestTime:   now.Add(-time.Second),
		IsStreaming:   false,
		DeploymentKey: "",
	}
	if err := s.LogRequest(ctx, masterLog); err != nil {
		t.Fatalf("LogRequest master: %v", err)
	}

	logs, total, err := s.GetLogs(ctx, 10, 0, storage.LogsFilter{})
	if err != nil {
		t.Fatalf("GetLogs: %v", err)
	}
	if total != 2 {
		t.Fatalf("total: got %d, want 2", total)
	}

	// First log (most recent) should be the keyed one with enriched fields.
	keyed := logs[0]
	if keyed.RequestID != "enriched-001" {
		t.Fatalf("expected enriched-001 first, got %s", keyed.RequestID)
	}
	if keyed.KeyName != "dev-key" {
		t.Errorf("KeyName: got %q, want %q", keyed.KeyName, "dev-key")
	}
	if keyed.AppName != "chatbot" {
		t.Errorf("AppName: got %q, want %q", keyed.AppName, "chatbot")
	}
	if keyed.TeamName != "engineering" {
		t.Errorf("TeamName: got %q, want %q", keyed.TeamName, "engineering")
	}
	if keyed.APIKeyID == nil || *keyed.APIKeyID != key.ID {
		t.Errorf("APIKeyID: got %v, want %d", keyed.APIKeyID, key.ID)
	}

	// Second log should have empty enriched fields (master key).
	master := logs[1]
	if master.RequestID != "enriched-002" {
		t.Fatalf("expected enriched-002 second, got %s", master.RequestID)
	}
	if master.KeyName != "" {
		t.Errorf("master KeyName: got %q, want empty", master.KeyName)
	}
	if master.AppName != "" {
		t.Errorf("master AppName: got %q, want empty", master.AppName)
	}
	if master.TeamName != "" {
		t.Errorf("master TeamName: got %q, want empty", master.TeamName)
	}
	if master.APIKeyID != nil {
		t.Errorf("master APIKeyID: got %v, want nil", master.APIKeyID)
	}
}

// TestGetLogsFilters verifies that model, team_id, and app_id filters work correctly.
func TestGetLogsFilters(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Set up two teams with apps and keys.
	team1, _ := s.CreateTeam(ctx, "team-alpha")
	team2, _ := s.CreateTeam(ctx, "team-beta")
	app1, _ := s.CreateApplication(ctx, team1.ID, "app-one")
	app2, _ := s.CreateApplication(ctx, team2.ID, "app-two")
	key1, _ := s.CreateAPIKey(ctx, app1.ID, "key-1", "sk-1111", "hash-1", nil, nil, nil, nil, nil)
	key2, _ := s.CreateAPIKey(ctx, app2.ID, "key-2", "sk-2222", "hash-2", nil, nil, nil, nil, nil)

	now := time.Now().UTC().Round(0)
	k1ID := key1.ID
	k2ID := key2.ID

	// Log: key1, gpt-4
	s.LogRequest(ctx, &storage.RequestLog{
		RequestID: "filter-001", APIKeyID: &k1ID, Model: "gpt-4", Provider: "openai",
		Endpoint: "/v1/chat/completions", StatusCode: 200, LatencyMS: 100, RequestTime: now,
	})
	// Log: key2, claude-3
	s.LogRequest(ctx, &storage.RequestLog{
		RequestID: "filter-002", APIKeyID: &k2ID, Model: "claude-3", Provider: "anthropic",
		Endpoint: "/v1/chat/completions", StatusCode: 200, LatencyMS: 100, RequestTime: now.Add(-time.Second),
	})
	// Log: master key, gpt-4
	s.LogRequest(ctx, &storage.RequestLog{
		RequestID: "filter-003", APIKeyID: nil, Model: "gpt-4", Provider: "openai",
		Endpoint: "/v1/chat/completions", StatusCode: 200, LatencyMS: 100, RequestTime: now.Add(-2 * time.Second),
	})

	t.Run("no filters returns all", func(t *testing.T) {
		logs, total, err := s.GetLogs(ctx, 10, 0, storage.LogsFilter{})
		if err != nil {
			t.Fatalf("GetLogs: %v", err)
		}
		if total != 3 {
			t.Errorf("total: got %d, want 3", total)
		}
		if len(logs) != 3 {
			t.Errorf("len: got %d, want 3", len(logs))
		}
	})

	t.Run("filter by model", func(t *testing.T) {
		logs, total, err := s.GetLogs(ctx, 10, 0, storage.LogsFilter{Model: "gpt-4"})
		if err != nil {
			t.Fatalf("GetLogs: %v", err)
		}
		if total != 2 {
			t.Errorf("total: got %d, want 2", total)
		}
		if len(logs) != 2 {
			t.Errorf("len: got %d, want 2", len(logs))
		}
	})

	t.Run("filter by team_id", func(t *testing.T) {
		tid := team1.ID
		logs, total, err := s.GetLogs(ctx, 10, 0, storage.LogsFilter{TeamID: &tid})
		if err != nil {
			t.Fatalf("GetLogs: %v", err)
		}
		if total != 1 {
			t.Errorf("total: got %d, want 1", total)
		}
		if len(logs) != 1 {
			t.Errorf("len: got %d, want 1", len(logs))
		}
		if logs[0].RequestID != "filter-001" {
			t.Errorf("expected filter-001, got %s", logs[0].RequestID)
		}
	})

	t.Run("filter by app_id", func(t *testing.T) {
		aid := app2.ID
		logs, total, err := s.GetLogs(ctx, 10, 0, storage.LogsFilter{AppID: &aid})
		if err != nil {
			t.Fatalf("GetLogs: %v", err)
		}
		if total != 1 {
			t.Errorf("total: got %d, want 1", total)
		}
		if len(logs) != 1 {
			t.Errorf("len: got %d, want 1", len(logs))
		}
		if logs[0].RequestID != "filter-002" {
			t.Errorf("expected filter-002, got %s", logs[0].RequestID)
		}
	})

	t.Run("combined model + team filter", func(t *testing.T) {
		tid := team1.ID
		logs, total, err := s.GetLogs(ctx, 10, 0, storage.LogsFilter{Model: "gpt-4", TeamID: &tid})
		if err != nil {
			t.Fatalf("GetLogs: %v", err)
		}
		if total != 1 {
			t.Errorf("total: got %d, want 1", total)
		}
		if len(logs) != 1 {
			t.Errorf("len: got %d, want 1", len(logs))
		}
	})

	t.Run("filter with no matches returns empty", func(t *testing.T) {
		logs, total, err := s.GetLogs(ctx, 10, 0, storage.LogsFilter{Model: "nonexistent"})
		if err != nil {
			t.Fatalf("GetLogs: %v", err)
		}
		if total != 0 {
			t.Errorf("total: got %d, want 0", total)
		}
		if len(logs) != 0 {
			t.Errorf("len: got %d, want 0", len(logs))
		}
	})
}
