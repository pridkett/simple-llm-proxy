package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

func TestAPIKeyExpirationUpgrade(t *testing.T) {
	ctx, s, _ := newTestStorage(t)
	team, app, key := fixture(t, ctx, s)
	if team.CreatedAt.IsZero() || app.CreatedAt.IsZero() {
		t.Fatal("new team or application has no creation date")
	}
	_, err := s.DB().ExecContext(ctx, `ALTER TABLE api_keys DROP COLUMN expires_at_ms;
		DELETE FROM schema_migrations WHERE version = 8`)
	check(t, err)
	check(t, s.Initialize(ctx))
	legacy, err := s.GetAPIKeyByID(ctx, key.ID)
	check(t, err)
	now := time.Now().UTC()
	if legacy.ExpiresAt != nil || !legacy.IsUsableAt(now) {
		t.Fatalf("migration changed legacy key: %+v", legacy)
	}

	deadline := now.Add(time.Hour).Truncate(time.Millisecond)
	opts := storage.APIKeyOptions{ExpiresAt: &deadline, SetExpiration: true}
	check(t, s.UpdateAPIKey(ctx, key.ID, "Scheduled", nil, nil, nil, nil, nil, opts))
	scheduled, err := s.GetAPIKeyByHash(ctx, key.KeyHash)
	check(t, err)
	if scheduled.ExpiresAt == nil || !scheduled.ExpiresAt.Equal(deadline) || !scheduled.IsUsableAt(now) {
		t.Fatalf("invalid scheduled key: %+v", scheduled)
	}
	_, err = s.DB().ExecContext(ctx, `UPDATE api_keys SET expires_at_ms=$1 WHERE id=$2`, now.Add(-time.Hour).UnixMilli(), key.ID)
	check(t, err)
	if err := s.UpdateAPIKey(ctx, key.ID, "Reactivated", nil, nil, nil, nil, nil, opts); !errors.Is(err, storage.ErrInactiveAPIKey) {
		t.Fatalf("expired key update returned %v", err)
	}
	listed, err := s.ListAPIKeys(ctx, app.ID)
	check(t, err)
	accessible, err := s.ListUserAccessibleKeys(ctx, "subject")
	check(t, err)
	spend, err := s.GetSpendSummary(ctx, now.Add(-time.Hour), now, storage.SpendFilters{})
	check(t, err)
	if len(listed) != 1 || listed[0].IsActive || len(accessible) != 0 || len(spend) != 0 {
		t.Fatalf("unexpected expired-key state: listed=%+v accessible=%+v spend=%+v", listed, accessible, spend)
	}
}
