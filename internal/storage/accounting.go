package storage

import (
	"context"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/accounting"
)

// AccountingStorage persists records and atomically finalizes their legacy usage-log projection.
// The bool reports whether finalization won; only that caller may credit in-memory budgets.
type AccountingStorage interface {
	PutAccounting(context.Context, *accounting.Record) error
	GetAccounting(context.Context, string) (*accounting.Record, error)
	FinalizeAccounting(context.Context, *accounting.Record, *RequestLog) (bool, error)
	FinalizeResponseAccounting(context.Context, *accounting.Record, *RequestLog, *ResponsesJob) (bool, error)
}

// AccountingLog is the compatibility projection used by spend queries and request logs.
// Decimal amounts remain authoritative in the durable accounting record.
func AccountingLog(rec *accounting.Record, modelName, endpoint string, started time.Time) *RequestLog {
	if len(rec.Attempts) == 0 {
		return nil
	}
	last := rec.Attempts[len(rec.Attempts)-1]
	known := rec.Cost.Amount != nil
	entry := &RequestLog{AccountingID: rec.ID, CostSource: rec.Cost.Source, CostKnown: &known, CostCoverage: rec.Coverage, RequestID: rec.RequestID, APIKeyID: rec.APIKeyID, Model: modelName, Provider: last.Provider, Endpoint: endpoint, RequestTime: started, LatencyMS: time.Since(started).Milliseconds(), StatusCode: 200, PoolName: last.Pool, DeploymentKey: last.DeploymentKey, TotalCost: accounting.Float(rec.Cost.Amount), UsageKnown: rec.Usage.InputTokens != nil && rec.Usage.OutputTokens != nil}
	if v := rec.Usage.InputTokens; v != nil {
		entry.InputTokens = int(*v)
	}
	if v := rec.Usage.OutputTokens; v != nil {
		entry.OutputTokens = int(*v)
	}
	if v := rec.Usage.InputDetails.CacheReadTokens; v != nil {
		entry.CacheReadTokens = int(*v)
	}
	if v := rec.Usage.InputDetails.CacheWriteTokens; v != nil {
		entry.CacheWriteTokens = int(*v)
	}
	return entry
}
