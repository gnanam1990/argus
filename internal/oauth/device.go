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

const (
	defaultDeviceLifetime = 10 * time.Minute
	defaultDeviceInterval = 5 * time.Second
	slowDownBackoff       = 5 * time.Second
)

// DeviceAuth is the device-authorization response (RFC 8628).
type DeviceAuth struct {
	DeviceCode              string
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	Interval                time.Duration
	ExpiresAt               time.Time
}

type deviceAuthResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
	Error                   string `json:"error"`
	ErrorDesc               string `json:"error_description"`
}

// RequestDeviceCode starts the device-authorization flow.
func RequestDeviceCode(ctx context.Context, httpc *http.Client, cfg Config, now func() time.Time) (DeviceAuth, error) {
	if cfg.DeviceAuthorizationEndpoint == "" {
		return DeviceAuth{}, ErrNoDeviceEndpoint
	}
	if httpc == nil {
		httpc = http.DefaultClient
	}
	form := url.Values{}
	form.Set("client_id", cfg.ClientID)
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}
	if len(cfg.Scopes) > 0 {
		form.Set("scope", strings.Join(cfg.Scopes, " "))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.DeviceAuthorizationEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return DeviceAuth{}, fmt.Errorf("oauth: build device request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	res, err := httpc.Do(req)
	if err != nil {
		return DeviceAuth{}, fmt.Errorf("oauth: device request: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, tokenResponseLimit))

	var dr deviceAuthResponse
	_ = json.Unmarshal(body, &dr)
	if dr.Error != "" {
		return DeviceAuth{}, fmt.Errorf("oauth: device authorization error %q", dr.Error)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return DeviceAuth{}, fmt.Errorf("oauth: device authorization returned HTTP %d", res.StatusCode)
	}
	if dr.DeviceCode == "" || dr.UserCode == "" {
		return DeviceAuth{}, fmt.Errorf("oauth: device authorization missing device/user code")
	}

	interval := time.Duration(dr.Interval) * time.Second
	if interval <= 0 {
		interval = defaultDeviceInterval
	}
	lifetime := time.Duration(dr.ExpiresIn) * time.Second
	if lifetime <= 0 {
		lifetime = defaultDeviceLifetime
	}
	return DeviceAuth{
		DeviceCode:              dr.DeviceCode,
		UserCode:                dr.UserCode,
		VerificationURI:         dr.VerificationURI,
		VerificationURIComplete: dr.VerificationURIComplete,
		Interval:                interval,
		ExpiresAt:               nowOrDefault(now)().Add(lifetime),
	}, nil
}

// sleepFn is the poll delay seam, swappable in tests.
var sleepFn = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// PollDeviceToken polls the token endpoint until authorization completes, the
// code expires, or ctx is cancelled.
func PollDeviceToken(ctx context.Context, httpc *http.Client, cfg Config, auth DeviceAuth, now func() time.Time) (Token, error) {
	clock := nowOrDefault(now)
	interval := auth.Interval
	if interval <= 0 {
		interval = defaultDeviceInterval
	}
	for {
		if !auth.ExpiresAt.IsZero() && !auth.ExpiresAt.After(clock()) {
			return Token{}, fmt.Errorf("oauth: device code expired before authorization")
		}
		if err := sleepFn(ctx, interval); err != nil {
			return Token{}, fmt.Errorf("oauth: device authorization canceled: %w", err)
		}
		tok, err := pollDeviceOnce(ctx, httpc, cfg, auth.DeviceCode, clock)
		switch {
		case err == nil:
			return tok, nil
		case err == ErrAuthorizationPending:
			// keep waiting
		case err == ErrSlowDown:
			interval += slowDownBackoff
		default:
			return Token{}, err
		}
	}
}

func pollDeviceOnce(ctx context.Context, httpc *http.Client, cfg Config, deviceCode string, now func() time.Time) (Token, error) {
	form := url.Values{}
	form.Set("grant_type", deviceGrantType)
	form.Set("device_code", deviceCode)
	form.Set("client_id", cfg.ClientID)
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}
	if err := validateTokenEndpoint(cfg.TokenEndpoint); err != nil {
		return Token{}, err
	}
	if httpc == nil {
		httpc = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, fmt.Errorf("oauth: build device poll: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	res, err := httpc.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("oauth: device poll: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, tokenResponseLimit))

	var tr tokenResponse
	_ = json.Unmarshal(body, &tr)
	if tr.Error != "" {
		switch tr.Error {
		case "authorization_pending":
			return Token{}, ErrAuthorizationPending
		case "slow_down":
			return Token{}, ErrSlowDown
		case "expired_token":
			return Token{}, fmt.Errorf("oauth: device code expired before authorization")
		case "access_denied":
			return Token{}, fmt.Errorf("oauth: authorization denied by the user")
		default:
			return Token{}, fmt.Errorf("oauth: device token error %q", tr.Error)
		}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 || tr.AccessToken == "" {
		return Token{}, fmt.Errorf("oauth: device token poll returned HTTP %d", res.StatusCode)
	}
	return tr.toToken(now()), nil
}
