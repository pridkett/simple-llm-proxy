package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// Sign computes the HMAC-SHA256 signature of payload using secret.
// Returns the signature in the format "sha256=<hex-digest>" per D-08.
// The signature is sent as the X-Webhook-Signature header value.
func Sign(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Verify checks that the given signature matches the expected HMAC-SHA256
// of payload using secret. Uses constant-time comparison via hmac.Equal
// to prevent timing attacks.
func Verify(payload []byte, secret, signature string) bool {
	expected := Sign(payload, secret)
	return hmac.Equal([]byte(expected), []byte(signature))
}
