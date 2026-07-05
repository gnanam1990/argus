// Package mcp exposes the app-aware computer-use subsystem (state
// observation, permission/approval gating, and action execution) as Model
// Context Protocol tools over a line-delimited JSON-RPC 2.0 stdio transport.
//
// Unlike a raw pixel-coordinate driver, every tool here operates against a
// named macOS application (its bundle identifier): get_app_state observes
// one app's window, accessibility element tree, and any per-app operating
// instructions, and the action tools (click, type_text, press_key, scroll,
// drag, perform_secondary_action) act on that app. To keep the model
// honest, the server tracks per-app "freshness": an action tool refuses to
// run unless get_app_state was the most recently observed thing for that
// app, forcing a fresh observation before every action. Actions are also
// gated on the host's permission/lock preconditions (permissions.Orchestrator)
// and on a persistent per-app approval decision (approval.Store) — an app
// that has not been explicitly approved cannot be driven.
//
// The server holds no OS dependencies of its own; every side effect
// (observing state, checking preconditions, checking approval, acting) is
// injected as an interface, so Serve can be driven entirely in-memory in
// tests.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/gnanam1990/argus/internal/computeruse/actor"
	"github.com/gnanam1990/argus/internal/computeruse/approval"
	"github.com/gnanam1990/argus/internal/computeruse/permissions"
	"github.com/gnanam1990/argus/internal/computeruse/state"
)

// protocolVersion is the MCP revision this server speaks.
const protocolVersion = "2025-06-18"

// maxLineBytes bounds a single JSON-RPC message read from stdin, so a client
// that floods input with no newline can't grow bufio's buffer unboundedly.
const maxLineBytes = 8 << 20 // 8 MiB

// Exact tool-error strings the spec requires verbatim, returned as in-band
// tool result content (isError: true), never as JSON-RPC protocol errors.
const (
	errNotFresh = "You must call get_app_state to get the latest state before doing other Computer Use actions."
	errLocked   = "Computer Use cannot run while the screen is locked."
	errPending  = "Computer Use preconditions are not ready yet; call this tool again to retry."
)

// errNotApproved formats the approval-required tool error for bundleID.
func errNotApproved(bundleID string) string {
	return fmt.Sprintf("Computer Use is not allowed to use the app %q. Ask the user for approval.", bundleID)
}

// cacheEntry is the last observation for one app and whether it is still
// fresh (usable to gate/resolve the next action) or has gone stale (an
// action already consumed it, so a fresh get_app_state is required again).
type cacheEntry struct {
	state state.AppState
	fresh bool
}

// Server serves app-aware Computer Use tools over MCP. All fields besides
// the mutex-guarded session cache are immutable after New.
type Server struct {
	sp    state.StateProvider
	act   actor.Actor
	orch  permissions.Orchestrator
	store approval.Store

	name    string
	version string

	mu    sync.Mutex
	cache map[string]cacheEntry // bundle identifier -> last observation
}

// Option configures a Server.
type Option func(*Server)

// WithInfo sets the server name/version reported on initialize.
func WithInfo(name, version string) Option {
	return func(s *Server) { s.name, s.version = name, version }
}

// New builds a Server. sp observes app state, act performs resolved UI
// actions, orch gates on host preconditions (screen lock, OS permissions),
// and store holds per-app approval decisions.
func New(sp state.StateProvider, act actor.Actor, orch permissions.Orchestrator, store approval.Store, opts ...Option) *Server {
	s := &Server{
		sp:      sp,
		act:     act,
		orch:    orch,
		store:   store,
		name:    "argus-computer-use",
		version: "dev",
		cache:   make(map[string]cacheEntry),
	}
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

// Serve reads newline-delimited JSON-RPC messages from in and writes
// responses to out until in is exhausted or ctx is cancelled. Each line is
// capped at maxLineBytes: a no-newline flood on stdin returns a clean error
// instead of growing memory without bound.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	enc := json.NewEncoder(out)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !sc.Scan() {
			if err := sc.Err(); err != nil {
				if errors.Is(err, bufio.ErrTooLong) {
					return fmt.Errorf("mcp: line exceeds %d byte limit", maxLineBytes)
				}
				return err
			}
			return nil // EOF
		}
		resp, respond := s.handleLine(ctx, sc.Bytes())
		if respond {
			if werr := enc.Encode(resp); werr != nil {
				return werr
			}
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

// Handle dispatches a single request and returns its response. It is
// concurrency-safe: multiple goroutines may call Handle (or drive Serve)
// against the same Server concurrently.
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

// getFresh returns the cached AppState for bundleID if it is still fresh.
func (s *Server) getFresh(bundleID string) (state.AppState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.cache[bundleID]
	if !ok || !e.fresh {
		return state.AppState{}, false
	}
	return e.state, true
}

// setFresh records st as the latest observation for bundleID and marks it
// fresh (called on a successful get_app_state).
func (s *Server) setFresh(bundleID string, st state.AppState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[bundleID] = cacheEntry{state: st, fresh: true}
}

// markStale invalidates the cached observation for bundleID (called after
// every action, so the next action must re-observe first).
func (s *Server) markStale(bundleID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.cache[bundleID]; ok {
		e.fresh = false
		s.cache[bundleID] = e
	}
}
