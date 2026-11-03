package storage

import (
	"context"
	"time"
)

// UsageStorage supplies aggregate reporting without changing the legacy spend API.
type UsageStorage interface {
	GetUsage(context.Context, UsageQuery) ([]UsageAggregate, error)
}

type UsageQuery struct {
	From, To     time.Time
	GroupBy      string
	Filters      LogsFilter
	Unattributed string
	Buckets      []UsageBucket
}

type UsageBucket struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Label string    `json:"label"`
}

// Measurements are independent known subtotals. Counts describe their coverage;
// zero with a zero known-count is unknown, rather than measured zero.
type UsageMetrics struct {
	Calls                int64   `json:"calls"`
	FailedCalls          int64   `json:"failed_calls"`
	InputTokens          int64   `json:"input_tokens"`
	OutputTokens         int64   `json:"output_tokens"`
	TotalTokens          int64   `json:"total_tokens"`
	CacheReadTokens      int64   `json:"cache_read_tokens"`
	CacheWriteTokens     int64   `json:"cache_write_tokens"`
	ReasoningTokens      int64   `json:"reasoning_tokens"`
	KnownInputCalls      int64   `json:"known_input_calls"`
	KnownOutputCalls     int64   `json:"known_output_calls"`
	KnownTokenCalls      int64   `json:"known_token_calls"`
	KnownCacheReadCalls  int64   `json:"known_cache_read_calls"`
	KnownCacheWriteCalls int64   `json:"known_cache_write_calls"`
	KnownReasoningCalls  int64   `json:"known_reasoning_calls"`
	KnownCostCalls       int64   `json:"known_cost_calls"`
	PartialCostCalls     int64   `json:"partial_cost_calls"`
	TotalSpend           float64 `json:"total_spend"`
	UpstreamCostCalls    int64   `json:"upstream_cost_calls"`
	EstimatedCostCalls   int64   `json:"estimated_cost_calls"`
	MixedCostCalls       int64   `json:"mixed_cost_calls"`
	LegacyCalls          int64   `json:"legacy_calls"`
}

func (m *UsageMetrics) Add(v UsageMetrics) {
	m.Calls += v.Calls
	m.FailedCalls += v.FailedCalls
	m.InputTokens += v.InputTokens
	m.OutputTokens += v.OutputTokens
	m.TotalTokens += v.TotalTokens
	m.CacheReadTokens += v.CacheReadTokens
	m.CacheWriteTokens += v.CacheWriteTokens
	m.ReasoningTokens += v.ReasoningTokens
	m.KnownInputCalls += v.KnownInputCalls
	m.KnownOutputCalls += v.KnownOutputCalls
	m.KnownTokenCalls += v.KnownTokenCalls
	m.KnownCacheReadCalls += v.KnownCacheReadCalls
	m.KnownCacheWriteCalls += v.KnownCacheWriteCalls
	m.KnownReasoningCalls += v.KnownReasoningCalls
	m.KnownCostCalls += v.KnownCostCalls
	m.PartialCostCalls += v.PartialCostCalls
	m.TotalSpend += v.TotalSpend
	m.UpstreamCostCalls += v.UpstreamCostCalls
	m.EstimatedCostCalls += v.EstimatedCostCalls
	m.MixedCostCalls += v.MixedCostCalls
	m.LegacyCalls += v.LegacyCalls
}

type UsageAggregate struct {
	Bucket  int    `json:"bucket"`
	GroupID string `json:"group_id"`
	Name    string `json:"name"`
	UsageMetrics
}
