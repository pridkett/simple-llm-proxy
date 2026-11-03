package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"
	_ "time/tzdata" // Reporting must also work in minimal container images.

	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

type usageStore interface {
	storage.UsageStorage
	GetSpendSummary(context.Context, time.Time, time.Time, storage.SpendFilters) ([]storage.SpendRow, error)
}

type usageGroup struct {
	GroupID string `json:"group_id"`
	Name    string `json:"name"`
	storage.UsageMetrics
}
type usageResponse struct {
	BudgetRows    []storage.SpendRow       `json:"budget_rows"`
	Alerts        []spendAlert             `json:"alerts"`
	From          string                   `json:"from"`
	To            string                   `json:"to"`
	FromTime      time.Time                `json:"from_time"`
	ToTime        time.Time                `json:"to_time"`
	Timezone      string                   `json:"timezone"`
	Interval      string                   `json:"interval"`
	GroupBy       string                   `json:"group_by"`
	RetentionDays int                      `json:"retention_days"`
	Summary       storage.UsageMetrics     `json:"summary"`
	Buckets       []storage.UsageBucket    `json:"buckets"`
	Groups        []usageGroup             `json:"groups"`
	Series        []storage.UsageAggregate `json:"series"`
}

// AdminUsage returns one consistent population for all reporting components.
// Date-only bounds are inclusive in the requested timezone; since/until are an
// optional exact half-open interval used when drilling into a chart bucket.
func AdminUsage(store usageStore, getCfg func() *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		user := middleware.UserFromContext(req.Context())
		if user == nil || !user.IsAdmin {
			model.WriteError(w, model.ErrForbidden("admin required"))
			return
		}
		query, tz, interval, err := parseUsageQuery(req.URL.Query(), time.Now())
		if err != nil {
			model.WriteError(w, model.ErrBadRequest(err.Error()))
			return
		}
		rows, err := store.GetUsage(req.Context(), query)
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to load usage report"))
			return
		}
		var budgetRows []storage.SpendRow
		// Unattributed traffic has no applicable active-key budget.
		if query.Unattributed == "" {
			budgetRows, err = store.GetSpendSummary(req.Context(), query.From, query.To, storage.SpendFilters{TeamID: query.Filters.TeamID, AppID: query.Filters.AppID, KeyID: query.Filters.KeyID})
			if err != nil {
				model.WriteError(w, model.ErrInternal("failed to load budget data"))
				return
			}
		}
		if budgetRows == nil {
			budgetRows = []storage.SpendRow{}
		}
		loc, _ := time.LoadLocation(tz)
		resp := usageResponse{BudgetRows: budgetRows, Alerts: computeAlerts(budgetRows), From: query.From.In(loc).Format("2006-01-02"), To: query.To.Add(-time.Nanosecond).In(loc).Format("2006-01-02"), FromTime: query.From.UTC(), ToTime: query.To.UTC(), Timezone: tz, Interval: interval, GroupBy: query.GroupBy, Buckets: query.Buckets, Groups: []usageGroup{}, Series: rows}
		if resp.Series == nil {
			resp.Series = []storage.UsageAggregate{}
		}
		if getCfg != nil {
			if cfg := getCfg(); cfg != nil {
				resp.RetentionDays = cfg.GeneralSettings.LogRetentionDays
			}
		}
		groups := map[string]*usageGroup{}
		for _, row := range rows {
			resp.Summary.Add(row.UsageMetrics)
			g := groups[row.GroupID]
			if g == nil {
				g = &usageGroup{GroupID: row.GroupID, Name: row.Name}
				groups[row.GroupID] = g
			}
			g.Add(row.UsageMetrics)
		}
		for _, g := range groups {
			resp.Groups = append(resp.Groups, *g)
		}
		sort.Slice(resp.Groups, func(i, j int) bool {
			if resp.Groups[i].Calls != resp.Groups[j].Calls {
				return resp.Groups[i].Calls > resp.Groups[j].Calls
			}
			return resp.Groups[i].GroupID < resp.Groups[j].GroupID
		})
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(resp)
	}
}

