package webhook

import (
	"strings"
	"testing"
)

func TestSignFormat(t *testing.T) {
	sig := Sign([]byte("hello"), "secret123")

	// Must start with "sha256="
	if !strings.HasPrefix(sig, "sha256=") {
		t.Errorf("signature should start with 'sha256=', got %q", sig)
	}

	// Hex portion should be exactly 64 characters (256 bits / 4 bits per hex char)
	hexPart := strings.TrimPrefix(sig, "sha256=")
	if len(hexPart) != 64 {
		t.Errorf("hex portion should be 64 chars, got %d: %q", len(hexPart), hexPart)
	}
}

func TestSignDeterministic(t *testing.T) {
	payload := []byte("deterministic test payload")
	secret := "test-secret-key"

	sig1 := Sign(payload, secret)
	sig2 := Sign(payload, secret)

	if sig1 != sig2 {
		t.Errorf("Sign should be deterministic: %q != %q", sig1, sig2)
	}
}

func TestSignDifferentSecrets(t *testing.T) {
	payload := []byte("same payload")

	sig1 := Sign(payload, "secret-a")
	sig2 := Sign(payload, "secret-b")

	if sig1 == sig2 {
		t.Error("different secrets should produce different signatures")
	}
}

func TestSignDifferentPayloads(t *testing.T) {
	secret := "same-secret"

	sig1 := Sign([]byte("payload-a"), secret)
	sig2 := Sign([]byte("payload-b"), secret)

	if sig1 == sig2 {
		t.Error("different payloads should produce different signatures")
	}
}

func TestVerify(t *testing.T) {
	payload := []byte("verify roundtrip test")
	secret := "roundtrip-secret"

	sig := Sign(payload, secret)
	if !Verify(payload, secret, sig) {
		t.Error("Verify should return true for a valid signature")
	}
}

func TestVerifyWrongSignature(t *testing.T) {
	payload := []byte("verify test")
	secret := "correct-secret"

	if Verify(payload, secret, "sha256=wrong") {
		t.Error("Verify should return false for a tampered signature")
	}
}

func TestVerifyWrongSecret(t *testing.T) {
	payload := []byte("verify test")
	correctSecret := "correct-secret"
	wrongSecret := "wrong-secret"

	sig := Sign(payload, correctSecret)
	if Verify(payload, wrongSecret, sig) {
		t.Error("Verify should return false when using the wrong secret")
	}
}

func TestSignKnownVector(t *testing.T) {
	// Known HMAC-SHA256 test vector:
	// HMAC-SHA256("The quick brown fox jumps over the lazy dog", "key")
	// = f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8
	payload := []byte("The quick brown fox jumps over the lazy dog")
	secret := "key"
	expected := "sha256=f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8"

	got := Sign(payload, secret)
	if got != expected {
		t.Errorf("known vector mismatch:\n  expected: %s\n  got:      %s", expected, got)
	}
}

func TestVerifyEmptyPayload(t *testing.T) {
	secret := "empty-test"
	payload := []byte("")

	sig := Sign(payload, secret)
	if !Verify(payload, secret, sig) {
		t.Error("Verify should work with empty payload")
	}
}

func TestVerifyMalformedSignature(t *testing.T) {
	payload := []byte("test")
	secret := "secret"

	// Missing sha256= prefix
	if Verify(payload, secret, "not-a-real-signature") {
		t.Error("Verify should return false for malformed signature without sha256= prefix")
	}
}
