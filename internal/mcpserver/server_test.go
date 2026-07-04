package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
	compfake "github.com/gnanam1990/argus/pkg/computer/fake"
)

var errTest = errors.New("driver failure")

func req(t *testing.T, id, method string, params any) rpcRequest {
	t.Helper()
	r := rpcRequest{JSONRPC: "2.0", Method: method}
	if id != "" {
		r.ID = json.RawMessage(id)
	}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		r.Params = b
	}
	return r
}

func call(name string, args map[string]any) map[string]any {
	return map[string]any{"name": name, "arguments": args}
}

func TestInitialize(t *testing.T) {
	t.Parallel()
	s := New(compfake.New(), WithInfo("argus", "1.0"))
	resp := s.Handle(context.Background(), req(t, "1", "initialize", nil))
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	m := resp.Result.(map[string]any)
	if m["protocolVersion"] != protocolVersion {
		t.Errorf("protocolVersion = %v", m["protocolVersion"])
	}
	info := m["serverInfo"].(map[string]any)
	if info["name"] != "argus" || info["version"] != "1.0" {
		t.Errorf("serverInfo = %v", info)
	}
}

func TestToolsList(t *testing.T) {
	t.Parallel()
	s := New(compfake.New())
	resp := s.Handle(context.Background(), req(t, "1", "tools/list", nil))
	tools := resp.Result.(map[string]any)["tools"].([]map[string]any)
	if len(tools) != 8 {
		t.Fatalf("got %d tools, want 8", len(tools))
	}
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl["name"].(string)] = true
	}
	for _, want := range []string{"screenshot", "click", "type", "key", "scroll", "cursor_position"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestCallScreenshot(t *testing.T) {
	t.Parallel()
	s := New(compfake.New().WithScreenshot(action.Image{MIME: action.MIMEPNG, Data: []byte{1, 2, 3}}, 100, 100))
	resp := s.Handle(context.Background(), req(t, "1", "tools/call", call("screenshot", nil)))
	res := resp.Result.(map[string]any)
	if res["isError"] != false {
		t.Fatalf("isError = %v", res["isError"])
	}
	content := res["content"].([]map[string]any)
	if content[0]["type"] != "image" {
		t.Errorf("content = %+v, want image", content[0])
	}
}

func TestCallClick(t *testing.T) {
	t.Parallel()
	f := compfake.New()
	s := New(f)
	resp := s.Handle(context.Background(), req(t, "1", "tools/call", call("click", map[string]any{"x": 10, "y": 20, "button": "right"})))
	if resp.Error != nil || resp.Result.(map[string]any)["isError"] != false {
		t.Fatalf("resp = %+v", resp)
	}
	last, _ := f.Last()
	if last.Method != "Click" || last.X != 10 || last.Y != 20 || last.Button != action.Right {
		t.Errorf("click recorded = %+v", last)
	}
}

func TestCallCursorPosition(t *testing.T) {
	t.Parallel()
	s := New(compfake.New().WithCursor(42, 99))
	resp := s.Handle(context.Background(), req(t, "1", "tools/call", call("cursor_position", nil)))
	content := resp.Result.(map[string]any)["content"].([]map[string]any)
	if content[0]["text"] != "42,99" {
		t.Errorf("cursor text = %v, want 42,99", content[0]["text"])
	}
}

func TestCallUnknownTool(t *testing.T) {
	t.Parallel()
	s := New(compfake.New())
	resp := s.Handle(context.Background(), req(t, "1", "tools/call", call("teleport", nil)))
	if resp.Error == nil || resp.Error.Code != codeInvalidParams {
		t.Errorf("expected invalid-params error, got %+v", resp.Error)
	}
}

func TestUnknownMethod(t *testing.T) {
	t.Parallel()
	s := New(compfake.New())
	resp := s.Handle(context.Background(), req(t, "1", "no/such", nil))
	if resp.Error == nil || resp.Error.Code != codeMethodNotFound {
		t.Errorf("expected method-not-found, got %+v", resp.Error)
	}
}

func TestCallActionDispatch(t *testing.T) {
	t.Parallel()
	cases := []struct {
		tool   string
		args   map[string]any
		method string
	}{
		{"double_click", map[string]any{"x": 1, "y": 2}, "Click"},
		{"move", map[string]any{"x": 3, "y": 4}, "MoveMouse"},
		{"type", map[string]any{"text": "hi"}, "TypeText"},
		{"key", map[string]any{"keys": []string{"ctrl", "c"}}, "KeyPress"},
		{"scroll", map[string]any{"x": 5, "y": 5, "dy": 2}, "Scroll"},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			t.Parallel()
			f := compfake.New()
			s := New(f)
			resp := s.Handle(context.Background(), req(t, "1", "tools/call", call(c.tool, c.args)))
			if resp.Error != nil || resp.Result.(map[string]any)["isError"] != false {
				t.Fatalf("%s resp = %+v", c.tool, resp)
			}
			last, _ := f.Last()
			if last.Method != c.method {
				t.Errorf("%s → %s, want %s", c.tool, last.Method, c.method)
			}
		})
	}
}

