package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func seedManager(t *testing.T, expiresIn time.Duration) (*Manager, *int64) {
	t.Helper()
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)
		_, _ = w.Write([]byte(`{"access_token":"NEW","refresh_token":"R2","token_type":"Bearer","expires_in":3600}`))
	}))
	t.Cleanup(srv.Close)

	now := time.Unix(1_700_000_000, 0).UTC()
	store := NewStore(t.TempDir())
	_ = store.Save(ProviderKey("p"), Token{AccessToken: "OLD", RefreshToken: "R1", ExpiresAt: now.Add(expiresIn)})

	m := NewManager(store,
		WithHTTPClient(srv.Client()),
		WithClock(func() time.Time { return now }),
		WithRefreshBuffer(60*time.Second),
		WithConfigResolver(func(string) (Config, bool) {
			return Config{ClientID: "c", TokenEndpoint: srv.URL}, true
		}),
	)
	return m, &hits
}

func TestManagerRefreshesNearExpiry(t *testing.T) {
	t.Parallel()
	m, hits := seedManager(t, 30*time.Second) // within the 60s buffer
	tok, err := m.GetFresh(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	if tok != "NEW" {
		t.Errorf("token = %q, want NEW", tok)
	}
	if atomic.LoadInt64(hits) != 1 {
		t.Errorf("token endpoint hits = %d, want 1", *hits)
	}
}

func TestManagerSkipsWhenFresh(t *testing.T) {
	t.Parallel()
	m, hits := seedManager(t, time.Hour) // well outside the buffer
	tok, err := m.GetFresh(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	if tok != "OLD" || atomic.LoadInt64(hits) != 0 {
		t.Errorf("token=%q hits=%d; want OLD/0 (no refresh)", tok, *hits)
	}
}

func TestManagerSingleFlight(t *testing.T) {
	t.Parallel()
	m, hits := seedManager(t, 30*time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.GetFresh(context.Background(), "p")
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt64(hits); got != 1 {
		t.Errorf("concurrent refresh hit the endpoint %d times, want 1", got)
	}
}

func TestManagerNotLoggedIn(t *testing.T) {
	t.Parallel()
	m := NewManager(NewStore(t.TempDir()))
	if _, err := m.GetFresh(context.Background(), "nope"); err == nil {
		t.Error("expected a not-logged-in error")
	}
}
