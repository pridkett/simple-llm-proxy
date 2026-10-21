// Package postgres provides PostgreSQL persistence (ADR 014).
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"github.com/pwagstro/simple_llm_proxy/internal/storage/sqlstore"
)

type Storage struct{ *sqlstore.Storage }

var _ storage.Storage = (*Storage)(nil)

func New(ctx context.Context, databaseURL string) (*Storage, error) {
	// URL schemes are case insensitive, while pgx recognizes lowercase prefixes.
	if i := strings.Index(databaseURL, "://"); i >= 0 {
		databaseURL = strings.ToLower(databaseURL[:i]) + databaseURL[i:]
	}
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		// Driver errors can include the entire DSN, including its password.
		return nil, errors.New("invalid PostgreSQL connection configuration")
	}
	db := stdlib.OpenDB(*cfg)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, errors.New("connecting to PostgreSQL failed; check availability, credentials, and TLS settings")
	}
	return &Storage{Storage: sqlstore.New(db, sqlstore.PostgreSQL)}, nil
}

func (s *Storage) Initialize(ctx context.Context) error {
	if err := s.migrate(ctx); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			return fmt.Errorf("PostgreSQL schema initialization failed (SQLSTATE %s)", pgErr.Code)
		}
		return errors.New("PostgreSQL schema initialization failed; check availability and migration permissions")
	}
	return nil
}

// SessionStore shares the storage connection pool with SCS login sessions.
func (s *Storage) SessionStore() *sqlstore.SessionStore {
	return &sqlstore.SessionStore{DB: s.DB(), Dialect: sqlstore.PostgreSQL}
}

// DB is available for operational tooling and integration tests.
func (s *Storage) DB() *sql.DB { return s.Storage.DB() }
