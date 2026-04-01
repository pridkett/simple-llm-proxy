package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

type adminStatusResponse struct {
	Status         string                   `json:"status"`
	UptimeSeconds  int64                    `json:"uptime_seconds"`
	Models         []router.ModelStatusInfo `json:"models"`
	Pools          []router.PoolStatusInfo  `json:"pools"`
	RouterSettings routerSettingsJSON       `json:"router_settings"`
}

// routerSettingsJSON is a JSON-friendly version of RouterSettings.
type routerSettingsJSON struct {
	RoutingStrategy string `json:"routing_strategy"`
	NumRetries      int    `json:"num_retries"`
	AllowedFails    int    `json:"allowed_fails"`
	CooldownTime    string `json:"cooldown_time"`
}

// AdminStatus handles GET /admin/status.
func AdminStatus(r *router.Router, startTime time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		settings := r.Settings()
		resp := adminStatusResponse{
			Status:        "healthy",
			UptimeSeconds: int64(time.Since(startTime).Seconds()),
			Models:        r.GetStatus(),
			Pools:         r.GetPoolStatus(),
			RouterSettings: routerSettingsJSON{
				RoutingStrategy: settings.RoutingStrategy,
				NumRetries:      settings.NumRetries,
				AllowedFails:    settings.AllowedFails,
				CooldownTime:    settings.CooldownTime.String(),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// configModelEntry is a sanitized model config for the API response.
type configModelEntry struct {
	ModelName   string `json:"model_name"`
	Provider    string `json:"provider"`
	ActualModel string `json:"actual_model"`
	APIKeySet   bool   `json:"api_key_set"`
	APIBase     string `json:"api_base,omitempty"`
	RPM         int    `json:"rpm,omitempty"`
	TPM         int    `json:"tpm,omitempty"`
}

type adminConfigResponse struct {
	ModelList      []configModelEntry `json:"model_list"`
	RouterSettings routerSettingsJSON `json:"router_settings"`
	GeneralSettings struct {
		MasterKeySet bool   `json:"master_key_set"`
		DatabaseURL  string `json:"database_url"`
		Port         int    `json:"port"`
	} `json:"general_settings"`
}

// AdminConfig handles GET /admin/config.
// Secrets (API keys, master key) are never returned.
// getCfg is called on each request so the response reflects the latest reloaded config.
func AdminConfig(getCfg func() *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		cfg := getCfg()
		models := make([]configModelEntry, 0, len(cfg.ModelList))
		for _, mc := range cfg.ModelList {
			parsed := config.ParseModelString(mc.LiteLLMParams.Model)
			models = append(models, configModelEntry{
				ModelName:   mc.ModelName,
				Provider:    parsed.Provider,
				ActualModel: parsed.ModelName,
				APIKeySet:   mc.LiteLLMParams.APIKey != "",
				APIBase:     mc.LiteLLMParams.APIBase,
				RPM:         mc.RPM,
				TPM:         mc.TPM,
			})
		}

		settings := cfg.RouterSettings
		resp := adminConfigResponse{
			ModelList: models,
			RouterSettings: routerSettingsJSON{
				RoutingStrategy: settings.RoutingStrategy,
				NumRetries:      settings.NumRetries,
				AllowedFails:    settings.AllowedFails,
				CooldownTime:    settings.CooldownTime.String(),
			},
		}
		resp.GeneralSettings.MasterKeySet = cfg.GeneralSettings.MasterKey != ""
		resp.GeneralSettings.DatabaseURL = cfg.GeneralSettings.DatabaseURL
		resp.GeneralSettings.Port = cfg.GeneralSettings.Port

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

type reloadResponse struct {
	Status string `json:"status"`
}

// AdminReload handles POST /admin/reload.
// It re-reads the config file and updates the router with the new configuration.
// Note: changes to master_key, port, and database_url require a server restart.
func AdminReload(reloader *config.Reloader, r *router.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		newCfg, err := reloader.Reload()
		if err != nil {
			model.WriteError(w, model.ErrInternalServer("failed to reload config file", err))
			return
		}
		if err := r.Reload(newCfg); err != nil {
			model.WriteError(w, model.ErrInternalServer("failed to apply reloaded config", err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(reloadResponse{Status: "ok"})
	}
}

type logEntry struct {
	RequestID     string    `json:"request_id"`
	Model         string    `json:"model"`
	Provider      string    `json:"provider"`
	Endpoint      string    `json:"endpoint"`
	InputTokens   int       `json:"prompt_tokens"`    // JSON key kept for frontend compatibility
	OutputTokens  int       `json:"completion_tokens"` // JSON key kept for frontend compatibility
	TotalTokens   int       `json:"total_tokens"`
	TotalCost     float64   `json:"total_cost"`
	StatusCode    int       `json:"status_code"`
	LatencyMS     int64     `json:"latency_ms"`
	RequestTime   time.Time `json:"request_time"`
	IsStreaming   bool      `json:"is_streaming"`
	DeploymentKey string    `json:"deployment_key"`
}

type adminLogsResponse struct {
	Logs   []logEntry `json:"logs"`
	Total  int        `json:"total"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset"`
}

// AdminLogs handles GET /admin/logs.
func AdminLogs(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if store == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(adminLogsResponse{Logs: []logEntry{}})
			return
		}

		limit := 50
		offset := 0
		if v := req.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}
		if v := req.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}

		logs, total, err := store.GetLogs(req.Context(), limit, offset)
		if err != nil {
			http.Error(w, `{"error":{"message":"failed to fetch logs","type":"server_error"}}`, http.StatusInternalServerError)
			return
		}

		entries := make([]logEntry, 0, len(logs))
		for _, l := range logs {
			entries = append(entries, logEntry{
				RequestID:     l.RequestID,
				Model:         l.Model,
				Provider:      l.Provider,
				Endpoint:      l.Endpoint,
				InputTokens:   l.InputTokens,
				OutputTokens:  l.OutputTokens,
				TotalTokens:   l.InputTokens + l.OutputTokens,
				TotalCost:     l.TotalCost,
				StatusCode:    l.StatusCode,
				LatencyMS:     l.LatencyMS,
				RequestTime:   l.RequestTime,
				IsStreaming:   l.IsStreaming,
				DeploymentKey: l.DeploymentKey,
			})
		}
		if entries == nil {
			entries = []logEntry{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(adminLogsResponse{
			Logs:   entries,
			Total:  total,
			Limit:  limit,
			Offset: offset,
		})
	}
}
