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

	// Serialize all connections through a single connection. WAL mode still
	// allows readers to run concurrently at the OS level, but constraining the
	// pool to one open connection ensures write transactions queue up within Go
	// rather than racing on separate file descriptors and triggering SQLITE_BUSY.
	//
	// NOTE: This is SQLite-specific. If the storage layer is ever migrated to
	// PostgreSQL, MySQL, or another RDBMS, this limit should be removed (or
	// raised significantly) so the connection pool can handle concurrent queries
	// across multiple goroutines.
	db.SetMaxOpenConns(1)

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting journal mode: %w", err)
	}

	// Wait up to 5 s before returning SQLITE_BUSY. This is a backstop for any
	// lock contention that slips past the single-connection pool limit (e.g. an
	// external process or a second server instance sharing the same file).
	//
	// NOTE: This is a SQLite-specific PRAGMA. It has no equivalent in other
	// databases (PostgreSQL/MySQL handle lock waits differently). Remove this
	// block if migrating away from SQLite.
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting busy timeout: %w", err)
	}

	// Enable foreign key enforcement — required for ON DELETE CASCADE
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
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

// DB returns the underlying *sql.DB handle.
// Used by the SCS session store to share the same database connection.
func (s *Storage) DB() *sql.DB {
	return s.db
}

// GetLogs returns paginated request logs ordered by most recent first.
// It LEFT JOINs through api_keys -> applications -> teams to resolve names.
// The filters parameter allows optional filtering by model, team, or application.
func (s *Storage) GetLogs(ctx context.Context, limit, offset int, filters storage.LogsFilter) ([]*storage.RequestLog, int, error) {
	// Build WHERE clauses for optional filters.
	var whereClauses []string
	var whereArgs []interface{}

	if filters.Model != "" {
		whereClauses = append(whereClauses, "ul.model = ?")
		whereArgs = append(whereArgs, filters.Model)
	}
	if filters.TeamID != nil {
		whereClauses = append(whereClauses, "t.id = ?")
		whereArgs = append(whereArgs, *filters.TeamID)
	}
	if filters.AppID != nil {
		whereClauses = append(whereClauses, "app.id = ?")
		whereArgs = append(whereArgs, *filters.AppID)
	}
	if filters.Provider != "" {
		whereClauses = append(whereClauses, "ul.provider = ?")
		whereArgs = append(whereArgs, filters.Provider)
	}
	if filters.PoolName != "" {
		whereClauses = append(whereClauses, "ul.pool_name = ?")
		whereArgs = append(whereArgs, filters.PoolName)
	}
	if filters.KeyID != nil {
		whereClauses = append(whereClauses, "ak.id = ?")
		whereArgs = append(whereArgs, *filters.KeyID)
	}
	if filters.DateFrom != nil {
		whereClauses = append(whereClauses, "ul.request_time >= ?")
		whereArgs = append(whereArgs, filters.DateFrom.UTC())
	}
	if filters.DateTo != nil {
		whereClauses = append(whereClauses, "ul.request_time <= ?")
		whereArgs = append(whereArgs, filters.DateTo.UTC())
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = " WHERE " + whereClauses[0]
		for _, c := range whereClauses[1:] {
			whereSQL += " AND " + c
		}
	}

	// Count total matching rows.
	countQuery := `
		SELECT COUNT(*)
		FROM usage_logs ul
		LEFT JOIN api_keys ak ON ul.api_key_id = ak.id
		LEFT JOIN applications app ON ak.application_id = app.id
		LEFT JOIN teams t ON app.team_id = t.id` + whereSQL

	var total int
	err := s.db.QueryRowContext(ctx, countQuery, whereArgs...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting logs: %w", err)
	}

	// Fetch the page of logs with enriched fields.
	dataQuery := `
		SELECT ul.request_id, ul.model, ul.provider, ul.endpoint,
		       ul.input_tokens, ul.output_tokens, ul.total_cost,
		       ul.status_code, ul.latency_ms, ul.request_time,
		       ul.is_streaming, COALESCE(ul.deployment_key, ''),
		       ul.api_key_id,
		       COALESCE(ak.name, ''),
		       COALESCE(app.name, ''),
		       COALESCE(t.name, ''),
		       COALESCE(ul.pool_name, ''),
		       ul.ttft_ms,
		       COALESCE(ul.req_body_snippet, ''),
		       COALESCE(ul.resp_body_snippet, ''),
		       ul.cache_read_tokens,
		       ul.cache_write_tokens
		FROM usage_logs ul
		LEFT JOIN api_keys ak ON ul.api_key_id = ak.id
		LEFT JOIN applications app ON ak.application_id = app.id
		LEFT JOIN teams t ON app.team_id = t.id` + whereSQL + `
		ORDER BY ul.request_time DESC
		LIMIT ? OFFSET ?`

	dataArgs := append(whereArgs, limit, offset)
	rows, err := s.db.QueryContext(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("querying logs: %w", err)
	}
	defer rows.Close()

	var logs []*storage.RequestLog
	for rows.Next() {
		entry := &storage.RequestLog{}
		var ttftNull sql.NullInt64
		if err := rows.Scan(
			&entry.RequestID, &entry.Model, &entry.Provider, &entry.Endpoint,
			&entry.InputTokens, &entry.OutputTokens, &entry.TotalCost,
			&entry.StatusCode, &entry.LatencyMS, &entry.RequestTime,
			&entry.IsStreaming, &entry.DeploymentKey,
			&entry.APIKeyID,
			&entry.KeyName, &entry.AppName, &entry.TeamName,
			&entry.PoolName,
			&ttftNull,
			&entry.ReqBodySnippet,
			&entry.RespBodySnippet,
			&entry.CacheReadTokens,   // plain int — NOT NULL DEFAULT 0 in DB
			&entry.CacheWriteTokens,  // plain int — NOT NULL DEFAULT 0 in DB
		); err != nil {
			return nil, 0, fmt.Errorf("scanning log: %w", err)
		}
		if ttftNull.Valid {
			v := ttftNull.Int64
			entry.TTFTMs = &v
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
			request_id, api_key_id, model, provider, endpoint,
			input_tokens, output_tokens, total_cost,
			status_code, latency_ms, request_time,
			is_streaming, deployment_key,
			pool_name, ttft_ms, req_body_snippet, resp_body_snippet,
			cache_read_tokens, cache_write_tokens
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		log.RequestID,
		log.APIKeyID,
		log.Model,
		log.Provider,
		log.Endpoint,
		log.InputTokens,
		log.OutputTokens,
		log.TotalCost,
		log.StatusCode,
		log.LatencyMS,
		log.RequestTime.UTC().Round(0),
		log.IsStreaming,
		log.DeploymentKey,
		log.PoolName,
		log.TTFTMs,
		log.ReqBodySnippet,
		log.RespBodySnippet,
		log.CacheReadTokens,
		log.CacheWriteTokens,
	)
	if err != nil {
		return fmt.Errorf("inserting log: %w", err)
	}
	return nil
}
