package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/gnanam1990/argus/internal/oauth"
)

// knownOAuthProviders is the set shown by `argus auth status` with no argument.
var knownOAuthProviders = []string{"chatgpt", "xai"}

func authCmd(args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(out, authUsage)
		return nil
	}
	switch args[0] {
	case "login":
		return authLogin(args[1:], out)
	case "status":
		return authStatus(args[1:], out)
	case "logout":
		return authLogout(args[1:], out)
	default:
		return fmt.Errorf("auth: unknown subcommand %q", args[0])
	}
}

func authLogin(args []string, out io.Writer) error {
	var provider string
	device, noBrowser := false, false
	for _, a := range args {
		switch a {
		case "--device":
			device = true
		case "--no-browser":
			noBrowser = true
		default:
			if provider == "" {
				provider = a
			}
		}
	}
	if provider == "" {
		return fmt.Errorf("auth login: a provider is required (e.g. argus auth login chatgpt)")
	}
	if !oauth.PresetsAllowed(os.Getenv) {
		fmt.Fprint(out, oauthCaveat)
		return fmt.Errorf("auth login: OAuth presets are opt-in; set ARGUS_OAUTH_ALLOW_PRESETS=1 to proceed")
	}
	cfg, ok := oauth.Preset(provider, os.Getenv)
	if !ok {
		return fmt.Errorf("auth login: no OAuth preset for %q", provider)
	}
	fmt.Fprint(out, oauthCaveat)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	mgr := oauth.NewManager(oauth.NewStore(""))

	var tok oauth.Token
	var err error
	if device || cfg.DeviceAuthorizationEndpoint != "" && noBrowser {
		tok, err = loginDevice(ctx, out, cfg)
	} else {
		tok, err = loginLoopback(ctx, out, cfg, noBrowser)
	}
	if err != nil {
		return err
	}
	if err := mgr.Login(provider, tok); err != nil {
		return err
	}
	when := "no expiry"
	if !tok.ExpiresAt.IsZero() {
		when = "expires " + tok.ExpiresAt.Local().Format(time.RFC1123)
	}
	fmt.Fprintf(out, "logged in to %s (%s)\n", provider, when)
	return nil
}

func loginLoopback(ctx context.Context, out io.Writer, cfg oauth.Config, noBrowser bool) (oauth.Token, error) {
	state, err := oauth.NewState()
	if err != nil {
		return oauth.Token{}, err
	}
	pkce, err := oauth.NewPKCE()
	if err != nil {
		return oauth.Token{}, err
	}
	l, err := oauth.NewLoopbackListenerOnPort(cfg.RedirectPort, state, cfg.RedirectPath)
	if err != nil {
		return oauth.Token{}, err
	}
	defer l.Close()

	// The redirect_uri must be byte-identical in the authorize request and the
	// code exchange, and must match the host the provider registered.
	redirectURI := l.RedirectURIForHost(cfg.RedirectHost)
	authURL, err := oauth.BuildAuthorizationURL(cfg, redirectURI, state, pkce)
	if err != nil {
		return oauth.Token{}, err
	}
	if noBrowser {
		fmt.Fprintln(out, "open this URL to authorize:\n ", authURL)
	} else if err := oauth.OpenURL(authURL); err != nil {
		fmt.Fprintln(out, "could not open a browser; open this URL to authorize:\n ", authURL)
	} else {
		fmt.Fprintln(out, "opened your browser to authorize; waiting…")
	}

	code, err := l.Wait(ctx)
	if err != nil {
		return oauth.Token{}, err
	}
	return oauth.ExchangeCode(ctx, nil, cfg, code, redirectURI, pkce.Verifier, nil)
}

func loginDevice(ctx context.Context, out io.Writer, cfg oauth.Config) (oauth.Token, error) {
	auth, err := oauth.RequestDeviceCode(ctx, nil, cfg, nil)
	if err != nil {
		return oauth.Token{}, err
	}
	uri := auth.VerificationURIComplete
	if uri == "" {
		uri = auth.VerificationURI
	}
	fmt.Fprintf(out, "to authorize, visit %s and enter code: %s\n", uri, auth.UserCode)
	return oauth.PollDeviceToken(ctx, nil, cfg, auth, nil)
}

func authStatus(args []string, out io.Writer) error {
	providers := args
	if len(providers) == 0 {
		providers = knownOAuthProviders
	}
	store := oauth.NewStore("")
	missing := 0
	for _, p := range providers {
		tok, err := store.Load(oauth.ProviderKey(p))
		if err != nil {
			fmt.Fprintf(out, "%-10s not logged in\n", p)
			missing++
			continue
		}
		exp := "no expiry"
		if !tok.ExpiresAt.IsZero() {
			exp = tok.ExpiresAt.Local().Format(time.RFC1123)
		}
		acct := ""
		if tok.Account != "" {
			acct = " account=" + tok.Account
		}
		fmt.Fprintf(out, "%-10s logged in  expires=%s%s\n", p, exp, acct)
	}
	if missing == len(providers) {
		return fmt.Errorf("auth status: no providers logged in")
	}
	return nil
}

func authLogout(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("auth logout: a provider is required")
	}
	if err := oauth.NewManager(oauth.NewStore("")).Logout(args[0]); err != nil {
		return err
	}
	fmt.Fprintf(out, "logged out of %s\n", args[0])
	return nil
}

const authUsage = `argus auth - OAuth-subscription logins (opt-in: ARGUS_OAUTH_ALLOW_PRESETS=1)

Usage:
  argus auth login <provider> [--device] [--no-browser]
  argus auth status [<provider>]
  argus auth logout <provider>

Providers: chatgpt, xai. See docs/oauth-subscriptions.md for the ToS caveat.
`

const oauthCaveat = `note: this reuses a public, undocumented CLI client identity and may violate
the provider's terms if used outside its sanctioned client. Tokens are stored
encrypted locally. The stable, ToS-safe path for automation is an API key.
`
