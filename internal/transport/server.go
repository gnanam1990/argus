package transport

import (
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

// Authenticate checks the bearer token.
func (b *BearerToken) Authenticate(r *http.Request) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.token != "" && r.Header.Get("Authorization") == "Bearer "+b.token
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

// ServeHTTP routes the protocol endpoints.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/status":
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case r.Method == http.MethodGet && r.URL.Path == "/commands":
		writeJSON(w, http.StatusOK, map[string]any{"commands": s.commands})
	case r.Method == http.MethodPost && r.URL.Path == "/cmd":
		s.handleCmd(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleCmd(w http.ResponseWriter, r *http.Request) {
	if !s.auth.Authenticate(r) {
		writeJSON(w, http.StatusUnauthorized, Response{OK: false, Error: "unauthorized"})
		return
	}
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

// limiter is a fixed-window rate limiter with an injectable clock.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	now    func() time.Time
	count  int
	start  time.Time
}

func newLimiter(max int, window time.Duration, now func() time.Time) *limiter {
	// start is initialized lazily on first allow() so a clock injected after
	// construction (WithClock) governs the window.
	return &limiter{max: max, window: window, now: now}
}

func (l *limiter) allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.now()
	if l.start.IsZero() || t.Sub(l.start) >= l.window {
		l.start = t
		l.count = 0
	}
	if l.count >= l.max {
		return false
	}
	l.count++
	return true
}
