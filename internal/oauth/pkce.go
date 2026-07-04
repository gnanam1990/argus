package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// MethodS256 is the only supported PKCE challenge method.
const MethodS256 = "S256"

const pkceVerifierBytes = 32

// PKCE is a Proof Key for Code Exchange pair.
type PKCE struct {
	Verifier  string
	Challenge string
	Method    string
}

// NewPKCE generates a fresh S256 PKCE pair.
func NewPKCE() (PKCE, error) {
	raw := make([]byte, pkceVerifierBytes)
	if _, err := rand.Read(raw); err != nil {
		return PKCE{}, fmt.Errorf("oauth: generate PKCE verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	return PKCE{Verifier: verifier, Challenge: challengeFor(verifier), Method: MethodS256}, nil
}

// challengeFor computes the S256 challenge: base64url(SHA-256(ASCII(verifier))).
// Per RFC 7636 the hash is over the ASCII bytes of the verifier STRING, not the
// original random bytes.
func challengeFor(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// NewState generates a fresh CSRF state value.
func NewState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("oauth: generate state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
