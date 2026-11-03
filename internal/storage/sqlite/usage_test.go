package sqlite

import (
	"context"
	"github.com/pwagstro/simple_llm_proxy/internal/storage/testutil"
	"testing"
)

func TestUsageReporting(t *testing.T) {
	testutil.UsageContract(t, context.Background(), newTestStorage(t))
}
