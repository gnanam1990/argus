// Package transport is the wire protocol between the agent (host) and the
// in-sandbox guest server: a small {id, command, params} request / {id, ok,
// result, error} response envelope, plus an authenticated, rate-limited,
// audited HTTP server and a matching client. The guest server is
// remote-code-execution-shaped (it types keystrokes and can run commands), so
// auth, a localhost-default bind, rate limiting, and a per-command audit log
// are first-class here rather than bolted on.
package transport

import (
	"context"
	"encoding/json"
	"fmt"
)

// Request is a command sent to the guest.
type Request struct {
	ID      string          `json:"id"`
	Command string          `json:"command"`
	TraceID string          `json:"trace_id,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is the guest's reply.
type Response struct {
	ID     string          `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// Handler dispatches a command request to a reply.
type Handler interface {
	Handle(ctx context.Context, req Request) Response
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(ctx context.Context, req Request) Response

// Handle implements Handler.
func (f HandlerFunc) Handle(ctx context.Context, req Request) Response { return f(ctx, req) }

// Result builds a successful response, marshaling v as the result.
func Result(id string, v any) Response {
	b, err := json.Marshal(v)
	if err != nil {
		return Errorf(id, "marshal result: %v", err)
	}
	return Response{ID: id, OK: true, Result: b}
}

// Errorf builds an error response.
func Errorf(id, format string, args ...any) Response {
	return Response{ID: id, OK: false, Error: fmt.Sprintf(format, args...)}
}
