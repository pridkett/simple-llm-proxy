package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/pwagstro/simple_llm_proxy/internal/storage/sqlstore"
)

// Storage implements the storage interface using SQLite.
type Storage struct {
	*sqlstore.Storage
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

	return &Storage{Storage: sqlstore.New(db, sqlstore.SQLite), db: db}, nil
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
