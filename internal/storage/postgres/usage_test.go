package postgres

import (
	"github.com/pwagstro/simple_llm_proxy/internal/storage/testutil"
	"testing"
)

func TestUsageReporting(t *testing.T) {
	ctx, s, _ := newTestStorage(t)
	testutil.UsageContract(t, ctx, s)
}
