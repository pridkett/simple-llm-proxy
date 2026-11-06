package postgres

import (
	"bytes"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

func TestMCPConnectionPersistence(t *testing.T) {
	ctx, s, _ := newTestStorage(t)
	check(t, s.UpsertUser(ctx, &storage.User{ID: "subject", Name: "User", Email: "user@example.test"}))
	const server, subject = "binding", "subject"
	ok, err := s.SetMCPToken(ctx, server, subject, "hash", 100, true)
	check(t, err)
	if ok {
		t.Fatal("issued a personal token before saving the key")
	}
	ciphertext := []byte{1, 2, 3}
	check(t, s.SetMCPKey(ctx, server, subject, ciphertext))
	ok, err = s.SetMCPToken(ctx, server, subject, "hash", 100, true)
	check(t, err)
	if !ok {
		t.Fatal("did not issue token")
	}
	connection, err := s.GetMCPConnectionByToken(ctx, server, "hash", 99)
	check(t, err)
	if connection == nil || !bytes.Equal(connection.Ciphertext, ciphertext) || connection.UserID != subject {
		t.Fatalf("unexpected connection: %#v", connection)
	}
	connection, err = s.GetMCPConnectionByToken(ctx, server, "hash", 100)
	check(t, err)
	if connection != nil {
		t.Fatal("expired token still active")
	}
	check(t, s.DeleteMCPConnection(ctx, server, subject))
	connection, err = s.GetMCPConnectionByToken(ctx, server, "hash", 99)
	check(t, err)
	if connection != nil {
		t.Fatal("deleted connection still has a token")
	}
}
