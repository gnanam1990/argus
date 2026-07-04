package transport_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gnanam1990/argus/internal/transport"
)

func echoHandler() transport.Handler {
	return transport.HandlerFunc(func(_ context.Context, req transport.Request) transport.Response {
		return transport.Result(req.ID, map[string]any{"echo": req.Command})
	})
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(transport.NewServer(echoHandler()))
	t.Cleanup(srv.Close)

	c := transport.NewClient(srv.URL)
	resp, err := c.Send(context.Background(), "hello", map[string]any{"a": 1})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("resp = %+v", resp)
	}
	var res map[string]string
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		t.Fatal(err)
	}
	if res["echo"] != "hello" {
		t.Errorf("echo = %q", res["echo"])
	}
}

func TestBearerAuth(t *testing.T) {
	t.Parallel()
	auth := transport.NewBearerToken("secret")
	srv := httptest.NewServer(transport.NewServer(echoHandler(), transport.WithAuth(auth)))
	t.Cleanup(srv.Close)

	// No token → unauthorized (non-2xx → client error).
	if _, err := transport.NewClient(srv.URL).Send(context.Background(), "x", nil); err == nil {
		t.Error("expected unauthorized error without token")
	}
	// Correct token → ok.
	c := transport.NewClient(srv.URL, transport.WithToken("secret"))
	if resp, err := c.Send(context.Background(), "x", nil); err != nil || !resp.OK {
		t.Errorf("authed send failed: %v %+v", err, resp)
	}
	// Rotate → old token rejected.
	auth.Rotate("new")
	if _, err := c.Send(context.Background(), "x", nil); err == nil {
		t.Error("rotated-out token should be rejected")
	}
}

func TestBearerAuthRejectsWrongLength(t *testing.T) {
	t.Parallel()
	auth := transport.NewBearerToken("secret-token")
	srv := httptest.NewServer(transport.NewServer(echoHandler(), transport.WithAuth(auth)))
	t.Cleanup(srv.Close)

	// A prefix and a suffix-extended token exercise the constant-time
	// compare's length-mismatch path — both must still be rejected.
	for _, tok := range []string{"secret", "secret-token-extra"} {
		c := transport.NewClient(srv.URL, transport.WithToken(tok))
		if _, err := c.Send(context.Background(), "x", nil); err == nil {
			t.Errorf("token %q should be rejected", tok)
		}
	}
}

func TestStatusAndCommandsRequireAuth(t *testing.T) {
	t.Parallel()
	auth := transport.NewBearerToken("secret")
	srv := httptest.NewServer(transport.NewServer(echoHandler(), transport.WithAuth(auth), transport.WithCommands("a")))
	t.Cleanup(srv.Close)

	for _, path := range []string{"/status", "/commands"} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without token = %d, want 401", path, res.StatusCode)
		}

		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer secret")
		res2, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res2.Body.Close()
		if res2.StatusCode != http.StatusOK {
			t.Errorf("%s with token = %d, want 200", path, res2.StatusCode)
		}
	}
}

func TestRateLimit(t *testing.T) {
	t.Parallel()
	now := time.Unix(0, 0)
	srv := httptest.NewServer(transport.NewServer(echoHandler(),
		transport.WithRateLimit(2, time.Second),
		transport.WithClock(func() time.Time { return now }),
	))
	t.Cleanup(srv.Close)

	c := transport.NewClient(srv.URL)
	send := func() error { _, err := c.Send(context.Background(), "x", nil); return err }

	if err := send(); err != nil {
		t.Fatalf("1st: %v", err)
	}
	if err := send(); err != nil {
		t.Fatalf("2nd: %v", err)
	}
	if err := send(); err == nil {
		t.Error("3rd request should be rate-limited")
	}
	// New window resets the counter.
	now = now.Add(2 * time.Second)
	if err := send(); err != nil {
		t.Errorf("after window reset: %v", err)
	}
}

