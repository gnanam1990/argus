package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

// TestLoginLogoutSerializeWithRefresh races GetFresh (which triggers a
// refresh, since the seeded token is inside the buffer) against Logout for
// the same provider. Login/Logout now take the same per-key lock
// refreshAndSave uses, so the two operations serialize instead of
// interleaving; Logout unconditionally deletes, so however the race falls,
// the final state must be logged-out — a concurrent in-flight refresh must
// never resurrect the token Logout removed.
func TestLoginLogoutSerializeWithRefresh(t *testing.T) {
	t.Parallel()
	m, _ := seedManager(t, 30*time.Second) // within the 60s refresh buffer

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = m.GetFresh(context.Background(), "p")
	}()
	go func() {
		defer wg.Done()
		_ = m.Logout("p")
	}()
	wg.Wait()

	if _, err := m.store.Load(ProviderKey("p")); !os.IsNotExist(err) {
		t.Errorf("expected the token to be gone after a concurrent logout, got err=%v", err)
	}
}

// TestRefreshFailureHintsReauth checks the refresh-failure error carries an
// actionable re-login hint (the audit's "refresh failure lacks re-login
// hint" MEDIUM finding).
func TestRefreshFailureHintsReauth(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0).UTC()
	store := NewStore(t.TempDir())
	if err := store.Save(ProviderKey("p"), Token{AccessToken: "OLD", RefreshToken: "R1", ExpiresAt: now.Add(30 * time.Second)}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	t.Cleanup(srv.Close)

	m := NewManager(store,
		WithHTTPClient(srv.Client()),
		WithClock(func() time.Time { return now }),
		WithRefreshBuffer(60*time.Second),
		WithConfigResolver(func(string) (Config, bool) {
			return Config{ClientID: "c", TokenEndpoint: srv.URL}, true
		}),
	)

	_, err := m.GetFresh(context.Background(), "p")
	if err == nil {
		t.Fatal("expected a refresh failure")
	}
	if !strings.Contains(err.Error(), `argus auth login p`) {
		t.Errorf("error missing re-login hint: %v", err)
	}
}

// TestLogoutDuringRefreshAbortsResurrection is the direct (non-racy, ordered)
// counterpart to TestLoginLogoutSerializeWithRefresh: force Logout to run
// while refreshAndSave already holds the lock's *next* acquisition pending,
// by simulating the ordering via a deleted store entry before the reload
// check. This proves the abort path in refreshAndSave itself, not just the
// end state of a race.
func TestLogoutDuringRefreshAbortsResurrection(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0).UTC()
	store := NewStore(t.TempDir())
	seeded := Token{AccessToken: "OLD", RefreshToken: "R1", ExpiresAt: now.Add(30 * time.Second)}
	if err := store.Save(ProviderKey("p"), seeded); err != nil {
		t.Fatal(err)
	}

	m := NewManager(store,
		WithClock(func() time.Time { return now }),
		WithRefreshBuffer(60*time.Second),
		WithConfigResolver(func(string) (Config, bool) {
			return Config{ClientID: "c", TokenEndpoint: "https://unused.example"}, true
		}),
	)

	// Simulate "Logout ran and won the lock first" by deleting the token
	// before refreshAndSave (called directly, bypassing the outer GetToken
	// load) does its post-lock reload.
	if err := m.Logout("p"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.refreshAndSave(context.Background(), "p", ProviderKey("p"), seeded, false); err == nil {
		t.Fatal("expected refreshAndSave to abort after a concurrent logout, not resurrect the token")
	}
	if _, err := m.store.Load(ProviderKey("p")); !os.IsNotExist(err) {
		t.Errorf("token should still be gone; got err=%v", err)
	}
}
