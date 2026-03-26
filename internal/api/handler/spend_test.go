package handler

import (
	"testing"
)

func TestAdminSpend(t *testing.T) {
	t.Run("returns 200 with aggregated spend rows for default 7d range", func(t *testing.T) {
		t.Skip("implement in Plan 2")
	})
	t.Run("returns pre-computed alerts for keys over soft budget", func(t *testing.T) {
		t.Skip("implement in Plan 2")
	})
	t.Run("returns 400 for malformed date params", func(t *testing.T) {
		t.Skip("implement in Plan 2")
	})
	t.Run("filters by team_id query param", func(t *testing.T) {
		t.Skip("implement in Plan 2")
	})
	// Auth rejection tests (HIGH priority — Plan 2 will implement these)
	// /admin/spend exposes deployment-wide spend; non-admin access must be explicitly rejected
	t.Run("non-admin session returns 403", func(t *testing.T) {
		t.Skip("implement in Plan 2")
	})
	t.Run("unauthenticated request returns 401", func(t *testing.T) {
		t.Skip("implement in Plan 2")
	})
	// Date boundary semantics (MEDIUM priority — Plan 2 will implement)
	t.Run("to date is inclusive — row at 23:59 on to date is included", func(t *testing.T) {
		t.Skip("implement in Plan 2")
	})
	t.Run("team_id=0 is treated as no filter (nil)", func(t *testing.T) {
		t.Skip("implement in Plan 2")
	})
}
