// Package oauth implements subscription-based OAuth for Argus providers (xAI and
// OpenAI/ChatGPT), adapted from the user's zero project but bound to Argus's own
// config and storage. It is self-contained — net/http + crypto/* only, no vendor
// SDK — and every network and clock seam is injectable, so the flows are tested
// hermetically against httptest with a fake clock.
//
// Security invariants (enforced by code + tests, not convention): tokens are
// never logged (Token redacts via String/GoString); tokens are stored
// AES-256-GCM encrypted at 0600 under a 0700 dir; PKCE S256 is mandatory; the
// token endpoint must be HTTPS (except loopback). Presets reuse public,
// undocumented CLI client identities and are opt-in behind
// ARGUS_OAUTH_ALLOW_PRESETS=1 — see docs/oauth-subscriptions.md for the ToS
// caveat.
package oauth

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Token is a stored OAuth credential for one provider. The JSON encoding is the
// plaintext sealed inside the on-disk AES-GCM blob.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	Scopes       []string  `json:"scopes,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Account      string    `json:"account,omitempty"`  // e.g. chatgpt-account-id (Stage C)
	IDToken      string    `json:"id_token,omitempty"` // OIDC id_token (JWT) for claim extraction
}

// Expired reports whether the access token is past its expiry.
func (t Token) Expired(now time.Time) bool {
	return !t.ExpiresAt.IsZero() && !t.ExpiresAt.After(now)
}

// NeedsRefresh reports whether the token expires within buffer of now.
func (t Token) NeedsRefresh(now time.Time, buffer time.Duration) bool {
	if t.ExpiresAt.IsZero() {
		return false
	}
	return !t.ExpiresAt.After(now.Add(buffer))
}

// String redacts the token so it never leaks via logging.
func (t Token) String() string { return "oauth.Token(redacted)" }

// GoString redacts the token so it never leaks via %#v.
func (t Token) GoString() string { return "oauth.Token(redacted)" }

// Config is a provider-agnostic OAuth client configuration; every field comes
// from a preset or an ARGUS_OAUTH_* override.
type Config struct {
	ClientID                    string
	ClientSecret                string // usually empty for public PKCE clients
	Scopes                      []string
	AuthorizationEndpoint       string
	TokenEndpoint               string
	DeviceAuthorizationEndpoint string
	IssuerURL                   string
	ExtraAuthParams             map[string]string
	RedirectPort                int    // 0 = ephemeral
	RedirectPath                string // default "/callback"
	// RedirectHost is the hostname advertised in the redirect_uri. It must match
	// exactly what the provider registered for the client (some register
	// "localhost", not "127.0.0.1"). Empty means use the listener's own address.
	RedirectHost string
}

// Sentinel errors.
var (
	ErrPKCEDowngrade         = errors.New("oauth: PKCE S256 is mandatory; plain is refused")
	ErrStateMismatch         = errors.New("oauth: callback state mismatch")
	ErrInsecureTokenEndpoint = errors.New("oauth: token endpoint must use https")
	ErrNoRefreshToken        = errors.New("oauth: no refresh token available")
	ErrAuthorizationPending  = errors.New("oauth: authorization pending")
	ErrSlowDown              = errors.New("oauth: slow down")
	ErrNoDeviceEndpoint      = errors.New("oauth: provider has no device authorization endpoint")
)

const (
	tokenResponseLimit   = 1 << 20 // 1 MiB cap on token-endpoint bodies
	defaultRefreshBuffer = 60 * time.Second
	deviceGrantType      = "urn:ietf:params:oauth:grant-type:device_code"
)

// tokenResponse is the shared wire shape for every token-endpoint response.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
	IDToken      string `json:"id_token"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// toToken maps a token-endpoint response into a Token, stamping expiry from now.
func (r tokenResponse) toToken(now time.Time) Token {
	t := Token{
		AccessToken:  r.AccessToken,
		RefreshToken: r.RefreshToken,
		TokenType:    r.TokenType,
		IDToken:      r.IDToken,
		Scopes:       strings.Fields(r.Scope),
	}
	if r.ExpiresIn > 0 {
		t.ExpiresAt = now.Add(time.Duration(r.ExpiresIn) * time.Second).UTC()
	}
	return t
}

// validateTokenEndpoint requires HTTPS unless the host is loopback.
func validateTokenEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("oauth: invalid token endpoint: %w", err)
	}
	if u.Scheme == "https" || isLoopbackHost(u.Hostname()) {
		return nil
	}
	return ErrInsecureTokenEndpoint
}

func isLoopbackHost(host string) bool {
	switch host {
	case "127.0.0.1", "::1", "localhost":
		return true
	default:
		return false
	}
}
