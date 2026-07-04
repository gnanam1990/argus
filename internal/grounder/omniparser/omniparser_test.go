package omniparser_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gnanam1990/argus/internal/grounder/omniparser"
	"github.com/gnanam1990/argus/pkg/action"
)

func TestDetectParsesElements(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"version":2,"elements":[
			{"id":1,"box":[10,20,30,40],"label":"OK","interactable":true,"confidence":0.9},
			{"id":2,"box":[0,0,5,5],"label":"weak","interactable":true,"confidence":0.2}
		]}`)
	}))
	t.Cleanup(srv.Close)

	c := omniparser.New(srv.URL, omniparser.WithMinConfidence(0.5))
	els, err := c.Detect(context.Background(), action.Image{Data: []byte{1}})
	if err != nil {
		t.Fatal(err)
	}
	// The 0.2-confidence element is filtered out.
	if len(els) != 1 {
		t.Fatalf("got %d elements, want 1 (min-conf filtered)", len(els))
	}
	if els[0].ID != 1 || els[0].Box.Center() != (action.Point{X: 20, Y: 30}) {
		t.Errorf("element = %+v", els[0])
	}
}

func TestDetectSchemaMismatch(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"version":99,"elements":[]}`)
	}))
	t.Cleanup(srv.Close)
	c := omniparser.New(srv.URL)
	if _, err := c.Detect(context.Background(), action.Image{}); err == nil {
		t.Error("expected schema mismatch error")
	}
}

func TestDetectServiceError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"gpu oom"}`)
	}))
	t.Cleanup(srv.Close)
	c := omniparser.New(srv.URL)
	if _, err := c.Detect(context.Background(), action.Image{}); err == nil {
		t.Error("expected service error")
	}
}

func TestCircuitBreaker(t *testing.T) {
	t.Parallel()
	var calls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	now := time.Unix(1000, 0)
	c := omniparser.New(srv.URL,
		omniparser.WithBreaker(2, time.Minute),
		omniparser.WithClock(func() time.Time { return now }),
	)

	// Two failures open the breaker.
	_, _ = c.Detect(context.Background(), action.Image{})
	_, _ = c.Detect(context.Background(), action.Image{})
	if atomic.LoadInt64(&calls) != 2 {
		t.Fatalf("calls = %d, want 2 before open", calls)
	}

	// While open, Detect fails fast without hitting the service.
	_, err := c.Detect(context.Background(), action.Image{})
	if !errors.Is(err, omniparser.ErrCircuitOpen) {
		t.Errorf("err = %v, want ErrCircuitOpen", err)
	}
	if atomic.LoadInt64(&calls) != 2 {
		t.Errorf("open breaker should not call the service; calls = %d", calls)
	}

	// After cooldown, requests resume.
	now = now.Add(2 * time.Minute)
	_, _ = c.Detect(context.Background(), action.Image{})
	if atomic.LoadInt64(&calls) != 3 {
		t.Errorf("calls = %d, want 3 after cooldown", calls)
	}
}