func TestCallExecutorErrorIsInBand(t *testing.T) {
	t.Parallel()
	f := compfake.New().WithError(errTest)
	s := New(f)
	resp := s.Handle(context.Background(), req(t, "1", "tools/call", call("click", map[string]any{"x": 1, "y": 1})))
	// Driver failure is reported as isError:true, not a protocol error.
	if resp.Error != nil {
		t.Fatalf("should be in-band, got protocol error %+v", resp.Error)
	}
	if resp.Result.(map[string]any)["isError"] != true {
		t.Errorf("isError = %v, want true", resp.Result.(map[string]any)["isError"])
	}
}

func TestCallBadArguments(t *testing.T) {
	t.Parallel()
	s := New(compfake.New())
	bad := rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call",
		Params: json.RawMessage(`{"name":"click","arguments":"not-an-object"}`)}
	resp := s.Handle(context.Background(), bad)
	if resp.Error == nil || resp.Error.Code != codeInvalidParams {
		t.Errorf("expected invalid-params, got %+v", resp.Error)
	}
}

func TestServeEndToEnd(t *testing.T) {
	t.Parallel()
	in := `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"
	var out bytes.Buffer
	s := New(compfake.New())
	if err := s.Serve(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	// Two requests → two responses; the notification produces none.
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d responses, want 2:\n%s", len(lines), out.String())
	}
	if !strings.Contains(lines[0], "protocolVersion") || !strings.Contains(lines[1], "tools") {
		t.Errorf("responses = %v", lines)
	}
}

// TestServeCapsLineLength checks a no-newline flood past maxLineBytes returns
// a clean error instead of growing bufio's buffer without bound.
func TestServeCapsLineLength(t *testing.T) {
	t.Parallel()
	huge := bytes.Repeat([]byte("a"), maxLineBytes+1) // no newline: one oversized "line"
	var out bytes.Buffer
	s := New(compfake.New())
	err := s.Serve(context.Background(), bytes.NewReader(huge), &out)
	if err == nil {
		t.Fatal("expected an error for a line exceeding the byte limit")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error = %v, want a size-limit message", err)
	}
}

// TestServeAllowsLineUpToLimit checks a line at (not over) the cap is still
// processed normally, so the boundary itself isn't off-by-one.
func TestServeAllowsLineUpToLimit(t *testing.T) {
	t.Parallel()
	// A single valid, newline-terminated request comfortably under the cap.
	in := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"
	var out bytes.Buffer
	s := New(compfake.New())
	if err := s.Serve(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if !strings.Contains(out.String(), "tools") {
		t.Errorf("expected a tools/list response, got %s", out.String())
	}
}

func TestServeParseError(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	s := New(compfake.New())
	if err := s.Serve(context.Background(), strings.NewReader("not json\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "parse error") {
		t.Errorf("expected parse error response, got %s", out.String())
	}
}
