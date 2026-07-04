package omniparser

import (
	"net/http"
	"testing"
	"time"
)

// TestNewDefaultTimeout checks New sets its own bounded default client
// rather than sharing the mutable http.DefaultClient.
func TestNewDefaultTimeout(t *testing.T) {
	t.Parallel()
	c := New("http://example.invalid")
	if c.http == http.DefaultClient {
		t.Fatal("New must not share the global http.DefaultClient")
	}
	if c.http.Timeout != defaultTimeout {
		t.Errorf("default timeout = %v, want %v", c.http.Timeout, defaultTimeout)
	}
}

// TestNewHTTPClientOverride checks WithHTTPClient fully replaces the default.
func TestNewHTTPClientOverride(t *testing.T) {
	t.Parallel()
	custom := &http.Client{Timeout: 3 * time.Second}
	c := New("http://example.invalid", WithHTTPClient(custom))
	if c.http != custom {
		t.Error("WithHTTPClient should override the client")
	}
}
