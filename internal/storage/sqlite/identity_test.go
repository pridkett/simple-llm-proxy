package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// newTestStorage creates an in-memory SQLite storage for testing.
func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening in-memory db: %v", err)
	}

	// Enable foreign keys for CASCADE tests
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		t.Fatalf("enabling foreign keys: %v", err)
	}

	s := &Storage{db: db}
	ctx := context.Background()
	if err := s.Initialize(ctx); err != nil {
		db.Close()
		t.Fatalf("initializing storage: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return s
}

// TestMigrations verifies that all identity tables are created.
func TestMigrations(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	expectedTables := []string{"users", "teams", "team_members", "applications", "sessions"}
	for _, table := range expectedTables {
		var name string
		err := s.db.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		} else if name != table {
			t.Errorf("expected table %q, got %q", table, name)
		}
	}
}

// TestUpsertGetUser tests create and update behavior.
func TestUpsertGetUser(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	user := &storage.User{
		ID:      "sub123",
		Email:   "a@b.com",
		Name:    "Alice",
		IsAdmin: false,
	}
	if err := s.UpsertUser(ctx, user); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	got, err := s.GetUser(ctx, "sub123")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got == nil {
		t.Fatal("GetUser returned nil")
	}
	if got.ID != "sub123" {
		t.Errorf("ID: got %q, want %q", got.ID, "sub123")
	}
	if got.Email != "a@b.com" {
		t.Errorf("Email: got %q, want %q", got.Email, "a@b.com")
	}
	if got.Name != "Alice" {
		t.Errorf("Name: got %q, want %q", got.Name, "Alice")
	}
	if got.IsAdmin {
		t.Error("IsAdmin: expected false")
	}
	createdAt := got.CreatedAt

	// Upsert again with is_admin=true
	user.IsAdmin = true
	if err := s.UpsertUser(ctx, user); err != nil {
		t.Fatalf("UpsertUser (update): %v", err)
	}

	got2, err := s.GetUser(ctx, "sub123")
	if err != nil {
		t.Fatalf("GetUser after update: %v", err)
	}
	if !got2.IsAdmin {
		t.Error("IsAdmin after update: expected true")
	}
	if !got2.CreatedAt.Equal(createdAt) {
		t.Errorf("CreatedAt changed on upsert: before=%v after=%v", createdAt, got2.CreatedAt)
	}
}

// TestListUsers tests listing multiple users.
func TestListUsers(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	users := []*storage.User{
		{ID: "sub001", Email: "alice@example.com", Name: "Alice"},
		{ID: "sub002", Email: "bob@example.com", Name: "Bob"},
	}
	for _, u := range users {
		if err := s.UpsertUser(ctx, u); err != nil {
			t.Fatalf("UpsertUser: %v", err)
		}
	}

	list, err := s.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("ListUsers: got %d users, want 2", len(list))
	}
}

// TestTeamCRUD tests create, list, and delete for teams.
func TestTeamCRUD(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	team, err := s.CreateTeam(ctx, "Engineering")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if team.ID == 0 {
		t.Error("CreateTeam: expected non-zero ID")
	}
	if team.Name != "Engineering" {
		t.Errorf("CreateTeam: got name %q, want %q", team.Name, "Engineering")
	}

	teams, err := s.ListTeams(ctx)
	if err != nil {
		t.Fatalf("ListTeams: %v", err)
	}
	if len(teams) != 1 {
		t.Errorf("ListTeams: got %d, want 1", len(teams))
	}

	if err := s.DeleteTeam(ctx, team.ID); err != nil {
		t.Fatalf("DeleteTeam: %v", err)
	}

	teams, err = s.ListTeams(ctx)
	if err != nil {
		t.Fatalf("ListTeams after delete: %v", err)
	}
	if len(teams) != 0 {
		t.Errorf("ListTeams after delete: got %d, want 0", len(teams))
	}
}

