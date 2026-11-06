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

func TestMCPOAuthPersistence(t *testing.T) {
	ctx, s, _ := newTestStorage(t)
	check(t, s.UpsertUser(ctx, &storage.User{ID: "subject", Name: "User", Email: "user@example.test"}))
	const binding, subject = "oauth-binding", "subject"
	check(t, s.PutMCPOAuth(ctx, binding, subject, storage.MCPOAuthCredential{
		Ciphertext: []byte("encrypted"), Generation: "generation",
	}))
	check(t, s.PutMCPOAuth(ctx, binding, "", storage.MCPOAuthCredential{
		Ciphertext: []byte("shared-encrypted"), Generation: "shared-generation",
	}))
	ok, err := s.SetMCPOAuthToken(ctx, binding, subject, "hash", 100)
	check(t, err)
	if !ok {
		t.Fatal("OAuth token was not issued")
	}
	ok, err = s.RefreshMCPOAuth(ctx, binding, subject, []byte("encrypted"), []byte("new-encrypted"))
	check(t, err)
	if !ok {
		t.Fatal("OAuth grant was not refreshed")
	}
	ok, err = s.RefreshMCPOAuth(ctx, binding, subject, []byte("encrypted"), []byte("stale"))
	check(t, err)
	if ok {
		t.Fatal("stale OAuth refresh overwrote a newer grant")
	}
	check(t, s.DeleteMCPOAuth(ctx, binding, subject))
	connection, err := s.GetMCPConnectionByToken(ctx, binding, "hash", 99)
	check(t, err)
	if connection != nil {
		t.Fatal("personal gateway token survived disconnect")
	}
	shared, err := s.GetMCPOAuth(ctx, binding, "")
	check(t, err)
	if shared == nil || shared.Generation != "shared-generation" {
		t.Fatal("personal disconnect affected the shared grant")
	}
}
