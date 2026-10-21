package sqlite

import "github.com/pwagstro/simple_llm_proxy/internal/storage/sqlstore"

// SessionStore persists SQLite login sessions without CGO.
type SessionStore = sqlstore.SessionStore
