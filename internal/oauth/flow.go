package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// reservedAuthParams cannot be overridden by a preset's ExtraAuthParams.
var reservedAuthParams = map[string]bool{
	"response_type": true, "client_id": true, "redirect_uri": true,
	"state": true, "code_challenge": true, "code_challenge_method": true, "scope": true,
}

// BuildAuthorizationURL constructs the authorization-code + PKCE authorize URL.
func BuildAuthorizationURL(cfg Config, redirectURI, state string, p PKCE) (string, error) {
	if p.Method != MethodS256 {
		return "", ErrPKCEDowngrade
	}
	u, err := url.Parse(cfg.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("oauth: invalid authorization endpoint: %w", err)
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	q.Set("code_challenge", p.Challenge)
	q.Set("code_challenge_method", p.Method)
	q.Set("scope", strings.Join(cfg.Scopes, " "))
	for k, v := range cfg.ExtraAuthParams {
		if !reservedAuthParams[k] {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// ExchangeCode exchanges an authorization code for a token.
func ExchangeCode(ctx context.Context, httpc *http.Client, cfg Config, code, redirectURI, verifier string, now func() time.Time) (Token, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", cfg.ClientID)
	form.Set("code_verifier", verifier)
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}
	return postToken(ctx, httpc, cfg.TokenEndpoint, form, current(now), Token{})
}

// Refresh exchanges a refresh token for a new access token, preserving the
// existing refresh token if the response omits one.
func Refresh(ctx context.Context, httpc *http.Client, cfg Config, current Token, now func() time.Time) (Token, error) {
	if current.RefreshToken == "" {
		return Token{}, ErrNoRefreshToken
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", current.RefreshToken)
	form.Set("client_id", cfg.ClientID)
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}
	return postToken(ctx, httpc, cfg.TokenEndpoint, form, nowOrDefault(now)(), current)
}

// postToken POSTs a form to the token endpoint and maps the response. prev is
// the token being refreshed (nil-equivalent for exchange) so a rotated-out
// refresh token is preserved when the provider does not return a new one.
func postToken(ctx context.Context, httpc *http.Client, endpoint string, form url.Values, now time.Time, prev Token) (Token, error) {
	if err := validateTokenEndpoint(endpoint); err != nil {
		return Token{}, err
	}
	if httpc == nil {
		httpc = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, fmt.Errorf("oauth: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	res, err := httpc.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("oauth: token request: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, tokenResponseLimit))

	var tr tokenResponse
	_ = json.Unmarshal(body, &tr)
	if tr.Error != "" {
		return Token{}, fmt.Errorf("oauth: token endpoint error %q", tr.Error)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Token{}, fmt.Errorf("oauth: token endpoint returned HTTP %d", res.StatusCode)
	}
	tok := tr.toToken(now)
	if tok.RefreshToken == "" {
		tok.RefreshToken = prev.RefreshToken
	}
	return tok, nil
}

func nowOrDefault(now func() time.Time) func() time.Time {
	if now == nil {
		return time.Now
	}
	return now
}

func current(now func() time.Time) time.Time { return nowOrDefault(now)() }
