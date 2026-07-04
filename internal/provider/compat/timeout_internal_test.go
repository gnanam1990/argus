package compat

import (
	"net/http"
	"testing"
	"time"
)

// TestDefaultHTTPClientHasHeaderTimeoutNoBodyTimeout covers H5: New(), absent
// WithHTTPClient, must not fall back to http.DefaultClient (no timeout at
// all — a hung server would wedge the run forever). It must bound only the
// wait for response headers, never the overall request.
func TestDefaultHTTPClientHasHeaderTimeoutNoBodyTimeout(t *testing.T) {
	t.Parallel()
	p := New()
	if p.http == nil {
		t.Fatal("New() left http nil")
	}
	if p.http.Timeout != 0 {
		t.Errorf("Client.Timeout = %s, want 0 (would cut off a long response body)", p.http.Timeout)
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
// defaultHTTPClient entirely.
func TestWithHTTPClientOverridesDefault(t *testing.T) {
	t.Parallel()
	custom := &http.Client{Timeout: 5 * time.Second}
	p := New(WithHTTPClient(custom))
	if p.http != custom {
		t.Error("WithHTTPClient did not override the default client")
	}
}

// TestUsesMaxCompletionTokensModelMatch exercises the model-id regex
// directly (white-box) since it is unexported: o-series and gpt-5.x match,
// trimming a leading "provider/" prefix; everything else keeps max_tokens.
func TestUsesMaxCompletionTokensModelMatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		model string
		want  bool
	}{
		{"gpt-4o", false},
		{"gpt-4-turbo", false},
		{"gpt-3.5-turbo", false},
		{"llama3", false},
		{"gpt-5.5", true},
		{"gpt-5", true},
		{"GPT-5-mini", true},
		{"o1", true},
		{"o3", true},
		{"o4-mini", true},
		{"openai/gpt-5.5", true},
		{"azure/o3", true},
		{"router/gpt-4o", false},
	}
	for _, tt := range tests {
		if got := usesMaxCompletionTokens(tt.model); got != tt.want {
			t.Errorf("usesMaxCompletionTokens(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}
