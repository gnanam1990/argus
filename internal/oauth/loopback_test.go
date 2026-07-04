package oauth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRedirectURIForHost(t *testing.T) {
	t.Parallel()
	l, err := NewLoopbackListenerOnPort(0, "STATE", "/auth/callback")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	// Empty host falls back to the listener's numeric address.
	if got := l.RedirectURIForHost(""); !strings.HasPrefix(got, "http://127.0.0.1:") {
		t.Errorf("empty host = %q", got)
	}
	// An explicit host is advertised verbatim on the real bound port.
	want := fmt.Sprintf("http://localhost:%d/auth/callback", l.Port())
	if got := l.RedirectURIForHost("localhost"); got != want {
		t.Errorf("RedirectURIForHost = %q, want %q", got, want)
	}
}

func TestLoopbackDeliversCode(t *testing.T) {
	t.Parallel()
	l, err := NewLoopbackListener("STATE", "/callback")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	res, err := http.Get(l.RedirectURI() + "?code=abc123&state=STATE")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	code, err := l.Wait(ctx)
	if err != nil || code != "abc123" {
		t.Errorf("Wait = %q, %v; want abc123", code, err)
	}
}

func TestLoopbackStateMismatchDroppedNotDelivered(t *testing.T) {
	t.Parallel()
	l, err := NewLoopbackListener("GOOD", "/callback")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	// A bogus/poisoning request with the wrong state arrives first. It must
	// be answered 400 and NOT fill the one-shot result channel (H8): the
	// listener has to keep waiting for the real callback.
	res, err := http.Get(l.RedirectURI() + "?code=evil&state=BAD")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "state mismatch") {
		t.Fatalf("bogus request = %d %q, want 400 \"state mismatch\"", res.StatusCode, body)
	}

	// The real callback (matching state) arrives second and must still be
	// delivered to Wait — proving the bogus request above was dropped rather
	// than consuming the single result slot.
	res2, err := http.Get(l.RedirectURI() + "?code=real123&state=GOOD")
	if err != nil {
		t.Fatal(err)
	}
	res2.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	code, err := l.Wait(ctx)
	if err != nil || code != "real123" {
		t.Errorf("Wait = %q, %v; want real123, nil", code, err)
	}
}

func TestLoopbackTimeout(t *testing.T) {
	t.Parallel()
	l, err := NewLoopbackListener("S", "/callback")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.Wait(ctx); err == nil {
		t.Error("expected timeout error on cancelled ctx")
	}
}
