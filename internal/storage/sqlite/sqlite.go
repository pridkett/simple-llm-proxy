package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// Storage implements the storage interface using SQLite.
type Storage struct {
	db *sql.DB
}

// New creates a new SQLite storage.
func New(dbPath string) (*Storage, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting journal mode: %w", err)
	}

	return &Storage{db: db}, nil
}

// Initialize creates the database schema.
func (s *Storage) Initialize(ctx context.Context) error {
	return s.migrate(ctx)
}

// Close closes the database connection.
func (s *Storage) Close() error {
	return s.db.Close()
}

// GetLogs returns paginated request logs ordered by most recent first.
func (s *Storage) GetLogs(ctx context.Context, limit, offset int) ([]*storage.RequestLog, int, error) {
	var total int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_logs").Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting logs: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT request_id, model, provider, endpoint,
		       prompt_tokens, completion_tokens, total_cost,
		       status_code, latency_ms, request_time
		FROM usage_logs
		ORDER BY request_time DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("querying logs: %w", err)
	}
	defer rows.Close()

	var logs []*storage.RequestLog
	for rows.Next() {
		entry := &storage.RequestLog{}
		if err := rows.Scan(
			&entry.RequestID, &entry.Model, &entry.Provider, &entry.Endpoint,
			&entry.PromptTokens, &entry.CompletionTokens, &entry.TotalCost,
			&entry.StatusCode, &entry.LatencyMS, &entry.RequestTime,
		); err != nil {
			return nil, 0, fmt.Errorf("scanning log: %w", err)
		}
		logs = append(logs, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterating logs: %w", err)
	}

	return logs, total, nil
}

// LogRequest logs a request to the usage_logs table.
func (s *Storage) LogRequest(ctx context.Context, log *storage.RequestLog) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO usage_logs (
			request_id, model, provider, endpoint,
			prompt_tokens, completion_tokens, total_cost,
			status_code, latency_ms, request_time
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		log.RequestID,
		log.Model,
		log.Provider,
		log.Endpoint,
		log.PromptTokens,
		log.CompletionTokens,
		log.TotalCost,
		log.StatusCode,
		log.LatencyMS,
		log.RequestTime,
	)
	if err != nil {
		return fmt.Errorf("inserting log: %w", err)
	}
	return nil
}
