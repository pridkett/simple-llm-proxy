package handler

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"github.com/rs/zerolog/log"
)

const keyIssuancePath = "/v1/key-issuance/keys"

// RegisterKeyIssuanceRoutes exposes only creation and scoped revocation to issuer credentials.
func RegisterKeyIssuanceRoutes(r chi.Router, store storage.Storage, getCfg func() *config.Config) {
	issued, ok := store.(storage.IssuedAPIKeyStorage)
	if !ok {
		return
	}
	r.Post(keyIssuancePath, withKeyIssuer(getCfg, func(w http.ResponseWriter, r *http.Request, issuerID string, policy config.KeyIssuer) {
		var body keySettings
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		err := decoder.Decode(&body)
		if err == nil {
			if trailing := decoder.Decode(new(any)); trailing != io.EOF {
				err = trailing
				if err == nil {
					err = errors.New("expected exactly one JSON object")
				}
			}
		}
		if err != nil {
			var limit *http.MaxBytesError
			if errors.As(err, &limit) {
				model.WriteError(w, &model.ProxyError{StatusCode: http.StatusRequestEntityTooLarge, Message: "key issuance request exceeds size limit", Type: "invalid_request_error"})
			} else {
				model.WriteError(w, model.ErrBadRequest("expected one JSON object with key settings"))
			}
			return
		}
		opts, validationError := scopeIssuedKey(&body, policy)
		if validationError != nil {
			model.WriteError(w, validationError)
			return
		}
		opts.IssuerID = issuerID
		key, token, err := createAppKey(r.Context(), store, policy.ApplicationID, body, opts)
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to create issued key"))
			return
		}
		log.Info().Str("issuer_id", issuerID).Int64("application_id", key.ApplicationID).Int64("key_id", key.ID).
			Time("expires_at", *key.ExpiresAt).Msg("app key issued")
		writeCreatedAppKey(w, key, token, body.AllowedModels)
	}))

	r.Delete(keyIssuancePath+"/{id}", withKeyIssuer(getCfg, func(w http.ResponseWriter, r *http.Request, issuerID string, policy config.KeyIssuer) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || id <= 0 {
			model.WriteError(w, model.ErrBadRequest("invalid key ID"))
			return
		}
		found, err := issued.RevokeIssuedAPIKey(r.Context(), policy.ApplicationID, issuerID, id)
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to revoke issued key"))
			return
		}
		if !found {
			model.WriteError(w, &model.ProxyError{StatusCode: http.StatusNotFound, Message: "issued key not found", Type: "invalid_request_error"})
			return
		}
		log.Info().Str("issuer_id", issuerID).Int64("application_id", policy.ApplicationID).Int64("key_id", id).
			Msg("app key revocation accepted")
		w.WriteHeader(http.StatusNoContent)
	}))
}

func withKeyIssuer(getCfg func() *config.Config, next func(http.ResponseWriter, *http.Request, string, config.KeyIssuer)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		scheme, token, _ := strings.Cut(r.Header.Get("Authorization"), " ")
		if strings.EqualFold(scheme, "Bearer") && strings.HasPrefix(token, config.KeyIssuerTokenPrefix) &&
			len(token) == len(config.KeyIssuerTokenPrefix)+64 {
			digest := sha256.Sum256([]byte(token))
			var issuerID string
			var policy config.KeyIssuer
			for id, candidate := range getCfg().KeyIssuers {
				if subtle.ConstantTimeCompare(digest[:], candidate.TokenHash[:]) == 1 {
					issuerID, policy = id, candidate
				}
			}
			if issuerID != "" {
				next(w, r, issuerID, policy)
				return
			}
		}
		w.Header().Set("WWW-Authenticate", "Bearer")
		model.WriteError(w, model.ErrUnauthorized("invalid or missing key issuer credential"))
	}
}

func scopeIssuedKey(body *keySettings, policy config.KeyIssuer) (storage.APIKeyOptions, *model.ProxyError) {
	opts, err := body.options()
	if err != nil {
		return opts, err
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" || opts.ExpiresAt == nil || !config.ValidModelAllowlist(body.AllowedModels) {
		return opts, model.ErrBadRequest("name, expires_at, and a nonempty allowlist of distinct models are required")
	}
	if opts.ExpiresAt.Truncate(time.Millisecond).After(time.Now().Add(policy.MaxTTL)) {
		return opts, model.ErrForbidden("expiration exceeds the key issuer's maximum lifetime")
	}
	for _, alias := range body.AllowedModels {
		if !slices.Contains(policy.AllowedModels, alias) {
			return opts, model.ErrForbidden("requested model is outside the key issuer's allowlist")
		}
	}
	if body.AllowDecisions != nil && *body.AllowDecisions && !policy.AllowDecisions {
		return opts, model.ErrForbidden("the key issuer cannot grant decision access")
	}
	if (body.MaxRPM != nil && *body.MaxRPM > math.MaxInt32) || (body.MaxRPD != nil && *body.MaxRPD > math.MaxInt32) {
		return opts, model.ErrBadRequest("request limits must fit in 32 bits")
	}
	if body.MaxRPM, err = issuedLimit("max_rpm", body.MaxRPM, policy.MaxRPM); err != nil {
		return opts, err
	}
	if body.MaxRPD, err = issuedLimit("max_rpd", body.MaxRPD, policy.MaxRPD); err != nil {
		return opts, err
	}
	if body.MaxBudget, err = issuedLimit("max_budget", body.MaxBudget, policy.MaxBudget); err != nil {
		return opts, err
	}
	if _, err = issuedLimit("soft_budget", body.SoftBudget, body.MaxBudget); err != nil {
		return opts, err
	}
	return opts, nil
}

func issuedLimit[T int | float64](field string, requested, ceiling *T) (*T, *model.ProxyError) {
	if requested == nil {
		return ceiling, nil
	}
	if *requested <= 0 {
		return nil, model.ErrBadRequest(field + " must be positive")
	}
	if ceiling != nil && *requested > *ceiling {
		return nil, model.ErrForbidden(field + " exceeds the permitted limit")
	}
	return requested, nil
}
