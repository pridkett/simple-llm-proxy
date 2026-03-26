package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

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

		var body struct {
			Name          string   `json:"name"`
			AllowedModels []string `json:"allowed_models"`
			MaxRPM        *int     `json:"max_rpm"`
			MaxRPD        *int     `json:"max_rpd"`
			MaxBudget     *float64 `json:"max_budget"`
			SoftBudget    *float64 `json:"soft_budget"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
			model.WriteError(w, model.ErrBadRequest("name is required"))
			return
		}

		// Generate cryptographically secure key: sk-app-{48 hex chars}
		rawBytes := make([]byte, 24) // 24 bytes → 48 hex chars
		if _, err := rand.Read(rawBytes); err != nil {
			model.WriteError(w, model.ErrInternal("failed to generate key"))
			return
		}
		hexPart := hex.EncodeToString(rawBytes) // 48 hex chars
		fullKey := "sk-app-" + hexPart
		keyPrefix := hexPart[:8] // first 8 hex chars for display

		// SHA-256 hash for storage — never store plaintext
		hashBytes := sha256.Sum256([]byte(fullKey))
		keyHash := hex.EncodeToString(hashBytes[:])

		key, err := store.CreateAPIKey(r.Context(), appID, body.Name, keyPrefix, keyHash,
			body.MaxRPM, body.MaxRPD, body.MaxBudget, body.SoftBudget, body.AllowedModels)
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to create key"))
			return
		}

		// Return full key ONCE. key.KeyHash is json:"-" — not serialized.
		resp := map[string]interface{}{
			"key":            fullKey, // plaintext — shown once only
			"id":             key.ID,
			"name":           key.Name,
			"key_prefix":     fmt.Sprintf("sk-app-%s...", key.KeyPrefix),
			"application_id": key.ApplicationID,
			"max_rpm":        key.MaxRPM,
			"max_rpd":        key.MaxRPD,
			"max_budget":     key.MaxBudget,
			"soft_budget":    key.SoftBudget,
			"is_active":      key.IsActive,
			"created_at":     key.CreatedAt,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(resp)
	}
}

// AdminUpdateKey handles PATCH /admin/api-keys/{id}
// Admin only. Updates the mutable fields of a key (name, limits, budget, allowed models).
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
		var body struct {
			Name          string   `json:"name"`
			AllowedModels []string `json:"allowed_models"`
			MaxRPM        *int     `json:"max_rpm"`
			MaxRPD        *int     `json:"max_rpd"`
			MaxBudget     *float64 `json:"max_budget"`
			SoftBudget    *float64 `json:"soft_budget"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
			model.WriteError(w, model.ErrBadRequest("name is required"))
			return
		}
		if err := store.UpdateAPIKey(r.Context(), id, body.Name, body.MaxRPM, body.MaxRPD, body.MaxBudget, body.SoftBudget, body.AllowedModels); err != nil {
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
