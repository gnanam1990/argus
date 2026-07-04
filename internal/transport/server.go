package transport

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Authenticator decides whether a request is permitted.
type Authenticator interface {
	Authenticate(r *http.Request) bool
}

// NoAuth permits everything — for a localhost-bound guest in a trusted sandbox.
type NoAuth struct{}

// Authenticate always returns true.
func (NoAuth) Authenticate(*http.Request) bool { return true }

// BearerToken authenticates an Authorization: Bearer <token> header and supports
// rotation (a cloud sandbox rotates the token out of band).
type BearerToken struct {
	mu    sync.RWMutex
	token string
}

// NewBearerToken builds a bearer authenticator.
func NewBearerToken(token string) *BearerToken { return &BearerToken{token: token} }

// Rotate replaces the accepted token.
func (b *BearerToken) Rotate(token string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.token = token
}

// Authenticate checks the bearer token using a constant-time comparison, so
// the response timing does not leak how many leading bytes of a guessed
// token were correct.
func (b *BearerToken) Authenticate(r *http.Request) bool {
	b.mu.RLock()
	token := b.token
	b.mu.RUnlock()
	if token == "" {
		return false
	}
	want := "Bearer " + token
	got := r.Header.Get("Authorization")
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// Server serves the command protocol over HTTP: POST /cmd, GET /status,
// GET /commands. Wrap it in an http.Server with TLS for WSS-equivalent
// transport security; bind to localhost by default.
type Server struct {
	handler  Handler
	auth     Authenticator
	limiter  *limiter
	audit    *slog.Logger
	commands []string
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithAuth sets the authenticator (default NoAuth).
func WithAuth(a Authenticator) ServerOption { return func(s *Server) { s.auth = a } }

// WithRateLimit caps commands to max per window.
func WithRateLimit(max int, window time.Duration) ServerOption {
	return func(s *Server) { s.limiter = newLimiter(max, window, time.Now) }
}

// WithClock injects the rate limiter's clock (for tests).
func WithClock(now func() time.Time) ServerOption {
	return func(s *Server) {
		if s.limiter != nil {
			s.limiter.now = now
		}
	}
}

// WithAuditLogger sets the per-command audit logger (default slog.Default).
func WithAuditLogger(l *slog.Logger) ServerOption { return func(s *Server) { s.audit = l } }

// WithCommands sets the list returned by GET /commands.
func WithCommands(cmds ...string) ServerOption { return func(s *Server) { s.commands = cmds } }

// NewServer builds a command server.
func NewServer(h Handler, opts ...ServerOption) *Server {
	s := &Server{handler: h, auth: NoAuth{}, audit: slog.Default()}
	for _, o := range opts {
		o(s)
	}
	return s
}

// ServeHTTP routes the protocol endpoints. Every endpoint — not just /cmd —
// passes the same Authenticate check: /status and /commands still leak
// whether the guest is up and what it can do, which is meaningful
// reconnaissance against an RCE-shaped server (H8-adjacent hardening).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/status":
		if !s.authenticate(w, r) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case r.Method == http.MethodGet && r.URL.Path == "/commands":
		if !s.authenticate(w, r) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"commands": s.commands})
	case r.Method == http.MethodPost && r.URL.Path == "/cmd":
		if !s.authenticate(w, r) {
			return
		}
		s.handleCmd(w, r)
	default:
		http.NotFound(w, r)
	}
}

// authenticate writes a 401 and returns false when r fails the configured
// Authenticator; callers return immediately when it reports false.
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) bool {
	if s.auth.Authenticate(r) {
		return true
	}
	writeJSON(w, http.StatusUnauthorized, Response{OK: false, Error: "unauthorized"})
	return false
}

func (s *Server) handleCmd(w http.ResponseWriter, r *http.Request) {
	if s.limiter != nil && !s.limiter.allow() {
		writeJSON(w, http.StatusTooManyRequests, Response{OK: false, Error: "rate limited"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, Response{OK: false, Error: "read error"})
		return
	}
	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, Response{OK: false, Error: "invalid request"})
		return
	}

	// Audit the command name + correlation id — never the params (may hold secrets).
	s.audit.Info("guest.command", "command", req.Command, "id", req.ID, "trace_id", req.TraceID)

	resp := s.handler.Handle(r.Context(), req)
	resp.ID = req.ID
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// limiter is a token-bucket rate limiter with an injectable clock: capacity
// equals max, refilling continuously at a rate of max tokens per window.
// Unlike a fixed window, there is no instant at which the counter resets and
// a full new quota becomes available all at once — regenerated capacity is
// strictly proportional to elapsed time — so a burst can never exceed max,
// including for requests that straddle what would have been a fixed window's
// boundary.
type limiter struct {
	mu       sync.Mutex
	max      float64
	window   time.Duration
	now      func() time.Time
	tokens   float64
	lastTime time.Time
}

func newLimiter(max int, window time.Duration, now func() time.Time) *limiter {
	// tokens starts full so an initial burst of up to max is allowed right
	// away; lastTime is initialized lazily on the first allow() so a clock
	// injected after construction (WithClock) governs refill from the start.
	return &limiter{max: float64(max), window: window, now: now, tokens: float64(max)}
}

func (l *limiter) allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.now()
	if l.lastTime.IsZero() {
		l.lastTime = t
	}
	if elapsed := t.Sub(l.lastTime); elapsed > 0 {
		l.tokens += elapsed.Seconds() * (l.max / l.window.Seconds())
		if l.tokens > l.max {
			l.tokens = l.max
		}
		l.lastTime = t
	}
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}