// TestTeamMemberRole tests adding, updating role, and removing team members.
func TestTeamMemberRole(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	team, _ := s.CreateTeam(ctx, "Alpha")
	_ = s.UpsertUser(ctx, &storage.User{ID: "u1", Email: "u1@test.com", Name: "User1"})

	if err := s.AddTeamMember(ctx, team.ID, "u1", "member"); err != nil {
		t.Fatalf("AddTeamMember: %v", err)
	}

	members, err := s.ListTeamMembers(ctx, team.ID)
	if err != nil {
		t.Fatalf("ListTeamMembers: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(members))
	}
	if members[0].Role != "member" {
		t.Errorf("role: got %q, want %q", members[0].Role, "member")
	}

	if err := s.UpdateTeamMemberRole(ctx, team.ID, "u1", "admin"); err != nil {
		t.Fatalf("UpdateTeamMemberRole: %v", err)
	}

	members, _ = s.ListTeamMembers(ctx, team.ID)
	if members[0].Role != "admin" {
		t.Errorf("role after update: got %q, want %q", members[0].Role, "admin")
	}

	if err := s.RemoveTeamMember(ctx, team.ID, "u1"); err != nil {
		t.Fatalf("RemoveTeamMember: %v", err)
	}

	members, _ = s.ListTeamMembers(ctx, team.ID)
	if len(members) != 0 {
		t.Errorf("after remove: got %d members, want 0", len(members))
	}
}

// TestTeamMemberRoleConstraint tests that an invalid role is rejected.
func TestTeamMemberRoleConstraint(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	team, _ := s.CreateTeam(ctx, "Beta")
	_ = s.UpsertUser(ctx, &storage.User{ID: "u2", Email: "u2@test.com", Name: "User2"})

	err := s.AddTeamMember(ctx, team.ID, "u2", "superuser")
	if err == nil {
		t.Error("expected error for invalid role 'superuser', got nil")
	}
}

// TestTeamCascadeDeletesMembers tests ON DELETE CASCADE for team deletion.
func TestTeamCascadeDeletesMembers(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	team, _ := s.CreateTeam(ctx, "Gamma")
	_ = s.UpsertUser(ctx, &storage.User{ID: "u3", Email: "u3@test.com", Name: "User3"})
	_ = s.AddTeamMember(ctx, team.ID, "u3", "member")

	if err := s.DeleteTeam(ctx, team.ID); err != nil {
		t.Fatalf("DeleteTeam: %v", err)
	}

	var count int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM team_members WHERE team_id = ?", team.ID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("querying team_members: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 team_members after team delete, got %d", count)
	}
}

// TestUserCascadeDeletesMemberships tests ON DELETE CASCADE for user deletion.
func TestUserCascadeDeletesMemberships(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	team, _ := s.CreateTeam(ctx, "Delta")
	_ = s.UpsertUser(ctx, &storage.User{ID: "u4", Email: "u4@test.com", Name: "User4"})
	_ = s.AddTeamMember(ctx, team.ID, "u4", "viewer")

	// Delete user directly via SQL
	if _, err := s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", "u4"); err != nil {
		t.Fatalf("deleting user: %v", err)
	}

	var count int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM team_members WHERE user_id = ?", "u4",
	).Scan(&count)
	if err != nil {
		t.Fatalf("querying team_members: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 team_members after user delete, got %d", count)
	}
}

// TestListMyTeams tests listing teams for a specific user.
func TestListMyTeams(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	team1, _ := s.CreateTeam(ctx, "Team One")
	team2, _ := s.CreateTeam(ctx, "Team Two")
	_ = s.UpsertUser(ctx, &storage.User{ID: "u5", Email: "u5@test.com", Name: "User5"})
	_ = s.AddTeamMember(ctx, team1.ID, "u5", "admin")
	_ = s.AddTeamMember(ctx, team2.ID, "u5", "viewer")

	myTeams, err := s.ListMyTeams(ctx, "u5")
	if err != nil {
		t.Fatalf("ListMyTeams: %v", err)
	}
	if len(myTeams) != 2 {
		t.Errorf("ListMyTeams: got %d, want 2", len(myTeams))
	}
}

