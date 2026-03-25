package auth

import (
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"time"
)

// RandString generates a cryptographically secure random URL-safe string of nByte bytes.
func RandString(nByte int) (string, error) {
	b := make([]byte, nByte)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// SetCallbackCookie sets a short-lived HttpOnly cookie for OIDC state/nonce.
// Uses SameSite=Lax (not Strict) because the cookie must be sent on cross-origin redirects from PocketID.
func SetCallbackCookie(w http.ResponseWriter, r *http.Request, name, value string) {
	c := &http.Cookie{
		Name:     name,
		Value:    value,
		MaxAge:   int(time.Hour.Seconds()),
		Secure:   r.TLS != nil,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, c)
}
