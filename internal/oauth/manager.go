package oauth

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// Manager returns a valid access token for a provider, refreshing on demand.
// Refresh is single-flight per provider (an in-process per-key mutex plus a
// post-lock reload) so a rotating refresh token is not spent twice concurrently.
// (A cross-process file lock is a documented hardening follow-up.)
type Manager struct {
	store   *Store
	client  *http.Client
	now     func() time.Time
	buffer  time.Duration
	resolve func(name string) (Config, bool)

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// ManagerOption configures a Manager.
type ManagerOption func(*Manager)

// WithHTTPClient overrides the HTTP client.
func WithHTTPClient(c *http.Client) ManagerOption { return func(m *Manager) { m.client = c } }

// WithClock injects the clock (for tests).
func WithClock(now func() time.Time) ManagerOption { return func(m *Manager) { m.now = now } }

// WithRefreshBuffer sets how far before expiry a refresh is triggered.
func WithRefreshBuffer(d time.Duration) ManagerOption { return func(m *Manager) { m.buffer = d } }

// WithConfigResolver sets how a provider name resolves to a Config (default:
// the preset table read from the environment).
func WithConfigResolver(fn func(name string) (Config, bool)) ManagerOption {
	return func(m *Manager) { m.resolve = fn }
}

// NewManager builds a token manager over store.
func NewManager(store *Store, opts ...ManagerOption) *Manager {
	m := &Manager{
		store:  store,
		client: http.DefaultClient,
		now:    time.Now,
		buffer: defaultRefreshBuffer,
		locks:  map[string]*sync.Mutex{},
	}
	m.resolve = func(name string) (Config, bool) { return Preset(name, os.Getenv) }
	for _, o := range opts {
		o(m)
	}
	return m
}

// Login persists a token for provider name. It takes the same per-key lock as
// refreshAndSave so a fresh login can't interleave with (be clobbered by, or
// clobber) an in-flight refresh for the same provider.
func (m *Manager) Login(name string, t Token) error {
	key := ProviderKey(name)
	lock := m.keyLock(key)
	lock.Lock()
	defer lock.Unlock()
	return m.store.Save(key, t)
}

// Logout removes the stored token for provider name. It takes the same
// per-key lock as refreshAndSave so a concurrent in-flight refresh can't
// resurrect the token this call is deleting.
func (m *Manager) Logout(name string) error {
	key := ProviderKey(name)
	lock := m.keyLock(key)
	lock.Lock()
	defer lock.Unlock()
	return m.store.Delete(key)
}

// GetToken returns a valid (refreshed-if-needed) token for provider name.
func (m *Manager) GetToken(ctx context.Context, name string) (Token, error) {
	key := ProviderKey(name)
	tok, err := m.store.Load(key)
	if err != nil {
		if os.IsNotExist(err) {
			return Token{}, fmt.Errorf("oauth: not logged in to %q (run: argus auth login %s)", name, name)
		}
		return Token{}, err
	}
	if !tok.NeedsRefresh(m.now(), m.buffer) {
		return tok, nil
	}
	return m.refreshAndSave(ctx, name, key, tok, false)
}

// GetFresh returns a valid access-token string for provider name.
func (m *Manager) GetFresh(ctx context.Context, name string) (string, error) {
	tok, err := m.GetToken(ctx, name)
	if err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}

// ForceRefresh refreshes regardless of expiry (for a hard 401 retry).
func (m *Manager) ForceRefresh(ctx context.Context, name string) (Token, error) {
	key := ProviderKey(name)
	tok, err := m.store.Load(key)
	if err != nil {
		return Token{}, err
	}
	return m.refreshAndSave(ctx, name, key, tok, true)
}

func (m *Manager) refreshAndSave(ctx context.Context, name, key string, current Token, force bool) (Token, error) {
	lock := m.keyLock(key)
	lock.Lock()
	defer lock.Unlock()

	// Double-check: another goroutine may have refreshed — or logged out —
	// while we waited for the lock.
	reloaded, loadErr := m.store.Load(key)
	switch {
	case loadErr == nil:
		if reloaded.AccessToken != current.AccessToken {
			return reloaded, nil
		}
		if !force && !reloaded.NeedsRefresh(m.now(), m.buffer) {
			return reloaded, nil
		}
		current = reloaded
	case os.IsNotExist(loadErr):
		// Logged out while we waited for the lock: never resurrect a deleted
		// token by writing a freshly refreshed one back.
		return Token{}, fmt.Errorf("oauth: not logged in to %q (run: argus auth login %s)", name, name)
	}

	cfg, ok := m.resolve(name)
	if !ok {
		return Token{}, fmt.Errorf("oauth: no OAuth config for provider %q", name)
	}
	refreshed, err := Refresh(ctx, m.client, cfg, current, m.now)
	if err != nil {
		return Token{}, fmt.Errorf("oauth: refresh %s failed: %w (run \"argus auth login %s\" to re-authenticate)", name, err, name)
	}
	// Carry non-refreshable fields the token endpoint doesn't return.
	if refreshed.Account == "" {
		refreshed.Account = current.Account
	}
	if refreshed.IDToken == "" {
		refreshed.IDToken = current.IDToken
	}
	if err := m.store.Save(key, refreshed); err != nil {
		return Token{}, err
	}
	return refreshed, nil
}

func (m *Manager) keyLock(key string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.locks[key]
	if !ok {
		l = &sync.Mutex{}
		m.locks[key] = l
	}
	return l
}
