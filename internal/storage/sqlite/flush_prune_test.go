package sqlite

import (
	"context"
	"fmt"
	"testing"
)

func TestFlushPruneMigrationKeepsLatestSnapshotPerKey(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	for _, q := range []string{
		`PRAGMA foreign_keys = OFF`,
		`DELETE FROM schema_migrations WHERE version = 66`,
		`INSERT INTO usage_logs (request_id,model,provider,endpoint,status_code,latency_ms,request_time) VALUES ('real','m','p','/v1/chat/completions',200,10,datetime('now'))`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	for key := 1; key <= 2; key++ {
		for i := 0; i < 5; i++ {
			if _, err := s.db.ExecContext(ctx, `INSERT INTO usage_logs (api_key_id,total_cost,model,provider,endpoint,request_time,request_id,status_code,latency_ms) VALUES (?, ?, '_flush','_flush','_flush',CURRENT_TIMESTAMP, ?, 0, 0)`, key, float64(i), fmt.Sprintf("flush-%d-%d", key, i)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT api_key_id, total_cost FROM usage_logs WHERE model = '_flush' ORDER BY api_key_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var kept []string
	for rows.Next() {
		var key int64
		var cost float64
		if err := rows.Scan(&key, &cost); err != nil {
			t.Fatal(err)
		}
		kept = append(kept, fmt.Sprintf("%d:%g", key, cost))
	}
	if fmt.Sprint(kept) != "[1:4 2:4]" {
		t.Fatalf("kept snapshots = %v, want the newest per key", kept)
	}
	if real, err := s.GetLogByID(ctx, "real"); err != nil || real == nil {
		t.Fatalf("real request log removed: %v %v", real, err)
	}
}