// TestApplicationCRUD tests creating, listing, and deleting applications.
func TestApplicationCRUD(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	team, _ := s.CreateTeam(ctx, "AppTeam")

	app, err := s.CreateApplication(ctx, team.ID, "MyApp")
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	if app.ID == 0 {
		t.Error("CreateApplication: expected non-zero ID")
	}
	if app.Name != "MyApp" {
		t.Errorf("CreateApplication: got name %q, want %q", app.Name, "MyApp")
	}

	apps, err := s.ListApplications(ctx, team.ID)
	if err != nil {
		t.Fatalf("ListApplications: %v", err)
	}
	if len(apps) != 1 {
		t.Errorf("ListApplications: got %d, want 1", len(apps))
	}

	if err := s.DeleteApplication(ctx, app.ID); err != nil {
		t.Fatalf("DeleteApplication: %v", err)
	}

	apps, _ = s.ListApplications(ctx, team.ID)
	if len(apps) != 0 {
		t.Errorf("ListApplications after delete: got %d, want 0", len(apps))
	}
}

// TestApplicationCascadeOnTeamDelete tests that deleting a team removes its applications.
func TestApplicationCascadeOnTeamDelete(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	team, _ := s.CreateTeam(ctx, "CascadeTeam")
	_, err := s.CreateApplication(ctx, team.ID, "ToBeGone")
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}

	if err := s.DeleteTeam(ctx, team.ID); err != nil {
		t.Fatalf("DeleteTeam: %v", err)
	}

	var count int
	err = s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM applications WHERE team_id = ?", team.ID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("querying applications: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 applications after team delete, got %d", count)
	}
}

// TestSessionStore tests the basic session store operations.
func TestSessionStore(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	store := &SessionStore{DB: s.db}
	expiry := time.Now().Add(1 * time.Hour)

	if err := store.CommitCtx(ctx, "tok1", []byte("payload"), expiry); err != nil {
		t.Fatalf("CommitCtx: %v", err)
	}

	data, found, err := store.FindCtx(ctx, "tok1")
	if err != nil {
		t.Fatalf("FindCtx: %v", err)
	}
	if !found {
		t.Error("FindCtx: expected found=true")
	}
	if string(data) != "payload" {
		t.Errorf("FindCtx: got data %q, want %q", string(data), "payload")
	}

	if err := store.DeleteCtx(ctx, "tok1"); err != nil {
		t.Fatalf("DeleteCtx: %v", err)
	}

	_, found, err = store.FindCtx(ctx, "tok1")
	if err != nil {
		t.Fatalf("FindCtx after delete: %v", err)
	}
	if found {
		t.Error("FindCtx after delete: expected found=false")
	}
}

// TestSessionStoreExpiry tests that expired sessions are not returned.
func TestSessionStoreExpiry(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	store := &SessionStore{DB: s.db}
	expiry := time.Now().Add(-1 * time.Second)

	if err := store.CommitCtx(ctx, "tok2", []byte("expired"), expiry); err != nil {
		t.Fatalf("CommitCtx: %v", err)
	}

	_, found, err := store.FindCtx(ctx, "tok2")
	if err != nil {
		t.Fatalf("FindCtx: %v", err)
	}
	if found {
		t.Error("FindCtx: expected found=false for expired session")
	}
}

// TestCleanExpiredSessions tests that CleanExpiredSessions removes expired sessions.
func TestCleanExpiredSessions(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	store := &SessionStore{DB: s.db}
	expiry := time.Now().Add(-1 * time.Second)

	if err := store.CommitCtx(ctx, "tok3", []byte("stale"), expiry); err != nil {
		t.Fatalf("CommitCtx: %v", err)
	}

	if err := s.CleanExpiredSessions(ctx); err != nil {
		t.Fatalf("CleanExpiredSessions: %v", err)
	}

	_, found, err := store.FindCtx(ctx, "tok3")
	if err != nil {
		t.Fatalf("FindCtx: %v", err)
	}
	if found {
		t.Error("FindCtx after clean: expected found=false")
	}

	var count int
	err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions").Scan(&count)
	if err != nil {
		t.Fatalf("counting sessions: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 sessions after clean, got %d", count)
	}
}
