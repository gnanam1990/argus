// Package mcpserver exposes a computer.Computer as Model Context Protocol tools
// (screenshot, click, type, key, scroll, move, cursor_position) over a JSON-RPC
// 2.0 stdio transport. It is self-contained (encoding/json + a small JSON-RPC
// loop), so any MCP client can drive the same driver the agent loop uses, and
// the protocol handling is unit-testable without a transport.
package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/gnanam1990/argus/pkg/computer"
)

// protocolVersion is the MCP revision this server speaks.
const protocolVersion = "2025-06-18"

// Server serves MCP tools backed by a Computer.
type Server struct {
	exec    computer.ActionExecutor
	name    string
	version string
}

// Option configures a Server.
type Option func(*Server)

// WithInfo sets the server name/version reported on initialize.
func WithInfo(name, version string) Option {
	return func(s *Server) { s.name, s.version = name, version }
}

// New builds a server over c.
func New(c computer.Computer, opts ...Option) *Server {
	s := &Server{exec: computer.NewExecutor(c), name: "argus", version: "dev"}
	for _, o := range opts {
		o(s)
	}
	return s
}

// JSON-RPC 2.0 envelopes.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
)

// Serve reads newline-delimited JSON-RPC messages from in and writes responses
// to out until in is exhausted or ctx is cancelled.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	r := bufio.NewReader(in)
	enc := json.NewEncoder(out)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			resp, respond := s.handleLine(ctx, line)
			if respond {
				if werr := enc.Encode(resp); werr != nil {
					return werr
				}
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// handleLine parses and dispatches one message, returning the response and
// whether it should be written (notifications produce no response).
func (s *Server) handleLine(ctx context.Context, line []byte) (rpcResponse, bool) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return errResponse(nil, codeParse, "parse error"), true
	}
	resp := s.Handle(ctx, req)
	// Notifications (no id) get no response.
	return resp, len(req.ID) > 0
}

// Handle dispatches a single request and returns its response.
func (s *Server) Handle(ctx context.Context, req rpcRequest) rpcResponse {
	switch req.Method {
	case "initialize":
		return okResponse(req.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"serverInfo":      map[string]any{"name": s.name, "version": s.version},
			"capabilities":    map[string]any{"tools": map[string]any{}},
		})
	case "notifications/initialized":
		return rpcResponse{} // notification, ignored
	case "tools/list":
		return okResponse(req.ID, map[string]any{"tools": toolList()})
	case "tools/call":
		return s.callTool(ctx, req)
	default:
		return errResponse(req.ID, codeMethodNotFound, fmt.Sprintf("method not found: %s", req.Method))
	}
}

func okResponse(id json.RawMessage, result any) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func errResponse(id json.RawMessage, code int, msg string) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}
