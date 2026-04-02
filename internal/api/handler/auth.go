package handler

import (
	"encoding/json"
	"net/http"
	"slices"

	"github.com/alexedwards/scs/v2"
	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2"

	"github.com/pwagstro/simple_llm_proxy/internal/auth"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// AuthLogin initiates the OIDC authorization code flow.
// Generates a random state and nonce, sets them as HttpOnly cookies, and redirects to the IdP.
func AuthLogin(oidcProvider *auth.OIDCProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if oidcProvider == nil {
			model.WriteError(w, model.ErrServiceUnavailable("OIDC not configured"))
			return
		}

		state, err := auth.RandString(16)
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to generate state"))
			return
		}
		nonce, err := auth.RandString(16)
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to generate nonce"))
			return
		}

		// PKCE: generate code verifier for S256 challenge (ADR 003 Decision 15)
		codeVerifier, err := auth.RandString(32)
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to generate PKCE verifier"))
			return
		}

		auth.SetCallbackCookie(w, r, "state", state)
		auth.SetCallbackCookie(w, r, "nonce", nonce)
		auth.SetCallbackCookie(w, r, "pkce", codeVerifier)

		redirectURL := oidcProvider.OAuth2Config.AuthCodeURL(state,
			oauth2.SetAuthURLParam("nonce", nonce),
			oauth2.S256ChallengeOption(codeVerifier),
		)
		http.Redirect(w, r, redirectURL, http.StatusFound)
	}
}

// AuthCallback handles the OIDC authorization callback.
// Validates state cookie, exchanges code, verifies ID token, and creates a session.
//
// Validation sequence (per ADR 003):
//  1. state cookie vs query param
//  2. nonce cookie read (before code exchange)
//  3. code exchange + id_token extraction
//  4. ID token verification (issuer/aud/expiry via go-oidc)
//  5. nonce cookie vs idToken.Nonce
//  6. email_verified claim
//  7. groups → isAdmin mapping
//  8. UpsertUser
//  9. RenewToken (session fixation mitigation) BEFORE Put("user_id")
func AuthCallback(oidcProvider *auth.OIDCProvider, store storage.Storage, sm *scs.SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if oidcProvider == nil {
			model.WriteError(w, model.ErrServiceUnavailable("OIDC not configured"))
			return
		}

		ctx := r.Context()

		// 1. Validate state
		stateCookie, err := r.Cookie("state")
		if err != nil {
			http.Error(w, "state cookie missing", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("state") != stateCookie.Value {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}

		// 2. Read nonce cookie before code exchange
		nonceCookie, err := r.Cookie("nonce")
		if err != nil {
			http.Error(w, "nonce cookie missing", http.StatusBadRequest)
			return
		}

		// 2b. Read PKCE verifier cookie
		pkceCookie, err := r.Cookie("pkce")
		if err != nil {
			http.Error(w, "PKCE verifier cookie missing", http.StatusBadRequest)
			return
		}

		// 3. Exchange code for token (with PKCE verifier per ADR 003 Decision 15)
		token, err := oidcProvider.OAuth2Config.Exchange(ctx, r.URL.Query().Get("code"),
			oauth2.VerifierOption(pkceCookie.Value),
		)
		if err != nil {
			log.Warn().Err(err).Msg("token exchange failed")
			http.Error(w, "token exchange failed", http.StatusBadRequest)
			return
		}

		// Extract raw ID token
		rawIDToken, ok := token.Extra("id_token").(string)
		if !ok || rawIDToken == "" {
			http.Error(w, "id_token missing from response", http.StatusBadRequest)
			return
		}

		// 4. Verify ID token (go-oidc/v3 validates issuer, audience, expiry automatically)
		idToken, err := oidcProvider.Verifier.Verify(ctx, rawIDToken)
		if err != nil {
			log.Warn().Err(err).Msg("ID token verification failed")
			http.Error(w, "ID token verification failed", http.StatusBadRequest)
			return
		}

		// 5. Verify nonce
		if idToken.Nonce != nonceCookie.Value {
			http.Error(w, "nonce mismatch", http.StatusBadRequest)
			return
		}

		// 6. Extract claims
		var claims struct {
			Sub           string   `json:"sub"`
			Email         string   `json:"email"`
			Name          string   `json:"name"`
			EmailVerified *bool    `json:"email_verified"`
			Groups        []string `json:"groups"`
		}
		if err := idToken.Claims(&claims); err != nil {
			http.Error(w, "failed to extract claims", http.StatusBadRequest)
			return
		}

		// Check email_verified
		if claims.EmailVerified != nil && !*claims.EmailVerified {
			http.Error(w, "email not verified", http.StatusForbidden)
			return
		}

		// 7. Handle missing groups claim — treat as non-admin, log WARN
		if claims.Groups == nil {
			claims.Groups = []string{}
			log.Warn().Str("sub", claims.Sub).Msg("groups claim absent — treating as non-admin")
		}

		// Determine isAdmin
		isAdmin := slices.Contains(claims.Groups, oidcProvider.AdminGroup)

		// 8. Upsert user
		if err := store.UpsertUser(ctx, &storage.User{
			ID:      claims.Sub,
			Email:   claims.Email,
			Name:    claims.Name,
			IsAdmin: isAdmin,
		}); err != nil {
			log.Error().Err(err).Str("sub", claims.Sub).Msg("failed to upsert user")
			model.WriteError(w, model.ErrInternal("failed to persist user"))
			return
		}

		// 9. Session fixation mitigation — MUST happen BEFORE Put
		if err := sm.RenewToken(ctx); err != nil {
			log.Error().Err(err).Msg("failed to renew session token")
			model.WriteError(w, model.ErrInternal("failed to create session"))
			return
		}

		// Set session
		sm.Put(ctx, "user_id", claims.Sub)

		// 10. Redirect to frontend dashboard
		http.Redirect(w, r, "/#/", http.StatusFound)
	}
}

// AuthLogout destroys the current session and redirects to the login page.
func AuthLogout(sm *scs.SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := sm.Destroy(r.Context()); err != nil {
			log.Warn().Err(err).Msg("failed to destroy session on logout")
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	}
}

// AdminMe returns the currently authenticated user's profile from the session.
func AdminMe(store storage.Storage, sm *scs.SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := sm.GetString(r.Context(), "user_id")
		if userID == "" {
			model.WriteError(w, model.ErrUnauthorized("not authenticated"))
			return
		}

		user, err := store.GetUser(r.Context(), userID)
		if err != nil || user == nil {
			model.WriteError(w, model.ErrUnauthorized("user not found"))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":       user.ID,
			"email":    user.Email,
			"name":     user.Name,
			"is_admin": user.IsAdmin,
		})
	}
}
