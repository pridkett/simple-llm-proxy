package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// spendResponse is the JSON envelope for GET /admin/spend.
type spendResponse struct {
	Rows   []storage.SpendRow `json:"rows"`
	Alerts []spendAlert       `json:"alerts"`
	From   string             `json:"from"`
	To     string             `json:"to"`
}

// spendAlert describes a key that has exceeded or is approaching its budget.
type spendAlert struct {
	KeyID      int64    `json:"key_id"`
	KeyName    string   `json:"key_name"`
	AppName    string   `json:"app_name"`
	TeamName   string   `json:"team_name"`
	TotalSpend float64  `json:"total_spend"`
	SoftBudget *float64 `json:"soft_budget"`
	MaxBudget  *float64 `json:"max_budget"`
	AlertType  string   `json:"alert_type"` // "soft" | "hard"
}

// AdminSpend handles GET /admin/spend.
//
// Authorization: admin-only. This endpoint exposes deployment-wide spend data.
// It is registered under the admin route group which applies RequireSession middleware
// (handles 401 for truly unauthenticated requests). Admin-only enforcement is done
// per-handler via middleware.UserFromContext — the same pattern used by AdminUsers,
// AdminTeams, and other admin-only handlers. A nil user or a non-admin user receives 403.
//
// Query params (all optional):
//
//	from    YYYY-MM-DD  User-facing inclusive start date. Default: today-7d.
//	to      YYYY-MM-DD  User-facing inclusive end date. Default: today.
//	                    IMPORTANT: The backend adds 1 day to `to` before passing to SQL,
//	                    making it an exclusive upper bound. This means a row at 23:59:59
//	                    on the user-facing `to` date IS included. The response returns
//	                    the user-facing inclusive date, not the exclusive SQL bound.
//	team_id  integer    Optional filter. Empty or "0" = no filter (nil).
//	app_id   integer    Optional filter. Empty or "0" = no filter (nil).
//	key_id   integer    Optional filter. Empty or "0" = no filter (nil).
func AdminSpend(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		// Admin-only guard — same pattern as AdminUsers, AdminTeams, etc.
		user := middleware.UserFromContext(req.Context())
		if user == nil || !user.IsAdmin {
			model.WriteError(w, model.ErrForbidden("admin required"))
			return
		}

		q := req.URL.Query()

		// Parse date range.
		// Default from: 7 days ago. Default to (user-facing inclusive): today.
		now := time.Now().UTC().Truncate(24 * time.Hour)
		defaultFrom := now.AddDate(0, 0, -7).Format("2006-01-02")
		defaultTo := now.Format("2006-01-02")

		fromTime, err := parseSpendDate(q.Get("from"), defaultFrom)
		if err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid 'from' date: use YYYY-MM-DD format"))
			return
		}

		// User sends inclusive to date; we add 1 day to make it exclusive for SQL.
		// Example: user sends to=2026-03-26 → SQL uses request_time < 2026-03-27T00:00:00Z
		// This ensures rows at 23:59:59 on 2026-03-26 are included.
		toInclusive, err := parseSpendDate(q.Get("to"), defaultTo)
		if err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid 'to' date: use YYYY-MM-DD format"))
			return
		}
		toSQL := toInclusive.AddDate(0, 0, 1) // exclusive upper bound for SQL

		// Parse optional int64 filters.
		// parseOptionalInt64 returns nil for empty string or "0" — ensures nil (not 0) reaches SQL.
		// The double-bind pattern (? IS NULL OR col = ?) in GetSpendSummary requires nil for "no filter".
		filters := storage.SpendFilters{
			TeamID: parseOptionalInt64(q.Get("team_id")),
			AppID:  parseOptionalInt64(q.Get("app_id")),
			KeyID:  parseOptionalInt64(q.Get("key_id")),
		}

		rows, err := store.GetSpendSummary(req.Context(), fromTime, toSQL, filters)
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to load spend data"))
			return
		}

		// Compute alerts: keys at or above soft threshold or hard budget.
		alerts := computeAlerts(rows)

		resp := spendResponse{
			Rows:   rows,
			Alerts: alerts,
			From:   fromTime.Format("2006-01-02"),
			To:     toInclusive.Format("2006-01-02"), // return user-facing inclusive date
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// parseSpendDate parses a YYYY-MM-DD string. If s is empty, parses defaultVal.
func parseSpendDate(s, defaultVal string) (time.Time, error) {
	if s == "" {
		s = defaultVal
	}
	return time.ParseInLocation("2006-01-02", s, time.UTC)
}

// parseOptionalInt64 returns nil if s is empty or "0", otherwise a pointer to the parsed int64.
// Only positive integers are valid filter IDs. "0" and non-numeric values return nil.
// This ensures the SQL double-bind pattern (? IS NULL OR col = ?) receives nil for absent filters.
func parseOptionalInt64(s string) *int64 {
	if s == "" {
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v <= 0 {
		return nil
	}
	return &v
}

// computeAlerts returns alerts for rows where spend has reached soft or hard budget.
// Hard budget exceeded takes precedence — a row appears only once in the alert list.
func computeAlerts(rows []storage.SpendRow) []spendAlert {
	alerts := make([]spendAlert, 0)
	for _, r := range rows {
		var alertType string
		// Hard budget check first (takes precedence over soft)
		if r.MaxBudget != nil && r.TotalSpend >= *r.MaxBudget {
			alertType = "hard"
		} else if r.SoftBudget != nil && r.TotalSpend >= *r.SoftBudget {
			alertType = "soft"
		}
		if alertType == "" {
			continue
		}
		alerts = append(alerts, spendAlert{
			KeyID:      r.KeyID,
			KeyName:    r.KeyName,
			AppName:    r.AppName,
			TeamName:   r.TeamName,
			TotalSpend: r.TotalSpend,
			SoftBudget: r.SoftBudget,
			MaxBudget:  r.MaxBudget,
			AlertType:  alertType,
		})
	}
	return alerts
}
