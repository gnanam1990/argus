package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Client sends commands to a guest server.
type Client struct {
	http    *http.Client
	baseURL string
	token   string
	traceID string
	seq     int
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithHTTPClient overrides the HTTP client.
func WithHTTPClient(c *http.Client) ClientOption { return func(cl *Client) { cl.http = c } }

// WithToken sets the bearer token sent on each request.
func WithToken(token string) ClientOption { return func(cl *Client) { cl.token = token } }

// WithTraceID sets a correlation id sent on each request.
func WithTraceID(id string) ClientOption { return func(cl *Client) { cl.traceID = id } }

// NewClient builds a client for the guest at baseURL.
func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{http: http.DefaultClient, baseURL: baseURL}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Send issues a command and returns the decoded response. A transport-level
// failure (network, non-2xx) is returned as an error; a command-level failure
// is carried in Response.Error with OK=false.
func (c *Client) Send(ctx context.Context, command string, params any) (Response, error) {
	c.seq++
	req := Request{ID: fmt.Sprintf("req-%d", c.seq), Command: command, TraceID: c.traceID}
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
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return Response{}, fmt.Errorf("transport: guest returned %d: %s", res.StatusCode, string(raw))
	}

	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Response{}, fmt.Errorf("transport decode: %w", err)
	}
	return resp, nil
}
