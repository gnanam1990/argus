package transport

import (
	"net/http"
	"testing"
	"time"
)

// TestNewClientDefaultTimeout checks NewClient sets its own bounded default
// client rather than sharing the mutable http.DefaultClient.
func TestNewClientDefaultTimeout(t *testing.T) {
	t.Parallel()
	c := NewClient("http://example.invalid")
	if c.http == http.DefaultClient {
		t.Fatal("NewClient must not share the global http.DefaultClient")
	}
	if c.http.Timeout != defaultTimeout {
		t.Errorf("default client timeout = %v, want %v", c.http.Timeout, defaultTimeout)
	}
}

// TestNewClientHTTPClientOverride checks WithHTTPClient fully replaces the
// default (including its timeout).
func TestNewClientHTTPClientOverride(t *testing.T) {
	t.Parallel()
	custom := &http.Client{Timeout: 5 * time.Second}
	c := NewClient("http://example.invalid", WithHTTPClient(custom))
	if c.http != custom {
		t.Fatal("WithHTTPClient should override the client")
	}
	if c.http.Timeout != 5*time.Second {
		t.Errorf("overridden timeout = %v, want 5s", c.http.Timeout)
	}
}
