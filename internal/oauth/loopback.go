package oauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
)

// LoopbackListener is a 127.0.0.1 HTTP server that catches the OAuth redirect,
// validates the CSRF state, and hands back the authorization code.
type LoopbackListener struct {
	listener net.Listener
	state    string
	path     string
	result   chan callbackResult
	server   *http.Server
}

type callbackResult struct {
	code string
	err  error
}

// NewLoopbackListener binds an ephemeral loopback port.
func NewLoopbackListener(state, path string) (*LoopbackListener, error) {
	return NewLoopbackListenerOnPort(0, state, path)
}

// NewLoopbackListenerOnPort binds a specific loopback port (0 = ephemeral).
func NewLoopbackListenerOnPort(port int, state, path string) (*LoopbackListener, error) {
	if path == "" {
		path = "/callback"
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, fmt.Errorf("oauth: bind loopback: %w", err)
	}
	l := &LoopbackListener{listener: ln, state: state, path: path, result: make(chan callbackResult, 1)}
	l.server = &http.Server{Handler: http.HandlerFunc(l.handle)}
	go func() { _ = l.server.Serve(ln) }()
	return l, nil
}

// RedirectURI is the loopback redirect URI to register with the provider.
func (l *LoopbackListener) RedirectURI() string {
	return "http://" + l.listener.Addr().String() + l.path
}

// Port returns the actual bound loopback port.
func (l *LoopbackListener) Port() int {
	return l.listener.Addr().(*net.TCPAddr).Port
}

// RedirectURIForHost is RedirectURI but with an explicit hostname, for providers
// that registered "localhost" rather than "127.0.0.1" (the two are not
// interchangeable under OAuth's exact-match redirect rule). An empty host falls
// back to the listener's own address.
func (l *LoopbackListener) RedirectURIForHost(host string) string {
	if host == "" {
		return l.RedirectURI()
	}
	return fmt.Sprintf("http://%s:%d%s", host, l.Port(), l.path)
}

func (l *LoopbackListener) handle(w http.ResponseWriter, r *http.Request) {
	// Only the callback path carries the result; ignore favicon etc.
	if r.URL.Path != l.path && r.URL.Path != "/callback" && r.URL.Path != "/auth/callback" {
		http.NotFound(w, r)
		return
	}
	code, err := parseCallback(r.URL.Query(), l.state)
	if err != nil {
		fmt.Fprintln(w, "Authorization failed. You may close this window.")
	} else {
		fmt.Fprintln(w, "Authorization complete. You may close this window.")
	}
	select {
	case l.result <- callbackResult{code: code, err: err}:
	default:
	}
}

func parseCallback(v url.Values, wantState string) (string, error) {
	if e := v.Get("error"); e != "" {
		return "", fmt.Errorf("oauth: authorization error %q", e)
	}
	if v.Get("state") != wantState {
		return "", ErrStateMismatch
	}
	code := v.Get("code")
	if code == "" {
		return "", fmt.Errorf("oauth: callback missing authorization code")
	}
	return code, nil
}

// Wait blocks until the callback arrives or ctx is done.
func (l *LoopbackListener) Wait(ctx context.Context) (string, error) {
	select {
	case res := <-l.result:
		return res.code, res.err
	case <-ctx.Done():
		return "", fmt.Errorf("oauth: timed out waiting for authorization: %w", ctx.Err())
	}
}

// Close shuts the server down.
func (l *LoopbackListener) Close() error { return l.server.Close() }
