package main

import (
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