func parseUsageQuery(v url.Values, now time.Time) (storage.UsageQuery, string, string, error) {
	var q storage.UsageQuery
	fail := func(msg string) (storage.UsageQuery, string, string, error) { return q, "", "", fmt.Errorf("%s", msg) }
	tz := v.Get("timezone")
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil || tz == "Local" {
		return fail("invalid timezone: use an IANA timezone name")
	}
	date := func(key string, def time.Time) (time.Time, error) {
		if v.Get(key) == "" {
			return def, nil
		}
		return time.ParseInLocation("2006-01-02", v.Get(key), loc)
	}
	today := now.In(loc)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)
	q.From, err = date("from", today.AddDate(0, 0, -6))
	if err != nil {
		return fail("invalid from: use YYYY-MM-DD")
	}
	last, err := date("to", today)
	if err != nil {
		return fail("invalid to: use YYYY-MM-DD")
	}
	q.To = last.AddDate(0, 0, 1)
	if v.Get("since") != "" || v.Get("until") != "" {
		q.From, err = time.Parse(time.RFC3339Nano, v.Get("since"))
		if err != nil {
			return fail("since and until require RFC3339 timestamps")
		}
		q.To, err = time.Parse(time.RFC3339Nano, v.Get("until"))
		if err != nil {
			return fail("since and until require RFC3339 timestamps")
		}
	}
	if !q.From.Before(q.To) || q.To.After(q.From.In(loc).AddDate(0, 0, 366)) {
		return fail("date range must be ordered and at most 366 days")
	}
	q.GroupBy = v.Get("group_by")
	if q.GroupBy == "" {
		q.GroupBy = "model"
	}
	switch q.GroupBy {
	case "model", "app", "key", "team":
	default:
		return fail("group_by must be model, app, key or team")
	}
	for _, f := range []struct {
		param  string
		target **int64
	}{{"team_id", &q.Filters.TeamID}, {"app_id", &q.Filters.AppID}, {"key_id", &q.Filters.KeyID}} {
		if raw := v.Get(f.param); raw != "" {
			id, e := strconv.ParseInt(raw, 10, 64)
			if e != nil || id <= 0 {
				return fail("invalid " + f.param)
			}
			*f.target = &id
		}
	}
	q.Filters.Model = v.Get("model")
	if len(q.Filters.Model) > 512 {
		return fail("model filter is too long")
	}
	q.Unattributed = v.Get("unattributed")
	switch q.Unattributed {
	case "", "team", "app", "key":
	default:
		return fail("invalid unattributed filter")
	}
	interval := v.Get("interval")
	if interval == "" {
		interval = "day"
	}
	switch interval {
	case "hour", "day", "week":
	default:
		return fail("interval must be hour, day or week")
	}
	q.Buckets, err = usageBuckets(q.From, q.To, loc, interval)
	if err != nil {
		return fail(err.Error())
	}
	return q, tz, interval, nil
}

func usageBuckets(from, to time.Time, loc *time.Location, interval string) ([]storage.UsageBucket, error) {
	local := from.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	if interval == "hour" {
		// Advance from local midnight by elapsed hours to preserve both fall-back hours.
		for !start.Add(time.Hour).After(from) {
			start = start.Add(time.Hour)
		}
	} else if interval == "week" {
		start = start.AddDate(0, 0, -(int(start.Weekday())+6)%7)
	}
	buckets := []storage.UsageBucket{}
	for start.Before(to) {
		end := start.Add(time.Hour)
		if interval == "day" {
			end = start.AddDate(0, 0, 1)
		} else if interval == "week" {
			end = start.AddDate(0, 0, 7)
		}
		lower, upper := start, end
		if lower.Before(from) {
			lower = from
		}
		if upper.After(to) {
			upper = to
		}
		label := start.In(loc).Format("Jan 2")
		if interval == "hour" {
			label = start.In(loc).Format("Jan 2 15:04 -07:00")
		}
		buckets = append(buckets, storage.UsageBucket{Start: lower.UTC(), End: upper.UTC(), Label: label})
		if len(buckets) > 744 {
			return nil, fmt.Errorf("too many buckets: choose a coarser interval or shorter range")
		}
		start = end
	}
	return buckets, nil
}
