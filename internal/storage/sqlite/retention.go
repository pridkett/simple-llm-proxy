package sqlite

import (
	"context"
	"fmt"
	"time"
)

// DeleteOldRequestLogs removes usage_logs rows with request_time older than cutoff in
// batches of 1,000 to avoid holding the SQLite write lock across a large delete.
// Returns the total number of rows deleted across all batches.
func (s *Storage) DeleteOldRequestLogs(ctx context.Context, cutoff time.Time) (int64, error) {
	var total int64
	for {
		result, err := s.db.ExecContext(ctx,
			`DELETE FROM usage_logs WHERE id IN (
				SELECT id FROM usage_logs WHERE request_time < ? LIMIT 1000
			)`,
			cutoff.UTC(),
		)
		if err != nil {
			return total, fmt.Errorf("deleting old request logs: %w", err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return total, fmt.Errorf("getting rows affected: %w", err)
		}
		total += n
		if n == 0 {
			break
		}
	}
	return total, nil
}
