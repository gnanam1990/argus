package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gnanam1990/argus/internal/oauth"
)

// These tests set ARGUS_OAUTH_HOME via t.Setenv, so they cannot be parallel.

func TestAuthStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ARGUS_OAUTH_HOME", dir)

	store := oauth.NewStore("")
	if err := store.Save(oauth.ProviderKey("xai"), oauth.Token{AccessToken: "a", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	var buf strings.Builder
	if err := run([]string{"auth", "status"}, &buf); err != nil {
		t.Fatalf("status: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "xai") || !strings.Contains(out, "logged in") {
		t.Errorf("status missing xai login:\n%s", out)
	}
	if !strings.Contains(out, "chatgpt") || !strings.Contains(out, "not logged in") {
		t.Errorf("status should show chatgpt as not logged in:\n%s", out)
	}
	// A token value must never appear.
	if strings.Contains(out, "\"a\"") {
		t.Error("status leaked a token")
	}
}

func TestAuthLoginGatedByOptIn(t *testing.T) {
	t.Setenv("ARGUS_OAUTH_ALLOW_PRESETS", "")
	var buf strings.Builder
	err := run([]string{"auth", "login", "chatgpt"}, &buf)
	if err == nil {
		t.Fatal("login should be gated without ARGUS_OAUTH_ALLOW_PRESETS=1")
	}
	if !strings.Contains(buf.String(), "may violate") {
		t.Errorf("login should print the ToS caveat:\n%s", buf.String())
	}
}

func TestAuthLogout(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ARGUS_OAUTH_HOME", dir)
	store := oauth.NewStore("")
	_ = store.Save(oauth.ProviderKey("xai"), oauth.Token{AccessToken: "a"})

	var buf strings.Builder
	if err := run([]string{"auth", "logout", "xai"}, &buf); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(oauth.ProviderKey("xai")); err == nil {
		t.Error("token should be gone after logout")
	}
}

// captureWriter calls fn with each Write's bytes (as a string), then discards
// them — used to intercept the printed authorize URL without a real terminal
// or browser.
type captureWriter struct{ fn func(string) }

func (c captureWriter) Write(p []byte) (int, error) {
	c.fn(string(p))
	return len(p), nil
}

// TestLoginLoopbackExchangeOutlivesWaitDeadline proves the split-deadline fix:
// the code exchange must be derived from the ORIGINAL (non-shortened) parent
// context, not from the (short-lived, here) browser-wait context, so a slow
// human doesn't also starve the exchange.
func TestLoginLoopbackExchangeOutlivesWaitDeadline(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slower than waitCtx's budget below — if the exchange incorrectly
		// inherited waitCtx (or a context derived from it), this response
		// would arrive too late and the exchange would fail.
		time.Sleep(300 * time.Millisecond)
		_ = r.ParseForm()
		_, _ = w.Write([]byte(`{"access_token":"AT","token_type":"Bearer","expires_in":3600}`))
	}))
	t.Cleanup(srv.Close)

	cfg := oauth.Config{
		ClientID:              "cid",
		AuthorizationEndpoint: "https://auth.example/authorize",
		TokenEndpoint:         srv.URL,
	}

	urlCh := make(chan string, 1)
	out := captureWriter{fn: func(s string) {
		if i := strings.Index(s, cfg.AuthorizationEndpoint); i >= 0 {
			select {
			case urlCh <- strings.TrimSpace(s[i:]):
			default:
			}
		}
	}}

	// Deliver the loopback callback almost immediately, well within waitCtx's
	// tight budget, by parsing the authorize URL loginLoopback printed.
	go func() {
		raw := <-urlCh
		u, err := url.Parse(raw)
		if err != nil {
			return
		}
		redirect := u.Query().Get("redirect_uri")
		state := u.Query().Get("state")
		res, err := http.Get(redirect + "?code=code123&state=" + state)
		if err == nil {
			res.Body.Close()
		}
	}()

	waitCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	signalCtx := context.Background() // the "original, non-expired parent"

	tok, err := loginLoopback(waitCtx, signalCtx, out, cfg, true)
	if err != nil {
		t.Fatalf("loginLoopback: %v (exchange should have outlived the wait deadline)", err)
	}
	if tok.AccessToken != "AT" {
		t.Errorf("token = %+v, want access_token=AT", tok)
	}
}

func TestAuthUnknownSubcommand(t *testing.T) {
	var buf strings.Builder
	if err := run([]string{"auth", "frobnicate"}, &buf); err == nil {
		t.Error("expected error for unknown auth subcommand")
	}
}

func TestAuthUsage(t *testing.T) {
	var buf strings.Builder
	if err := run([]string{"auth"}, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "argus auth login") {
		t.Errorf("auth usage missing:\n%s", buf.String())
	}
}
