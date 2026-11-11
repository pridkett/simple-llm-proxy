package postgres

import (
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

func TestIssuedAPIKeyOwnershipUpgrade(t *testing.T) {
	ctx, s, _ := newTestStorage(t)
	team, app, legacy := fixture(t, ctx, s)
	_, err := s.DB().ExecContext(ctx, `ALTER TABLE api_keys DROP COLUMN issuer_revoked_at_ms;
		ALTER TABLE api_keys DROP COLUMN issuer_id;
		DELETE FROM schema_migrations WHERE version = 9`)
	check(t, err)
	check(t, s.Initialize(ctx))
	key, err := s.GetAPIKeyByID(ctx, legacy.ID)
	check(t, err)
	if key.IssuerID != "" || key.IssuerRevokedAt != nil || !key.IsActive {
		t.Fatal("migration changed legacy key ownership or state")
	}
	otherApp, err := s.CreateApplication(ctx, team.ID, "Other app")
	check(t, err)
	deadline := time.Now().Add(time.Hour)
	issued, err := s.CreateAPIKey(ctx, app.ID, "Run", "issued", "issued-hash", nil, nil, nil, nil, []string{"model"},
		storage.APIKeyOptions{ExpiresAt: &deadline, IssuerID: "framework"})
	check(t, err)
	for _, tc := range []struct {
		appID  int64
		issuer string
		keyID  int64
	}{
		{app.ID, "other", issued.ID},
		{otherApp.ID, "framework", issued.ID},
		{app.ID, "framework", legacy.ID},
		{app.ID, "", legacy.ID},
	} {
		revoked, err := s.RevokeIssuedAPIKey(ctx, tc.appID, tc.issuer, tc.keyID)
		check(t, err)
		if revoked {
			t.Fatal("issuer revoked a key outside its scope")
		}
	}
	revoked, err := s.RevokeIssuedAPIKey(ctx, app.ID, "framework", issued.ID)
	check(t, err)
	after, err := s.GetAPIKeyByHash(ctx, issued.KeyHash)
	check(t, err)
	if !revoked || after == nil || after.IsActive || after.IssuerID != "framework" || after.IssuerRevokedAt == nil {
		t.Fatal("issuer revocation was not persisted")
	}
	revoked, err = s.RevokeIssuedAPIKey(ctx, app.ID, "framework", issued.ID)
	check(t, err)
	again, err := s.GetAPIKeyByID(ctx, issued.ID)
	check(t, err)
	if !revoked || again.IssuerRevokedAt == nil || !again.IssuerRevokedAt.Equal(*after.IssuerRevokedAt) {
		t.Fatal("repeated revocation changed the audit timestamp")
	}
}