// TestRateLimitNoBurstAcrossWindowBoundary reproduces the fixed-window bug the
// token bucket replaces: a limiter anchored at t=0 would roll over to a fresh
// window at t=1000ms and grant a full new quota a single millisecond after
// the previous quota was used — a 4-request burst within ~1ms instead of the
// intended 2 requests per second.
func TestRateLimitNoBurstAcrossWindowBoundary(t *testing.T) {
	t.Parallel()
	now := time.Unix(0, 0)
	srv := httptest.NewServer(transport.NewServer(echoHandler(),
		transport.WithRateLimit(2, time.Second),
		transport.WithClock(func() time.Time { return now }),
	))
	t.Cleanup(srv.Close)

	c := transport.NewClient(srv.URL)
	send := func() error { _, err := c.Send(context.Background(), "x", nil); return err }

	// Anchor the limiter at t=0 (consumes 1 of 2 tokens), then spend the 2nd
	// token just before the 1-second mark.
	if err := send(); err != nil {
		t.Fatalf("priming send at t=0: %v", err)
	}
	now = now.Add(999 * time.Millisecond)
	if err := send(); err != nil {
		t.Fatalf("send at t=999ms: %v", err)
	}

	// A fixed window anchored at t=0 rolls over at t=1000ms, so a request 1ms
	// later would get a fresh quota (2 more requests) immediately. A token
	// bucket instead has only regenerated ~0.002 of a token by then.
	now = now.Add(1 * time.Millisecond)
	if err := send(); err != nil {
		t.Fatalf("send at t=1000ms should still be within the bucket's spare token: %v", err)
	}
	if err := send(); err == nil {
		t.Error("an immediate 2nd send at the old window boundary should still be limited (no 2x burst)")
	}
}

func TestStatusAndCommands(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(transport.NewServer(echoHandler(), transport.WithCommands("a", "b")))
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Errorf("/status = %d", res.StatusCode)
	}

	res2, err := http.Get(srv.URL + "/commands")
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	var body struct{ Commands []string }
	_ = json.NewDecoder(res2.Body).Decode(&body)
	if len(body.Commands) != 2 || body.Commands[0] != "a" {
		t.Errorf("commands = %v", body.Commands)
	}
}

// TestClientSurfacesBodyReadError forces a body-read failure (a declared
// Content-Length longer than the bytes actually sent) and checks Send returns
// an error rather than silently ignoring the read error, as the old
// `raw, _ := io.ReadAll(res.Body)` did.
func TestClientSurfacesBodyReadError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"1"`)) // far short of the declared length
	}))
	t.Cleanup(srv.Close)

	if _, err := transport.NewClient(srv.URL).Send(context.Background(), "x", nil); err == nil {
		t.Error("expected a body-read error to surface")
	}
}

// TestClientCapsResponseSize checks an oversized response body doesn't hang
// or blow up memory: it's read up to the cap and then fails to decode
// (truncated JSON), rather than growing an unbounded buffer.
func TestClientCapsResponseSize(t *testing.T) {
	t.Parallel()
	huge := bytes.Repeat([]byte("a"), 9<<20) // 9 MiB, over the 8 MiB cap
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(huge)
	}))
	t.Cleanup(srv.Close)

	if _, err := transport.NewClient(srv.URL).Send(context.Background(), "x", nil); err == nil {
		t.Error("expected a decode error from the capped (truncated) body")
	}
}

// TestClientConcurrentSendUniqueIDs races many Send calls on one Client and
// checks every request id is unique — regression test for the unsynchronized
// seq counter (run with -race).
func TestClientConcurrentSendUniqueIDs(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(transport.NewServer(echoHandler()))
	t.Cleanup(srv.Close)

	c := transport.NewClient(srv.URL)
	const n = 50
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := c.Send(context.Background(), "x", nil)
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = resp.ID
		}(i)
	}
	wg.Wait()

	seen := make(map[string]bool, n)
	for _, id := range ids {
		if id == "" {
			t.Fatal("empty request id")
		}
		if seen[id] {
			t.Fatalf("duplicate request id %q — seq race", id)
		}
		seen[id] = true
	}
}

func TestErrorResponse(t *testing.T) {
	t.Parallel()
	h := transport.HandlerFunc(func(_ context.Context, req transport.Request) transport.Response {
		return transport.Errorf(req.ID, "boom %d", 42)
	})
	srv := httptest.NewServer(transport.NewServer(h))
	t.Cleanup(srv.Close)

	resp, err := transport.NewClient(srv.URL).Send(context.Background(), "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.OK || resp.Error != "boom 42" {
		t.Errorf("resp = %+v", resp)
	}
}
