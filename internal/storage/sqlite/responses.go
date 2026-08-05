package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// CreateResponsesJob inserts a new background job row.
func (s *Storage) CreateResponsesJob(ctx context.Context, job *storage.ResponsesJob) error {
	var poolName any
	if job.PoolName != "" {
		poolName = job.PoolName
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO responses_jobs (id, api_key_id, deployment_key, model_name, pool_name, status, request_json, response_json, error_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.APIKeyID, job.DeploymentKey, job.ModelName, poolName, job.Status, job.RequestJSON, job.ResponseJSON, job.ErrorJSON,
	)
	if err != nil {
		return fmt.Errorf("creating responses job %s: %w", job.ID, err)
	}
	return nil
}

// GetResponsesJob returns the job with the given id, or (nil, nil) if not found.
func (s *Storage) GetResponsesJob(ctx context.Context, id string) (*storage.ResponsesJob, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, api_key_id, deployment_key, model_name, COALESCE(pool_name, ''), status, request_json, response_json, error_json, created_at, updated_at, completed_at
		 FROM responses_jobs WHERE id = ?`, id)

	job, err := scanResponsesJob(row)
	if err != nil {
		return nil, err
	}
	return job, nil
}

// UpdateResponsesJob updates a job's status, response, and error state.
func (s *Storage) UpdateResponsesJob(ctx context.Context, id, status string, responseJSON, errorJSON *string, completedAt *time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE responses_jobs
		 SET status = ?, response_json = ?, error_json = ?, updated_at = datetime('now'), completed_at = ?
		 WHERE id = ?`,
		status, responseJSON, errorJSON, completedAt, id,
	)
	if err != nil {
		return fmt.Errorf("updating responses job %s: %w", id, err)
	}
	return nil
}

// ListPendingResponsesJobs returns all jobs not yet in a terminal status.
func (s *Storage) ListPendingResponsesJobs(ctx context.Context) ([]*storage.ResponsesJob, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, api_key_id, deployment_key, model_name, COALESCE(pool_name, ''), status, request_json, response_json, error_json, created_at, updated_at, completed_at
		 FROM responses_jobs
		 WHERE status NOT IN ('completed', 'failed', 'cancelled', 'incomplete')
		 ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("listing pending responses jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*storage.ResponsesJob
	for rows.Next() {
		job, err := scanResponsesJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating pending responses jobs: %w", err)
	}
	return jobs, nil
}

// rowScanner abstracts *sql.Row and *sql.Rows for shared scan logic.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanResponsesJob(row rowScanner) (*storage.ResponsesJob, error) {
	job := &storage.ResponsesJob{}
	err := row.Scan(
		&job.ID, &job.APIKeyID, &job.DeploymentKey, &job.ModelName, &job.PoolName, &job.Status,
		&job.RequestJSON, &job.ResponseJSON, &job.ErrorJSON,
		&job.CreatedAt, &job.UpdatedAt, &job.CompletedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scanning responses job: %w", err)
	}
	return job, nil
}
