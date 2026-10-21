package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pwagstro/simple_llm_proxy/internal/accounting"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

func (s *Storage) PutAccounting(ctx context.Context, r *accounting.Record) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO request_accounting(id,api_key_id,request_id,state,record_json) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET record_json=excluded.record_json WHERE request_accounting.state='pending'`, r.ID, r.APIKeyID, r.RequestID, r.State, string(b))
	return err
}
func (s *Storage) GetAccounting(ctx context.Context, id string) (*accounting.Record, error) {
	var data string
	var key sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT record_json,api_key_id FROM request_accounting WHERE id=?`, id).Scan(&data, &key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r accounting.Record
	if err = json.Unmarshal([]byte(data), &r); err != nil {
		return nil, err
	}
	if key.Valid {
		r.APIKeyID = &key.Int64
	}
	return &r, nil
}
func (s *Storage) FinalizeAccounting(ctx context.Context, r *accounting.Record, entry *storage.RequestLog) (bool, error) {
	return s.finalizeAccounting(ctx, r, entry, nil)
}
func (s *Storage) FinalizeResponseAccounting(ctx context.Context, r *accounting.Record, entry *storage.RequestLog, job *storage.ResponsesJob) (bool, error) {
	return s.finalizeAccounting(ctx, r, entry, job)
}
func (s *Storage) finalizeAccounting(ctx context.Context, r *accounting.Record, entry *storage.RequestLog, job *storage.ResponsesJob) (bool, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	// A transient failure saving the initial pending row must not lose a completed charge.
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO request_accounting(id,api_key_id,request_id,state,record_json) VALUES(?,?,?,'pending',?)`, r.ID, r.APIKeyID, r.RequestID, string(b)); err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE request_accounting SET state='final',record_json=? WHERE id=? AND state='pending'`, string(b), r.ID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 0 {
		return false, err
	}
	if entry != nil {
		if err = insertRequestLog(ctx, tx, entry); err != nil {
			return false, err
		}
	}
	if job != nil {
		if _, err = tx.ExecContext(ctx, `UPDATE responses_jobs SET status=?,response_json=?,error_json=?,completed_at=?,updated_at=datetime('now') WHERE id=?`, job.Status, job.ResponseJSON, job.ErrorJSON, job.CompletedAt, job.ID); err != nil {
			return false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
