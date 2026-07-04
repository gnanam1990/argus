package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestDeviceFlow is not parallel: it swaps the package-level sleepFn to remove
// real delays.
func TestDeviceFlow(t *testing.T) {
	orig := sleepFn
	sleepFn = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	t.Cleanup(func() { sleepFn = orig })

	var polls int64
	mux := http.NewServeMux()
	mux.HandleFunc("/device", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"device_code":"DC","user_code":"UC","verification_uri":"https://v","expires_in":600,"interval":5}`))
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("grant_type") != deviceGrantType || r.PostForm.Get("device_code") != "DC" {
			t.Errorf("poll form = %v", r.PostForm)
		}
		n := atomic.AddInt64(&polls, 1)
		switch n {
		case 1:
			_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
		case 2:
			_, _ = w.Write([]byte(`{"error":"slow_down"}`))
		default:
			_, _ = w.Write([]byte(`{"access_token":"AT","token_type":"Bearer","expires_in":3600}`))
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := Config{ClientID: "c", DeviceAuthorizationEndpoint: srv.URL + "/device", TokenEndpoint: srv.URL + "/token"}

	auth, err := RequestDeviceCode(context.Background(), nil, cfg, fixedNow())
	if err != nil {
		t.Fatal(err)
	}
	if auth.DeviceCode != "DC" || auth.UserCode != "UC" {
		t.Errorf("device auth = %+v", auth)
	}
	auth.Interval = time.Millisecond // small; sleepFn is stubbed anyway

	tok, err := PollDeviceToken(context.Background(), nil, cfg, auth, fixedNow())
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "AT" {
		t.Errorf("token = %+v", tok)
	}
	if atomic.LoadInt64(&polls) != 3 {
		t.Errorf("polls = %d, want 3 (pending, slow_down, success)", polls)
	}
}

func TestDeviceNoEndpoint(t *testing.T) {
	t.Parallel()
	if _, err := RequestDeviceCode(context.Background(), nil, Config{}, nil); err != ErrNoDeviceEndpoint {
		t.Errorf("err = %v, want ErrNoDeviceEndpoint", err)
	}
}
