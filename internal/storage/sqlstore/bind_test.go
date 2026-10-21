package sqlstore

import "testing"

func TestBind(t *testing.T) {
	query := `SELECT '?', 'it''s ?', "?" FROM t WHERE a = ? AND b = ?`
	want := `SELECT '?', 'it''s ?', "?" FROM t WHERE a = $1 AND b = $2`
	if got := PostgreSQL.bind(query); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if got := SQLite.bind(query); got != query {
		t.Fatalf("SQLite query changed: %s", got)
	}
}
