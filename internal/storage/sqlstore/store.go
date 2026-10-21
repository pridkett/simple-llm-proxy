// Package sqlstore contains the shared SQLite and PostgreSQL persistence queries.
// Backend packages own connections and schema migrations.
package sqlstore

import "database/sql"

type Dialect int

const (
	SQLite Dialect = iota
	PostgreSQL
)

type Storage struct {
	db      *sql.DB
	dialect Dialect
}

func New(db *sql.DB, dialect Dialect) *Storage {
	return &Storage{db: db, dialect: dialect}
}

func (s *Storage) DB() *sql.DB  { return s.db }
func (s *Storage) Close() error { return s.db.Close() }

func (s *Storage) bind(query string) string      { return s.dialect.bind(query) }
func (s *SessionStore) bind(query string) string { return s.Dialect.bind(query) }
