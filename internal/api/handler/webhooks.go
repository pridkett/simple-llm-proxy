package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// webhookResponse is the API representation of a webhook (YAML or DB).
// Secret is NEVER included. Source and ReadOnly are computed at response time.
type webhookResponse struct {
	ID        int64    `json:"id"`
	URL       string   `json:"url"`
	Events    []string `json:"events"`
	Enabled   bool     `json:"enabled"`
	Source    string   `json:"source"`     // "yaml" or "ui"
	ReadOnly  bool     `json:"read_only"`  // true for YAML webhooks
	CreatedAt string   `json:"created_at,omitempty"`
}

type adminWebhooksListResponse struct {
	Webhooks []webhookResponse `json:"webhooks"`
}

type webhookCreateRequest struct {
	URL     string   `json:"url"`
	Events  []string `json:"events"`
	Secret  string   `json:"secret"`
	Enabled *bool    `json:"enabled"` // pointer so we detect omitted vs false
}

type adminEventsResponse struct {
	Events []eventResponse `json:"events"`
	Total  int             `json:"total"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

type eventResponse struct {
	ID        int64  `json:"id"`
	EventType string `json:"event_type"`
	Payload   any    `json:"payload"` // parsed from JSON string for clean API response
	CreatedAt string `json:"created_at"`
}

// AdminListWebhooks handles GET /admin/webhooks.
// Returns a merged list of YAML-configured and DB-stored (UI-created) webhooks.
// YAML webhooks have synthetic negative IDs, source="yaml", read_only=true.
// DB webhooks have source="ui", read_only=false. Secrets are never included.
func AdminListWebhooks(store storage.Storage, getCfg func() *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		// Load DB webhooks
		dbHooks, err := store.ListWebhookSubscriptions(req.Context())
		if err != nil {
			model.WriteError(w, model.ErrInternalServer("failed to list webhooks", err))
			return
		}

		// Load YAML webhooks from config
		cfg := getCfg()
		yamlHooks := cfg.Webhooks

		result := make([]webhookResponse, 0, len(yamlHooks)+len(dbHooks))

		// YAML webhooks first, with synthetic negative IDs
		for i, yh := range yamlHooks {
			result = append(result, webhookResponse{
				ID:       int64(-(i + 1)),
				URL:      yh.URL,
				Events:   yh.Events,
				Enabled:  yh.Enabled,
				Source:   "yaml",
				ReadOnly: true,
			})
		}

		// DB webhooks second
		for _, dh := range dbHooks {
			result = append(result, webhookResponse{
				ID:        dh.ID,
				URL:       dh.URL,
				Events:    dh.Events,
				Enabled:   dh.Enabled,
				Source:    "ui",
				ReadOnly:  false,
				CreatedAt: dh.CreatedAt.Format("2006-01-02T15:04:05Z"),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(adminWebhooksListResponse{Webhooks: result})
	}
}

// AdminCreateWebhook handles POST /admin/webhooks.
// Creates a new UI webhook in the database. Returns 201 with the created webhook.
func AdminCreateWebhook(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var body webhookCreateRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid JSON body"))
			return
		}

		// Validate required fields
		if body.URL == "" {
			model.WriteError(w, model.ErrBadRequest("url is required"))
			return
		}
		if len(body.Events) == 0 {
			model.WriteError(w, model.ErrBadRequest("events must be a non-empty array"))
			return
		}

		// Default Enabled to true if not provided
		enabled := true
		if body.Enabled != nil {
			enabled = *body.Enabled
		}

		sub := &storage.WebhookSubscription{
			URL:     body.URL,
			Events:  body.Events,
			Secret:  body.Secret,
			Enabled: enabled,
		}

		created, err := store.CreateWebhookSubscription(req.Context(), sub)
		if err != nil {
			model.WriteError(w, model.ErrInternalServer("failed to create webhook", err))
			return
		}

		resp := webhookResponse{
			ID:        created.ID,
			URL:       created.URL,
			Events:    created.Events,
			Enabled:   created.Enabled,
			Source:    "ui",
			ReadOnly:  false,
			CreatedAt: created.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(resp)
	}
}

// AdminUpdateWebhook handles PUT /admin/webhooks/{id}.
// Updates an existing UI webhook. Returns 403 for YAML webhooks (negative IDs).
func AdminUpdateWebhook(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		idStr := chi.URLParam(req, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid webhook id"))
			return
		}

		// YAML webhooks have negative IDs and are read-only
		if id < 0 {
			model.WriteError(w, model.ErrForbidden("YAML webhooks are read-only and cannot be modified"))
			return
		}

		var body webhookCreateRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid JSON body"))
			return
		}

		if body.URL == "" {
			model.WriteError(w, model.ErrBadRequest("url is required"))
			return
		}
		if len(body.Events) == 0 {
			model.WriteError(w, model.ErrBadRequest("events must be a non-empty array"))
			return
		}

		enabled := true
		if body.Enabled != nil {
			enabled = *body.Enabled
		}

		sub := &storage.WebhookSubscription{
			ID:      id,
			URL:     body.URL,
			Events:  body.Events,
			Secret:  body.Secret,
			Enabled: enabled,
		}

		if err := store.UpdateWebhookSubscription(req.Context(), sub); err != nil {
			model.WriteError(w, model.ErrInternalServer("failed to update webhook", err))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// AdminDeleteWebhook handles DELETE /admin/webhooks/{id}.
// Deletes a UI webhook. Returns 403 for YAML webhooks (negative IDs).
func AdminDeleteWebhook(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		idStr := chi.URLParam(req, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid webhook id"))
			return
		}

		// YAML webhooks have negative IDs and are read-only
		if id < 0 {
			model.WriteError(w, model.ErrForbidden("YAML webhooks are read-only and cannot be deleted"))
			return
		}

		if err := store.DeleteWebhookSubscription(req.Context(), id); err != nil {
			model.WriteError(w, model.ErrInternalServer("failed to delete webhook", err))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// AdminEvents handles GET /admin/events.
// Returns paginated notification events, optionally filtered by event_type.
func AdminEvents(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
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
		eventType := req.URL.Query().Get("event_type")

		events, total, err := store.ListNotificationEvents(req.Context(), limit, offset, eventType)
		if err != nil {
			model.WriteError(w, model.ErrInternalServer("failed to list events", err))
			return
		}

		entries := make([]eventResponse, 0, len(events))
		for _, e := range events {
			// Parse the payload JSON string into any for clean API output
			var payload any
			if e.Payload != "" {
				if jsonErr := json.Unmarshal([]byte(e.Payload), &payload); jsonErr != nil {
					// If parsing fails, use the raw string
					payload = e.Payload
				}
			}

			entries = append(entries, eventResponse{
				ID:        e.ID,
				EventType: e.EventType,
				Payload:   payload,
				CreatedAt: e.CreatedAt.Format("2006-01-02T15:04:05Z"),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(adminEventsResponse{
			Events: entries,
			Total:  total,
			Limit:  limit,
			Offset: offset,
		})
	}
}
