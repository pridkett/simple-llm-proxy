package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// UpsertUser creates or updates a user. user.ID must be the OIDC sub claim.
// On update, created_at is preserved and last_seen is refreshed.
func (s *Storage) UpsertUser(ctx context.Context, user *storage.User) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, email, name, is_admin, last_seen)
		VALUES (?, ?, ?, ?, datetime('now'))
		ON CONFLICT(id) DO UPDATE SET
			email    = excluded.email,
			name     = excluded.name,
			is_admin = excluded.is_admin,
			last_seen = datetime('now')
	`, user.ID, user.Email, user.Name, user.IsAdmin)
	if err != nil {
		return fmt.Errorf("upsert user: %w", err)
	}
	return nil
}

// GetUser returns the user with the given id (OIDC sub claim).
// Returns (nil, nil) if no user exists with that id.
func (s *Storage) GetUser(ctx context.Context, id string) (*storage.User, error) {
	u := &storage.User{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, email, name, is_admin, created_at, last_seen
		FROM users
		WHERE id = ?
	`, id).Scan(&u.ID, &u.Email, &u.Name, &u.IsAdmin, &u.CreatedAt, &u.LastSeen)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

// ListUsers returns all users ordered by email.
func (s *Storage) ListUsers(ctx context.Context) ([]*storage.User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, email, name, is_admin, created_at, last_seen
		FROM users
		ORDER BY email
	`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []*storage.User
	for rows.Next() {
		u := &storage.User{}
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.IsAdmin, &u.CreatedAt, &u.LastSeen); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating users: %w", err)
	}
	return users, nil
}

// CreateTeam creates a new team with the given name.
func (s *Storage) CreateTeam(ctx context.Context, name string) (*storage.Team, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO teams (name) VALUES (?)
	`, name)
	if err != nil {
		return nil, fmt.Errorf("create team: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("get team id: %w", err)
	}
	return &storage.Team{ID: id, Name: name}, nil
}

// DeleteTeam deletes the team with the given id.
// ON DELETE CASCADE (with PRAGMA foreign_keys=ON) removes team_members and applications.
func (s *Storage) DeleteTeam(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM teams WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete team: %w", err)
	}
	return nil
}

// ListTeams returns all teams ordered by name.
func (s *Storage) ListTeams(ctx context.Context) ([]*storage.Team, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, created_at FROM teams ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	defer rows.Close()

	var teams []*storage.Team
	for rows.Next() {
		t := &storage.Team{}
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan team: %w", err)
		}
		teams = append(teams, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating teams: %w", err)
	}
	return teams, nil
}

// AddTeamMember adds a user to a team with the given role.
// The DB CHECK constraint rejects roles outside ('admin','member','viewer').
func (s *Storage) AddTeamMember(ctx context.Context, teamID int64, userID string, role string) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, ?)
	`, teamID, userID, role); err != nil {
		return fmt.Errorf("add team member: %w", err)
	}
	return nil
}

// RemoveTeamMember removes a user from a team.
func (s *Storage) RemoveTeamMember(ctx context.Context, teamID int64, userID string) error {
	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM team_members WHERE team_id = ? AND user_id = ?
	`, teamID, userID); err != nil {
		return fmt.Errorf("remove team member: %w", err)
	}
	return nil
}

// UpdateTeamMemberRole updates the role of a team member.
func (s *Storage) UpdateTeamMemberRole(ctx context.Context, teamID int64, userID string, role string) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE team_members SET role = ? WHERE team_id = ? AND user_id = ?
	`, role, teamID, userID); err != nil {
		return fmt.Errorf("update team member role: %w", err)
	}
	return nil
}

// ListTeamMembers returns all members of a team, joined with user info.
func (s *Storage) ListTeamMembers(ctx context.Context, teamID int64) ([]*storage.TeamMember, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT tm.team_id, tm.user_id, tm.role, u.email, u.name
		FROM team_members tm
		JOIN users u ON u.id = tm.user_id
		WHERE tm.team_id = ?
	`, teamID)
	if err != nil {
		return nil, fmt.Errorf("list team members: %w", err)
	}
	defer rows.Close()

	var members []*storage.TeamMember
	for rows.Next() {
		m := &storage.TeamMember{}
		if err := rows.Scan(&m.TeamID, &m.UserID, &m.Role, &m.UserEmail, &m.UserName); err != nil {
			return nil, fmt.Errorf("scan team member: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating team members: %w", err)
	}
	return members, nil
}

// ListMyTeams returns all teams the given user belongs to, joined with team info.
func (s *Storage) ListMyTeams(ctx context.Context, userID string) ([]*storage.TeamMember, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT tm.team_id, tm.user_id, tm.role, t.name
		FROM team_members tm
		JOIN teams t ON t.id = tm.team_id
		WHERE tm.user_id = ?
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list my teams: %w", err)
	}
	defer rows.Close()

	var memberships []*storage.TeamMember
	for rows.Next() {
		m := &storage.TeamMember{}
		if err := rows.Scan(&m.TeamID, &m.UserID, &m.Role, &m.TeamName); err != nil {
			return nil, fmt.Errorf("scan team membership: %w", err)
		}
		memberships = append(memberships, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating team memberships: %w", err)
	}
	return memberships, nil
}

// CreateApplication creates a new application scoped to the given team.
func (s *Storage) CreateApplication(ctx context.Context, teamID int64, name string) (*storage.Application, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO applications (team_id, name) VALUES (?, ?)
	`, teamID, name)
	if err != nil {
		return nil, fmt.Errorf("create application: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("get application id: %w", err)
	}
	return &storage.Application{ID: id, TeamID: teamID, Name: name}, nil
}

// DeleteApplication deletes the application with the given id.
func (s *Storage) DeleteApplication(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM applications WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete application: %w", err)
	}
	return nil
}

// ListApplications returns all applications for the given team, ordered by name.
func (s *Storage) ListApplications(ctx context.Context, teamID int64) ([]*storage.Application, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, team_id, name, created_at
		FROM applications
		WHERE team_id = ?
		ORDER BY name
	`, teamID)
	if err != nil {
		return nil, fmt.Errorf("list applications: %w", err)
	}
	defer rows.Close()

	var apps []*storage.Application
	for rows.Next() {
		a := &storage.Application{}
		if err := rows.Scan(&a.ID, &a.TeamID, &a.Name, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan application: %w", err)
		}
		apps = append(apps, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating applications: %w", err)
	}
	return apps, nil
}

// CleanExpiredSessions removes all sessions whose expiry time has passed.
func (s *Storage) CleanExpiredSessions(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expiry <= datetime('now')`); err != nil {
		return fmt.Errorf("clean expired sessions: %w", err)
	}
	return nil
}
