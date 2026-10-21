package database

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestBackend(t *testing.T) {
	for input, want := range map[string]string{
		"": "sqlite", "./proxy.db": "sqlite", "/data/proxy.db": "sqlite",
		":memory:": "sqlite", "file:proxy.db?mode=rwc": "sqlite",
		"./data/with:colon.db": "sqlite", "/data/with:colon.db": "sqlite",
		`C:\data\proxy.db`: "sqlite", "C:/data/proxy.db": "sqlite",
		"postgres://user:secret@host/db": "postgres", "postgresql://host/db": "postgres",
		"mysql://host/db": "unsupported", "postgres:host/db": "unsupported",
		"mysql://host%invalid/db": "unsupported",
	} {
		if got := Backend(input); got != want {
			t.Errorf("Backend(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestOpenPostgreSQLWithSessions(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("set TEST_POSTGRES_URL for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("connecting to test database failed")
	}
	defer admin.Close(ctx)
	schema := fmt.Sprintf("proxy_factory_test_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid TEST_POSTGRES_URL")
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	// Backend selection and pgx must agree on case-insensitive URI schemes.
	u.Scheme = "PostgreSQL"
	store, sessions, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if sessions == nil {
		t.Fatal("missing PostgreSQL sessions")
	}
	if err := sessions.Commit("token", []byte{0, 255}, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	data, found, err := sessions.Find("token")
	if err != nil || !found || len(data) != 2 || data[1] != 255 {
		t.Fatalf("PostgreSQL session wiring failed: %v %v", found, err)
	}
}

func TestOpenSQLiteAndDisabled(t *testing.T) {
	ctx := context.Background()
	store, sessions, err := Open(ctx, "")
	if err != nil || store != nil || sessions != nil {
		t.Fatalf("disabled: %v %v %v", store, sessions, err)
	}
	for _, url := range []string{":memory:", filepath.Join(t.TempDir(), "proxy.db"), "file:" + filepath.Join(t.TempDir(), "uri.db")} {
		store, sessions, err = Open(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		if sessions == nil {
			t.Fatal("missing session store")
		}
		if err := sessions.Commit("token", []byte("data"), time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := sessions.Find("token"); err != nil || !ok {
			t.Fatalf("session: %v %v", ok, err)
		}
		store.Close()
	}
}

func TestOpenFailureAndRedaction(t *testing.T) {
	for _, url := range []string{
		"mysql://private-user:secret-password@host/db?password=secret-query",
		"postgres://private-user:secret-password@127.0.0.1:1/db?sslmode=disable&connect_timeout=1",
		"postgresql://private-user:secret-password@host/db?sslmode=invalid-secret-query",
	} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		store, sessions, err := Open(ctx, url)
		cancel()
		if err == nil || store != nil || sessions != nil {
			t.Fatalf("expected clean failure, got %v %v %v", store, sessions, err)
		}
		for _, secret := range []string{"private-user", "secret-password", "secret-query"} {
			if strings.Contains(err.Error(), secret) || strings.Contains(RedactedURL(url), secret) {
				t.Fatalf("exposed credentials: %v", err)
			}
		}
	}
	if got := RedactedURL("./proxy.db"); got != "./proxy.db" {
		t.Fatalf("SQLite path changed: %s", got)
	}
}
