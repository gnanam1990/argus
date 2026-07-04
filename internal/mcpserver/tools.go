package mcpserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/gnanam1990/argus/pkg/action"
)

// callParams is the tools/call request shape.
type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// toolArgs covers every tool's argument set.
type toolArgs struct {
	X      int      `json:"x"`
	Y      int      `json:"y"`
	Button string   `json:"button"`
	Text   string   `json:"text"`
	Keys   []string `json:"keys"`
	DX     int      `json:"dx"`
	DY     int      `json:"dy"`
}

func (a toolArgs) button() action.Button {
	switch a.Button {
	case "right":
		return action.Right
	case "middle":
		return action.Middle
	default:
		return action.Left
	}
}

// toBoolContent renders a text content array.
func textContent(s string) []map[string]any {
	return []map[string]any{{"type": "text", "text": s}}
}

// callTool executes a named tool and returns an MCP tool result.
func (s *Server) callTool(ctx context.Context, req rpcRequest) rpcResponse {
	var p callParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errResponse(req.ID, codeInvalidParams, "invalid tool params")
	}
	var args toolArgs
	if len(p.Arguments) > 0 {
		if err := json.Unmarshal(p.Arguments, &args); err != nil {
			return errResponse(req.ID, codeInvalidParams, "invalid tool arguments")
		}
	}

	a, ok := toAction(p.Name, args)
	if !ok {
		return errResponse(req.ID, codeInvalidParams, fmt.Sprintf("unknown tool: %s", p.Name))
	}

	res, err := s.exec.Execute(ctx, a)
	if err != nil {
		// Tool errors are reported in-band (isError), not as protocol errors.
		return okResponse(req.ID, map[string]any{
			"content": textContent("error: " + err.Error()),
			"isError": true,
		})
	}

	content := resultContent(p.Name, a, res)
	return okResponse(req.ID, map[string]any{"content": content, "isError": false})
}

func toAction(name string, args toolArgs) (action.Action, bool) {
	base := action.Action{Mark: action.NoMark, Button: args.button()}
	switch name {
	case "screenshot":
		base.Type = action.Screenshot
	case "cursor_position":
		base.Type = action.CursorPosition
	case "click":
		base.Type = action.Click
		base.Point = action.Point{X: args.X, Y: args.Y}
	case "double_click":
		base.Type = action.DoubleClick
		base.Point = action.Point{X: args.X, Y: args.Y}
	case "move":
		base.Type = action.Move
		base.Point = action.Point{X: args.X, Y: args.Y}
	case "type":
		base.Type = action.Type
		base.Text = args.Text
	case "key":
		base.Type = action.Key
		base.Keys = args.Keys
	case "scroll":
		base.Type = action.Scroll
		base.Point = action.Point{X: args.X, Y: args.Y}
		base.DX, base.DY = args.DX, args.DY
		if base.DX == 0 && base.DY == 0 {
			base.DY = 1
		}
	default:
		return action.Action{}, false
	}
	return base, true
}

func resultContent(name string, a action.Action, res action.Result) []map[string]any {
	switch {
	case !res.Screenshot.Empty():
		return []map[string]any{{
			"type":     "image",
			"data":     base64.StdEncoding.EncodeToString(res.Screenshot.Data),
			"mimeType": res.Screenshot.MIME,
		}}
	case a.Type == action.CursorPosition:
		return textContent(fmt.Sprintf("%d,%d", res.Cursor.X, res.Cursor.Y))
	default:
		return textContent(name + " ok")
	}
}

// toolList returns the advertised tool schemas.
func toolList() []map[string]any {
	coord := map[string]any{
		"x": map[string]any{"type": "integer"},
		"y": map[string]any{"type": "integer"},
	}
	return []map[string]any{
		schema("screenshot", "Capture the screen", map[string]any{}, nil),
		schema("click", "Click at (x,y)", merge(coord, map[string]any{
			"button": map[string]any{"type": "string", "enum": []string{"left", "right", "middle"}},
		}), []string{"x", "y"}),
		schema("double_click", "Double-click at (x,y)", coord, []string{"x", "y"}),
		schema("move", "Move the pointer to (x,y)", coord, []string{"x", "y"}),
		schema("type", "Type literal text", map[string]any{
			"text": map[string]any{"type": "string"},
		}, []string{"text"}),
		schema("key", "Press a key chord", map[string]any{
			"keys": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}, []string{"keys"}),
		schema("scroll", "Scroll at (x,y) by (dx,dy)", merge(coord, map[string]any{
			"dx": map[string]any{"type": "integer"},
			"dy": map[string]any{"type": "integer"},
		}), []string{"x", "y"}),
		schema("cursor_position", "Report the pointer position", map[string]any{}, nil),
	}
}

func schema(name, desc string, props map[string]any, required []string) map[string]any {
	in := map[string]any{"type": "object", "properties": props}
	if required != nil {
		in["required"] = required
	}
	return map[string]any{"name": name, "description": desc, "inputSchema": in}
}

func merge(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
