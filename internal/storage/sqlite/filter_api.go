package sqlite

import (
	"context"
	"fmt"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// GetLogByID returns the full RequestLog for the given request_id.
// Returns (nil, nil) when no log exists with that request_id.
// TODO(phase16-02): implement full SQLite query.
func (s *Storage) GetLogByID(_ context.Context, _ string) (*storage.RequestLog, error) {
	return nil, fmt.Errorf("GetLogByID: not yet implemented (phase 16-02)")
}

// GetLogsMeta returns distinct values for all filter dimensions.
// Used to populate dashboard filter dropdowns.
// TODO(phase16-02): implement full SQLite query.
func (s *Storage) GetLogsMeta(_ context.Context) (*storage.LogsMeta, error) {
	return nil, fmt.Errorf("GetLogsMeta: not yet implemented (phase 16-02)")
}
