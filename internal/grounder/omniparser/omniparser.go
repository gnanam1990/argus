// Package omniparser is a grounder.Grounder backed by an OmniParser vision
// service running out-of-process (it needs a GPU). Argus owns and versions the
// JSON contract. A circuit breaker wraps the client so a down or slow service
// degrades gracefully — Detect fails fast instead of stalling every step — and
// the caller (a chain grounder) can fall back to another detector.
//
// Licensing note: OmniParser's icon-detection weights are AGPL. Serving them
// over a network can trigger source-availability obligations, so making this
// the default detector is a release-gated decision; the permissive
// accessibility-tree detector is the safer default.
package omniparser

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/grounder"
)

// SchemaVersion is the owned request/response contract version. A response with
// a different version is rejected so service drift surfaces immediately.
const SchemaVersion = 2

const (
	// responseCap bounds how much of a /parse response body is read, so a
	// runaway or malicious service response can't OOM the caller.
	responseCap = 8 << 20 // 8 MiB
	// defaultTimeout bounds a request when the caller does not supply an
	// *http.Client via WithHTTPClient, so a hanging GPU service can't stall
	// every step forever — and, once Do returns that timeout as an error, it
	// counts as a breaker failure like any other request error.
	defaultTimeout = 15 * time.Second
)

// ErrCircuitOpen is returned while the breaker is open (service unhealthy).
var ErrCircuitOpen = errors.New("omniparser: circuit open (service unhealthy)")

// Client calls an OmniParser service.
type Client struct {
	http    *http.Client
	baseURL string
	minConf float64
	breaker *breaker
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient overrides the HTTP client.
func WithHTTPClient(c *http.Client) Option { return func(cl *Client) { cl.http = c } }

// WithMinConfidence drops detections below conf.
func WithMinConfidence(conf float64) Option { return func(cl *Client) { cl.minConf = conf } }

// WithBreaker configures the circuit breaker (failure threshold + cooldown).
func WithBreaker(threshold int, cooldown time.Duration) Option {
	return func(cl *Client) { cl.breaker.threshold, cl.breaker.cooldown = threshold, cooldown }
}

// WithClock injects the breaker's clock (for tests).
func WithClock(now func() time.Time) Option {
	return func(cl *Client) { cl.breaker.now = now }
}

// New builds a client for the OmniParser service at baseURL. The default HTTP
// client has its own defaultTimeout (rather than sharing the mutable
// http.DefaultClient); pass WithHTTPClient to override it.
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		http:    &http.Client{Timeout: defaultTimeout},
		baseURL: baseURL,
		breaker: &breaker{threshold: 3, cooldown: 30 * time.Second, now: time.Now},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

var _ grounder.Grounder = (*Client)(nil)

type parseRequest struct {
	Image   string `json:"image"`
	Version int    `json:"version"`
}

type wireElement struct {
	ID           int     `json:"id"`
	Box          [4]int  `json:"box"` // x0,y0,x1,y1
	Label        string  `json:"label"`
	Text         string  `json:"text"`
	Interactable bool    `json:"interactable"`
	Confidence   float64 `json:"confidence"`
}

type parseResponse struct {
	Version  int           `json:"version"`
	Elements []wireElement `json:"elements"`
	Error    string        `json:"error"`
}

// Detect sends the image to the service and returns the detected elements.
func (c *Client) Detect(ctx context.Context, img action.Image) ([]grounder.Element, error) {
	if !c.breaker.allow() {
		return nil, ErrCircuitOpen
	}
	els, err := c.detect(ctx, img)
	if err != nil {
		c.breaker.failure()
		return nil, err
	}
	c.breaker.success()
	return els, nil
}

func (c *Client) detect(ctx context.Context, img action.Image) ([]grounder.Element, error) {
	body, err := json.Marshal(parseRequest{
		Image:   base64.StdEncoding.EncodeToString(img.Data),
		Version: SchemaVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("omniparser marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/parse", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("omniparser request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("omniparser: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, responseCap))
	if err != nil {
		return nil, fmt.Errorf("omniparser: read response: %w", err)
	}
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("omniparser api error (status %d): %s", res.StatusCode, string(raw))
	}

	var out parseResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("omniparser decode: %w", err)
	}
	if out.Error != "" {
		return nil, fmt.Errorf("omniparser: %s", out.Error)
	}
	if out.Version != SchemaVersion {
		return nil, fmt.Errorf("omniparser: schema version mismatch (got %d, want %d)", out.Version, SchemaVersion)
	}

	els := make([]grounder.Element, 0, len(out.Elements))
	for _, e := range out.Elements {
		if e.Confidence < c.minConf {
			continue
		}
		els = append(els, grounder.Element{
			ID:           e.ID,
			Box:          action.Rect{Min: action.Point{X: e.Box[0], Y: e.Box[1]}, Max: action.Point{X: e.Box[2], Y: e.Box[3]}},
			Label:        e.Label,
			Text:         e.Text,
			Interactable: e.Interactable,
			Confidence:   e.Confidence,
		})
	}
	return els, nil
}

// breaker is a minimal three-state circuit breaker: closed (healthy) → open
// (threshold consecutive failures; every call fails fast) → half-open (the
// cooldown has elapsed; exactly one probe call is let through while every
// other caller still fails fast) → closed again on a successful probe, or
// back to open for another full cooldown on a failed one.
//
// The old two-state version (closed/open only) let every caller through the
// instant the cooldown expired, so a burst of concurrent requests could all
// hit a still-recovering service at once; probing gates that down to one.
type breaker struct {
	mu        sync.Mutex
	threshold int
	cooldown  time.Duration
	now       func() time.Time
	fails     int
	open      bool
	openUntil time.Time
	probing   bool
}

// allow reports whether a call may proceed. Once open, it transitions to
// half-open (admitting exactly one probing call) only after the cooldown has
// elapsed; while a probe is outstanding, every other caller still fails fast.
func (b *breaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.open {
		return true
	}
	if b.now().Before(b.openUntil) {
		return false // still fully open
	}
	if b.probing {
		return false // a probe is already in flight
	}
	b.probing = true
	return true
}

// success records a successful call. A successful probe closes the breaker
// entirely; a success while closed is a no-op (fails is already 0).
func (b *breaker) success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fails = 0
	b.open = false
	b.probing = false
	b.openUntil = time.Time{}
}

// failure records a failed call. A failed probe reopens for another full
// cooldown; a failure while closed only opens once fails reaches threshold.
func (b *breaker) failure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.probing = false
	if b.open {
		b.openUntil = b.now().Add(b.cooldown)
		b.fails = 0
		return
	}
	b.fails++
	if b.fails >= b.threshold {
		b.open = true
		b.openUntil = b.now().Add(b.cooldown)
		b.fails = 0
	}
}
