package transport_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
