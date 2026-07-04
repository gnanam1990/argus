package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func fixedNow() func() time.Time {
	t := time.Unix(1_700_000_000, 0).UTC()
	return func() time.Time { return t }
}

func TestBuildAuthorizationURL(t *testing.T) {
	t.Parallel()
	cfg := Config{
		ClientID:              "cid",
		AuthorizationEndpoint: "https://auth.example/authorize",
		Scopes:                []string{"openid", "email"},
		ExtraAuthParams:       map[string]string{"prompt": "login", "client_id": "SPOOF"},
	}
	p := PKCE{Verifier: "v", Challenge: "chal", Method: MethodS256}
	raw, err := BuildAuthorizationURL(cfg, "http://127.0.0.1:1455/cb", "st8", p)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	q := u.Query()
	checks := map[string]string{
		"response_type": "code", "client_id": "cid", "redirect_uri": "http://127.0.0.1:1455/cb",
		"state": "st8", "code_challenge": "chal", "code_challenge_method": "S256",
		"scope": "openid email", "prompt": "login",
	}
	for k, want := range checks {
		if q.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, q.Get(k), want)
		}
	}
	// A reserved param in ExtraAuthParams cannot clobber the real one.
	if q.Get("client_id") == "SPOOF" {
		t.Error("ExtraAuthParams overrode a reserved param")
	}
}

func TestBuildAuthorizationURLRefusesPlain(t *testing.T) {
	t.Parallel()
	_, err := BuildAuthorizationURL(Config{AuthorizationEndpoint: "https://a"}, "r", "s", PKCE{Method: "plain"})
	if err != ErrPKCEDowngrade {
		t.Errorf("err = %v, want ErrPKCEDowngrade", err)
	}
}

func TestExchangeCode(t *testing.T) {
	t.Parallel()
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		_, _ = w.Write([]byte(`{"access_token":"AT","refresh_token":"RT","token_type":"Bearer","expires_in":3600,"scope":"a b","id_token":"IDT"}`))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{ClientID: "cid", TokenEndpoint: srv.URL}
	tok, err := ExchangeCode(context.Background(), nil, cfg, "code123", "http://127.0.0.1/cb", "verif", fixedNow())
	if err != nil {
		t.Fatal(err)
	}
	if gotForm.Get("grant_type") != "authorization_code" || gotForm.Get("code") != "code123" ||
		gotForm.Get("code_verifier") != "verif" || gotForm.Get("client_id") != "cid" {
		t.Errorf("exchange form = %v", gotForm)
	}
	if tok.AccessToken != "AT" || tok.RefreshToken != "RT" || tok.IDToken != "IDT" {
		t.Errorf("token = %+v", tok)
	}
	if !tok.ExpiresAt.Equal(fixedNow()().Add(time.Hour)) {
		t.Errorf("expiry = %v", tok.ExpiresAt)
	}
}

func TestRefresh(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("grant_type") != "refresh_token" || r.PostForm.Get("refresh_token") != "OLD" {
			t.Errorf("refresh form = %v", r.PostForm)
		}
		// No refresh_token in the response → the old one must be preserved.
		_, _ = w.Write([]byte(`{"access_token":"NEW","token_type":"Bearer","expires_in":3600}`))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{ClientID: "cid", TokenEndpoint: srv.URL}
	tok, err := Refresh(context.Background(), nil, cfg, Token{RefreshToken: "OLD"}, fixedNow())
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "NEW" || tok.RefreshToken != "OLD" {
		t.Errorf("refreshed token = %+v (refresh token should carry over)", tok)
	}
}

func TestRefreshPreservesScopesAndTokenType(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		// The response omits both token_type and scope entirely.
		_, _ = w.Write([]byte(`{"access_token":"NEW","expires_in":3600}`))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{ClientID: "cid", TokenEndpoint: srv.URL}
	prev := Token{RefreshToken: "OLD", TokenType: "Bearer", Scopes: []string{"openid", "profile"}}
	tok, err := Refresh(context.Background(), nil, cfg, prev, fixedNow())
	if err != nil {
		t.Fatal(err)
	}
	if tok.TokenType != "Bearer" {
		t.Errorf("token_type = %q, want preserved Bearer", tok.TokenType)
	}
	if len(tok.Scopes) != 2 || tok.Scopes[0] != "openid" || tok.Scopes[1] != "profile" {
		t.Errorf("scopes = %v, want preserved [openid profile]", tok.Scopes)
	}
}

func TestRefreshNoToken(t *testing.T) {
	t.Parallel()
	if _, err := Refresh(context.Background(), nil, Config{}, Token{}, nil); err != ErrNoRefreshToken {
		t.Errorf("err = %v, want ErrNoRefreshToken", err)
	}
}

func TestInsecureTokenEndpoint(t *testing.T) {
	t.Parallel()
	cfg := Config{ClientID: "c", TokenEndpoint: "http://evil.example/token"} // not https, not loopback
	if _, err := ExchangeCode(context.Background(), nil, cfg, "c", "r", "v", nil); err != ErrInsecureTokenEndpoint {
		t.Errorf("err = %v, want ErrInsecureTokenEndpoint", err)
	}
}
