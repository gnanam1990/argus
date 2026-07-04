package codex

import (
	"net/http"
	"testing"
	"time"
)

// TestDefaultHTTPClientHasHeaderTimeoutNoBodyTimeout covers H5: New(), absent
// WithHTTPClient, must not fall back to http.DefaultClient (no timeout at
// all — a hung server would wedge the run forever). It must bound only the
// wait for response headers, never the overall request (the Responses/chat
// endpoints stream a long-lived body).
func TestDefaultHTTPClientHasHeaderTimeoutNoBodyTimeout(t *testing.T) {
	t.Parallel()
	p := New()
	if p.http == nil {
		t.Fatal("New() left http nil")
	}
	if p.http.Timeout != 0 {
		t.Errorf("Client.Timeout = %s, want 0 (would cut off long SSE streams)", p.http.Timeout)
	}
	tr, ok := p.http.Transport.(*http.Transport)
	if !ok || tr == nil {
		t.Fatalf("Transport = %#v, want *http.Transport", p.http.Transport)
	}
	if tr.ResponseHeaderTimeout != 60*time.Second {
		t.Errorf("ResponseHeaderTimeout = %s, want 60s", tr.ResponseHeaderTimeout)
	}
}

// TestWithHTTPClientOverridesDefault confirms an injected client bypasses
// defaultHTTPClient entirely (needed so tests/integrations can use their own
// transport, e.g. a shorter timeout).
func TestWithHTTPClientOverridesDefault(t *testing.T) {
	t.Parallel()
	custom := &http.Client{Timeout: 5 * time.Second}
	p := New(WithHTTPClient(custom))
	if p.http != custom {
		t.Error("WithHTTPClient did not override the default client")
	}
}
