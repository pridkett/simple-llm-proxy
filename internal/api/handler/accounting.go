package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pwagstro/simple_llm_proxy/internal/accounting"
	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

type callAccountingKey struct{}
type callAccounting struct {
	reconcile                           func(*model.Usage)
	collector                           *accounting.Collector
	ctx                                 context.Context
	attemptCtx                          context.Context
	store                               storage.Storage
	sa                                  *keystore.SpendAccumulator
	cm                                  *costmap.Manager
	router                              *router.Router
	writer                              http.ResponseWriter
	inline, streaming, background, done bool
	model, endpoint, session            string
	start                               time.Time
	deployment                          *provider.Deployment
	logParams                           *logRequestParams
	err                                 error
	record                              *accounting.Record
}

func callAccountingFromContext(ctx context.Context) *callAccounting {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(callAccountingKey{}).(*callAccounting)
	return s
}
func startAccounting(ctx context.Context, w http.ResponseWriter, req *http.Request, store storage.Storage, sa *keystore.SpendAccumulator, cm *costmap.Manager, r *router.Router, modelName, endpoint, session string, keyID *int64, stream bool) (context.Context, *callAccounting) {
	c := accounting.New(middleware.RequestIDFromContext(ctx), keyID)
	ctx = accounting.WithCollector(ctx, c)
	s := &callAccounting{collector: c, ctx: ctx, store: store, sa: sa, cm: cm, router: r, writer: w, inline: req.Header.Get("X-LLM-Include-Accounting") == "true", streaming: stream, model: modelName, endpoint: endpoint, session: session, start: time.Now()}
	ctx = context.WithValue(ctx, callAccountingKey{}, s)
	s.ctx = ctx
	pending := c.Snapshot(false)
	setAccountingHeaders(w, pending)
	if db, ok := store.(storage.AccountingStorage); ok {
		if err := db.PutAccounting(ctx, pending); err != nil {
			fmt.Fprintln(os.Stderr, "accounting: failed to persist pending record:", err)
		}
	}
	return ctx, s
}
func (s *callAccounting) options(tokens int) router.RouteOptions {
	options := router.RouteOptions{EstimatedTokens: tokens, UsageSnapshot: s.collector.CurrentUsage, BeforeAttempt: func(d *provider.Deployment, pool string) {
		s.deployment = d
		s.attemptCtx = s.collector.Begin(s.ctx, d.ProviderName, d.ModelName, d.ActualModel, d.DeploymentKey(), pool, d.CostPolicy, s.cm)
	}, AfterAttempt: func(resp *model.ChatCompletionResponse, err error) {
		if resp != nil {
			accounting.ObserveUsage(s.attemptCtx, resp.Usage)
		}
		if err != nil || !s.streaming {
			accounting.EndAttempt(s.attemptCtx, err)
		}
		s.savePending()
	}}
	if s.streaming {
		options.DeferReconcile = func(finish func(*model.Usage)) { s.reconcile = finish }
	}
	return options
}
func (s *callAccounting) cost(usage *model.Usage) float64 {
	s.collector.ObserveUsage(usage)
	return accounting.Float(s.collector.Snapshot(false).Cost.Amount)
}
func (s *callAccounting) close() {
	if !s.done && !s.background {
		s.complete()
	}
}
func (s *callAccounting) complete() *accounting.Record {
	if s.done {
		return s.record
	}
	s.done = true
	if s.attemptCtx != nil {
		accounting.EndAttempt(s.attemptCtx, s.err)
	}
	rec := s.collector.Snapshot(true)
	s.record = rec
	if s.reconcile != nil {
		s.reconcile(s.collector.CurrentUsage())
	}
	entry := s.logEntry(rec)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	won := true
	if db, ok := s.store.(storage.AccountingStorage); ok {
		var err error
		won, err = db.FinalizeAccounting(ctx, rec, entry)
		if err != nil {
			fmt.Fprintln(os.Stderr, "accounting: failed to finalize record:", err)
			won = false
		}
	} else if s.store != nil && entry != nil {
		if err := s.store.LogRequest(ctx, entry); err != nil {
			fmt.Fprintln(os.Stderr, "accounting: failed to write usage log:", err)
			won = false
		}
	}
	if won {
		creditAccounting(rec, s.sa, s.router.BudgetManager())
	}
	if !s.streaming {
		setAccountingHeaders(s.writer, rec)
	}
	return rec
}
func creditAccounting(rec *accounting.Record, sa *keystore.SpendAccumulator, budget *router.PoolBudgetManager) {
	if rec.APIKeyID != nil && sa != nil {
		if v := accounting.Float(rec.Cost.Amount); v > 0 {
			sa.Credit(*rec.APIKeyID, v)
		}
	}
	if budget != nil {
		for _, a := range rec.Attempts {
			if v := accounting.Float(a.Cost.Amount); v > 0 && a.Pool != "" {
				budget.Credit(a.Pool, v)
			}
		}
	}
}
func (s *callAccounting) logEntry(rec *accounting.Record) *storage.RequestLog {
	if len(rec.Attempts) == 0 {
		return nil
	}
	entry := storage.AccountingLog(rec, s.model, s.endpoint, s.start)
	entry.SessionID = s.session
	entry.IsStreaming = s.streaming
	if s.logParams != nil {
		p := s.logParams
		entry.TTFTMs = p.TTFTMs
		entry.ReqBodySnippet = p.ReqBodySnippet
		entry.RespBodySnippet = p.RespBodySnippet
		entry.RequestTime = p.StartTime
		entry.LatencyMS = time.Since(p.StartTime).Milliseconds()
	}
	if s.err != nil {
		entry.StatusCode = http.StatusBadGateway
		var status interface{ HTTPStatus() int }
		if errors.As(s.err, &status) {
			entry.StatusCode = status.HTTPStatus()
		}
	}
	if s.deployment != nil {
		entry.Provider = s.deployment.ProviderName
		entry.DeploymentKey = s.deployment.DeploymentKey()
	}
	entry.PoolName = rec.Attempts[len(rec.Attempts)-1].Pool
	return entry
}
func (s *callAccounting) inlineRecord() any {
	rec := s.complete()
	if s.inline {
		return rec
	}
	return nil
}
func (s *callAccounting) emitStream(w http.ResponseWriter, responses bool) {
	rec := s.complete()
	if !s.inline {
		return
	}
	data, _ := json.Marshal(map[string]any{"proxy_accounting": rec, "choices": []any{}})
	if responses {
		data, _ = json.Marshal(map[string]any{"type": "proxy.accounting", "proxy_accounting": rec})
		fmt.Fprint(w, "event: proxy.accounting\n")
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}
func setAccountingHeaders(w http.ResponseWriter, r *accounting.Record) {
	h := w.Header()
	h.Set("X-LLM-Accounting-ID", r.ID)
	h.Set("X-LLM-Accounting-State", r.State)
	h.Set("X-LLM-Accounting-Coverage", r.Coverage)
	if r.State != "final" {
		return
	}
	if r.Cost.Amount != nil {
		h.Set("X-LLM-Cost-USD", *r.Cost.Amount)
		h.Set("X-LLM-Cost-Source", r.Cost.Source)
		h.Set("x-litellm-response-cost", *r.Cost.Amount)
	}
	if r.Usage.TotalTokens != nil {
		h.Set("x-litellm-total-tokens", strconv.FormatInt(*r.Usage.TotalTokens, 10))
	}
}

// AccountingDetail exposes a record only to its owning app key or an administrator.
func AccountingDetail(store storage.Storage, admin bool) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		db, ok := store.(storage.AccountingStorage)
		if !ok {
			model.WriteError(w, model.ErrNotFound("accounting unavailable"))
			return
		}
		if admin {
			u := middleware.UserFromContext(req.Context())
			if u == nil || !u.IsAdmin {
				model.WriteError(w, model.ErrForbidden("admin access required"))
				return
			}
		}
		rec, err := db.GetAccounting(req.Context(), chi.URLParam(req, "id"))
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to read accounting"))
			return
		}
		if rec != nil && !admin {
			if key := middleware.APIKeyFromContext(req.Context()); key != nil {
				if rec.APIKeyID == nil || *rec.APIKeyID != key.Key.ID {
					rec = nil
				}
			}
		}
		if rec == nil {
			model.WriteError(w, model.ErrNotFound("accounting not found"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(rec)
	}
}
func (s *callAccounting) savePending() {
	if db, ok := s.store.(storage.AccountingStorage); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.PutAccounting(ctx, s.collector.Snapshot(false)); err != nil {
			fmt.Fprintln(os.Stderr, "accounting: failed to update pending record:", err)
		}
	}
}

func setJobAccounting(w http.ResponseWriter, req *http.Request, store storage.Storage, job *storage.ResponsesJob, resp *model.ResponsesResponse) {
	db, ok := store.(storage.AccountingStorage)
	if !ok || job.AccountingID == "" {
		return
	}
	rec, err := db.GetAccounting(req.Context(), job.AccountingID)
	if err != nil || rec == nil {
		return
	}
	setAccountingHeaders(w, rec)
	if req.Header.Get("X-LLM-Include-Accounting") == "true" {
		if resp != nil {
			resp.ProxyAccounting = rec
		} else if job.ResponseJSON != nil {
			var obj map[string]any
			if json.Unmarshal([]byte(*job.ResponseJSON), &obj) == nil {
				obj["proxy_accounting"] = rec
				b, _ := json.Marshal(obj)
				body := string(b)
				job.ResponseJSON = &body
			}
		}
	}
}

func (s *callAccounting) hasCost() bool {
	if s.record != nil {
		return s.record.Cost.Amount != nil
	}
	return s.collector.Snapshot(false).Cost.Amount != nil
}
