// Package database selects and initializes a persistence backend from database_url.
package database

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"github.com/pwagstro/simple_llm_proxy/internal/storage/postgres"
	"github.com/pwagstro/simple_llm_proxy/internal/storage/sqlite"
	"github.com/pwagstro/simple_llm_proxy/internal/storage/sqlstore"
)

// Open returns initialized storage and its matching SCS session store.
// Empty databaseURL disables persistence. The caller controls startup deadlines.
func Open(ctx context.Context, databaseURL string) (storage.Storage, *sqlstore.SessionStore, error) {
	if databaseURL == "" {
		return nil, nil, nil
	}
	var store storage.Storage
	var sessions *sqlstore.SessionStore
	var err error
	switch Backend(databaseURL) {
	case "postgres":
		var pg *postgres.Storage
		pg, err = postgres.New(ctx, databaseURL)
		if err == nil {
			store, sessions = pg, pg.SessionStore()
		}
	case "sqlite":
		var sq *sqlite.Storage
		sq, err = sqlite.New(databaseURL)
		if err == nil {
			store, sessions = sq, &sqlstore.SessionStore{DB: sq.DB()}
		}
	default:
		return nil, nil, fmt.Errorf("unsupported database URL scheme; use a SQLite path/file URI or postgres:// or postgresql://")
	}
	if err != nil {
		return nil, nil, err
	}
	if err := store.Initialize(ctx); err != nil {
		store.Close()
		return nil, nil, fmt.Errorf("initialize database schema: %w", err)
	}
	return store, sessions, nil
}

// Backend classifies only connection forms supported by this application.
func Backend(databaseURL string) string {
	lower := strings.ToLower(databaseURL)
	if strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://") {
		return "postgres"
	}
	if strings.HasPrefix(lower, "file:") {
		return "sqlite"
	}
	// Windows drive paths are valid SQLite paths, including on cross-built binaries.
	if len(databaseURL) > 2 && databaseURL[1] == ':' && (databaseURL[2] == '/' || databaseURL[2] == '\\') {
		return "sqlite"
	}
	// Parse only to distinguish a URI scheme from colons within ordinary paths.
	u, err := url.Parse(databaseURL)
	if strings.Contains(databaseURL, "://") || (err == nil && u.Scheme != "") {
		return "unsupported"
	}
	return "sqlite"
}

// RedactedURL preserves SQLite paths but never exposes a network DSN's userinfo
// or query parameters. Invalid URLs are withheld too: parse errors may echo them.
func RedactedURL(databaseURL string) string {
	if Backend(databaseURL) == "sqlite" {
		return databaseURL
	}
	if Backend(databaseURL) == "postgres" {
		return "postgresql://[redacted]"
	}
	return "[redacted]"
}
