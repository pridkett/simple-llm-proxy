package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// logDetailEntry is a superset of logEntry with all telemetry fields.
// Fields are copied explicitly (not embedded) to avoid JSON serialization ambiguity.
type logDetailEntry struct {
	// --- logEntry fields (verbatim copy — do not embed) ---
	RequestID     string    `json:"request_id"`
	Model         string    `json:"model"`
	Provider      string    `json:"provider"`
	Endpoint      string    `json:"endpoint"`
	InputTokens   int       `json:"prompt_tokens"`
	OutputTokens  int       `json:"completion_tokens"`
	TotalTokens   int       `json:"total_tokens"`
	TotalCost     float64   `json:"total_cost"`
	StatusCode    int       `json:"status_code"`
	LatencyMS     int64     `json:"latency_ms"`
	RequestTime   time.Time `json:"request_time"`
	IsStreaming   bool      `json:"is_streaming"`
	DeploymentKey string    `json:"deployment_key"`
	APIKeyID      *int64    `json:"api_key_id"`
	KeyName       string    `json:"key_name"`
	AppName       string    `json:"app_name"`
	TeamName      string    `json:"team_name"`
	// --- Additional telemetry fields (superset; not in logEntry) ---
	PoolName         string  `json:"pool_name"`
	TTFTMs           *int64  `json:"ttft_ms"`
	ReqBodySnippet   *string `json:"req_body_snippet"`
	RespBodySnippet  string  `json:"resp_body_snippet"`
	CacheReadTokens  int     `json:"cache_read_tokens"`
	CacheWriteTokens int     `json:"cache_write_tokens"`
}

// AdminLogDetail handles GET /admin/logs/{requestID}.
// Returns full log detail including body snippets and telemetry fields for a single row.
// Returns 404 when no log exists for the given request_id (per D-04).
func AdminLogDetail(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		requestID := chi.URLParam(req, "requestID")
		if requestID == "" {
			model.WriteError(w, model.ErrBadRequest("request id required"))
			return
		}
		log, err := store.GetLogByID(req.Context(), requestID)
		if err != nil {
			model.WriteError(w, model.ErrInternalServer("failed to fetch log", err))
			return
		}
		if log == nil {
			model.WriteError(w, model.ErrNotFound("log not found"))
			return
		}
		entry := logDetailEntry{
			RequestID:        log.RequestID,
			Model:            log.Model,
			Provider:         log.Provider,
			Endpoint:         log.Endpoint,
			InputTokens:      log.InputTokens,
			OutputTokens:     log.OutputTokens,
			TotalTokens:      log.InputTokens + log.OutputTokens,
			TotalCost:        log.TotalCost,
			StatusCode:       log.StatusCode,
			LatencyMS:        log.LatencyMS,
			RequestTime:      log.RequestTime,
			IsStreaming:      log.IsStreaming,
			DeploymentKey:    log.DeploymentKey,
			APIKeyID:         log.APIKeyID,
			KeyName:          log.KeyName,
			AppName:          log.AppName,
			TeamName:         log.TeamName,
			PoolName:         log.PoolName,
			TTFTMs:           log.TTFTMs,
			ReqBodySnippet:   log.ReqBodySnippet,
			RespBodySnippet:  log.RespBodySnippet,
			CacheReadTokens:  log.CacheReadTokens,
			CacheWriteTokens: log.CacheWriteTokens,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entry)
	}
}

// AdminLogsMeta handles GET /admin/logs/meta.
// Returns distinct filter dimension values for populating frontend dropdowns (per D-05, D-06, D-07).
// ID-bearing dimensions (teams, apps, keys) return []LogsMetaIDName; string dims return []string.
func AdminLogsMeta(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if store == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(&storage.LogsMeta{
				Teams:     []storage.LogsMetaIDName{},
				Apps:      []storage.LogsMetaIDName{},
				Keys:      []storage.LogsMetaIDName{},
				Providers: []string{},
				Models:    []string{},
				Pools:     []string{},
			})
			return
		}
		meta, err := store.GetLogsMeta(req.Context())
		if err != nil {
			model.WriteError(w, model.ErrInternalServer("failed to fetch log meta", err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(meta)
	}
}
