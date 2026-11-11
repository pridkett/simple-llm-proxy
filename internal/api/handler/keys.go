package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// keyListItem wraps APIKey with its allowed_models for the list response.
type keyListItem struct {
	*storage.APIKey
	AllowedModels []string `json:"allowed_models"`
}

type keySettings struct {
	Name           string          `json:"name"`
	AllowDecisions *bool           `json:"allow_decisions"`
	AllowedModels  []string        `json:"allowed_models"`
	MaxRPM         *int            `json:"max_rpm"`
	MaxRPD         *int            `json:"max_rpd"`
	MaxBudget      *float64        `json:"max_budget"`
	SoftBudget     *float64        `json:"soft_budget"`
	ExpiresAt      json.RawMessage `json:"expires_at"`
}

func (body keySettings) options() (storage.APIKeyOptions, *model.ProxyError) {
	opts := storage.APIKeyOptions{AllowDecisions: body.AllowDecisions, SetExpiration: body.ExpiresAt != nil}
	if opts.SetExpiration {
		if err := json.Unmarshal(body.ExpiresAt, &opts.ExpiresAt); err != nil {
			return opts, model.ErrBadRequest("expires_at must be an RFC3339 date and time or null")
		}
		if opts.ExpiresAt != nil && !opts.ExpiresAt.Truncate(time.Millisecond).After(time.Now()) {
			return opts, model.ErrBadRequest("expires_at must be in the future")
		}
	}
	return opts, nil
}

// AdminListKeys handles GET /admin/applications/{id}/keys
// Returns all keys for the application with their allowed model lists.
func AdminListKeys(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid application id"))
			return
		}
		keys, err := store.ListAPIKeys(r.Context(), appID)
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to list keys"))
			return
		}
		items := make([]keyListItem, 0, len(keys))
		for _, k := range keys {
			models, err := store.GetKeyAllowedModels(r.Context(), k.ID)
			if err != nil {
				model.WriteError(w, model.ErrInternal("failed to get key models"))
				return
			}
			items = append(items, keyListItem{APIKey: k, AllowedModels: models})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(items)
	}
}

// AdminCreateKey handles POST /admin/applications/{id}/keys
// Admin only. Generates a new key, stores hash+prefix, returns full key ONCE.
// Per D-12: key format sk-app-{48 hex chars}. Per D-13: full key in response, not stored.
func AdminCreateKey(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil || !user.IsAdmin {
			model.WriteError(w, model.ErrForbidden("admin required"))
			return
		}
		appID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid application id"))
			return
		}

		var body keySettings
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
			model.WriteError(w, model.ErrBadRequest("name is required"))
			return
		}
		opts, validationError := body.options()
		if validationError != nil {
			model.WriteError(w, validationError)
			return
		}

		key, token, err := createAppKey(r.Context(), store, appID, body, opts)
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to create key"))
			return
		}
		writeCreatedAppKey(w, key, token, nil)
	}
}

func createAppKey(ctx context.Context, store storage.Storage, appID int64, body keySettings, opts storage.APIKeyOptions) (*storage.APIKey, string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, "", err
	}
	encoded := hex.EncodeToString(raw[:])
	token := "sk-app-" + encoded
	hash := sha256.Sum256([]byte(token))
	key, err := store.CreateAPIKey(ctx, appID, body.Name, encoded[:8], hex.EncodeToString(hash[:]),
		body.MaxRPM, body.MaxRPD, body.MaxBudget, body.SoftBudget, body.AllowedModels, opts)
	return key, token, err
}

func writeCreatedAppKey(w http.ResponseWriter, key *storage.APIKey, token string, allowedModels []string) {
	response := struct {
		*storage.APIKey
		Token         string   `json:"key"`
		Prefix        string   `json:"key_prefix"`
		AllowedModels []string `json:"allowed_models,omitempty"`
	}{key, token, "sk-app-" + key.KeyPrefix + "...", allowedModels}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// AdminUpdateKey handles PATCH /admin/api-keys/{id}
// Admin only. Updates the mutable fields of an active key, including its expiration.
// Immediately invalidates the cache entry so changes take effect on the next request.
func AdminUpdateKey(store storage.Storage, cache *keystore.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil || !user.IsAdmin {
			model.WriteError(w, model.ErrForbidden("admin required"))
			return
		}
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid key id"))
			return
		}
		var body keySettings
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
			model.WriteError(w, model.ErrBadRequest("name is required"))
			return
		}
		opts, validationError := body.options()
		if validationError != nil {
			model.WriteError(w, validationError)
			return
		}
		if err := store.UpdateAPIKey(r.Context(), id, body.Name, body.MaxRPM, body.MaxRPD, body.MaxBudget, body.SoftBudget, body.AllowedModels, opts); err != nil {
			if errors.Is(err, storage.ErrInactiveAPIKey) {
				model.WriteError(w, &model.ProxyError{
					StatusCode: http.StatusConflict,
					Type:       "invalid_request_error",
					Message:    "API key is revoked, expired, or no longer exists",
				})
				return
			}
			model.WriteError(w, model.ErrInternal("failed to update key"))
			return
		}
		cache.Invalidate(id)
		w.WriteHeader(http.StatusNoContent)
	}
}

// AdminRevokeKey handles DELETE /admin/api-keys/{id}
// Admin only. Marks the key inactive and immediately invalidates the cache entry.
func AdminRevokeKey(store storage.Storage, cache *keystore.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil || !user.IsAdmin {
			model.WriteError(w, model.ErrForbidden("admin required"))
			return
		}
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid key id"))
			return
		}
		if err := store.RevokeAPIKey(r.Context(), id); err != nil {
			model.WriteError(w, model.ErrInternal("failed to revoke key"))
			return
		}
		// Immediately evict from cache — revoked key must not be accepted on next request
		cache.Invalidate(id)
		w.WriteHeader(http.StatusNoContent)
	}
}
