package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

const (
	// maxResponseBytes caps how much of a guest response body Send will read,
	// so a misbehaving or malicious guest can't OOM the host with an
	// unbounded response.
	maxResponseBytes = 8 << 20 // 8 MiB
	// defaultTimeout bounds a request when the caller does not supply an
	// *http.Client via WithHTTPClient, so a hung guest can't wedge the agent
	// forever.
	defaultTimeout = 30 * time.Second
)

// Client sends commands to a guest server.
type Client struct {
	http    *http.Client
	baseURL string
	token   string
	traceID string
	seq     atomic.Int64
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithHTTPClient overrides the HTTP client.
func WithHTTPClient(c *http.Client) ClientOption { return func(cl *Client) { cl.http = c } }

// WithToken sets the bearer token sent on each request.
func WithToken(token string) ClientOption { return func(cl *Client) { cl.token = token } }

// WithTraceID sets a correlation id sent on each request.
func WithTraceID(id string) ClientOption { return func(cl *Client) { cl.traceID = id } }

// NewClient builds a client for the guest at baseURL. The default HTTP client
// has its own 30s timeout (rather than sharing the mutable http.DefaultClient)
// so it can't be silently repointed by an unrelated part of the process; pass
// WithHTTPClient to override the timeout or transport.
func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{http: &http.Client{Timeout: defaultTimeout}, baseURL: baseURL}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Send issues a command and returns the decoded response. A transport-level
// failure (network, non-2xx) is returned as an error; a command-level failure
// is carried in Response.Error with OK=false. seq is an atomic counter so
// concurrent callers on the same Client get distinct request ids.
func (c *Client) Send(ctx context.Context, command string, params any) (Response, error) {
	seq := c.seq.Add(1)
	req := Request{ID: fmt.Sprintf("req-%d", seq), Command: command, TraceID: c.traceID}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return Response{}, fmt.Errorf("transport marshal params: %w", err)
		}
		req.Params = b
	}

	body, err := json.Marshal(req)
	if err != nil {
		return Response{}, fmt.Errorf("transport marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/cmd", bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("transport request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}

	res, err := c.http.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("transport: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return Response{}, fmt.Errorf("transport: read response: %w", err)
	}
	if res.StatusCode >= 400 {
		return Response{}, fmt.Errorf("transport: guest returned %d: %s", res.StatusCode, string(raw))
	}

	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Response{}, fmt.Errorf("transport decode: %w", err)
	}
	return resp, nil
}
