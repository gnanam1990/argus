package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gnanam1990/argus/internal/computeruse/actor"
	"github.com/gnanam1990/argus/internal/computeruse/approval"
	"github.com/gnanam1990/argus/internal/computeruse/permissions"
	"github.com/gnanam1990/argus/internal/computeruse/state"
)

// callParams is the tools/call request shape.
type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// toolArgs covers every tool's argument set. Optional integer fields are
// pointers so "absent" (nil) is distinguishable from an explicit 0, which
// matters for ElementIndex (index 0 is a valid element) and for X/Y.
type toolArgs struct {
	BundleIdentifier string   `json:"bundle_identifier"`
	ElementIndex     *int     `json:"element_index,omitempty"`
	X                *int     `json:"x,omitempty"`
	Y                *int     `json:"y,omitempty"`
	Button           string   `json:"button,omitempty"`
	Text             string   `json:"text,omitempty"`
	Keys             []string `json:"keys,omitempty"`
	Direction        string   `json:"direction,omitempty"`
	Pages            int      `json:"pages,omitempty"`
	FromX            int      `json:"from_x,omitempty"`
	FromY            int      `json:"from_y,omitempty"`
	ToX              int      `json:"to_x,omitempty"`
	ToY              int      `json:"to_y,omitempty"`
}

// textContent renders a single-item text content array.
func textContent(s string) []map[string]any {
	return []map[string]any{{"type": "text", "text": s}}
}

// toolError builds an in-band tool error result (isError: true). Per spec,
// preconditions and ordering violations are reported this way, not as
// JSON-RPC protocol errors, so the model sees them in the tool result it is
// already parsing.
func toolError(msg string) map[string]any {
	return map[string]any{"content": textContent(msg), "isError": true}
}

// toolOK builds a successful in-band tool result.
func toolOK(content []map[string]any) map[string]any {
	return map[string]any{"content": content, "isError": false}
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

	switch p.Name {
	case "get_app_state":
		return okResponse(req.ID, s.toolGetAppState(ctx, args))
	case "list_apps":
		return okResponse(req.ID, s.toolListApps(ctx))
	case "click":
		return okResponse(req.ID, s.toolClick(ctx, args))
	case "type_text":
		return okResponse(req.ID, s.toolTypeText(ctx, args))
	case "press_key":
		return okResponse(req.ID, s.toolPressKey(ctx, args))
	case "scroll":
		return okResponse(req.ID, s.toolScroll(ctx, args))
	case "drag":
		return okResponse(req.ID, s.toolDrag(ctx, args))
	case "perform_secondary_action":
		return okResponse(req.ID, s.toolPerformSecondaryAction(ctx, args))
	default:
		return errResponse(req.ID, codeInvalidParams, fmt.Sprintf("unknown tool: %s", p.Name))
	}
}

// toolGetAppState observes bundle_identifier, caches the observation as
// fresh, and returns the window info, element tree (with indices), and any
// instruction as JSON text. The screenshot is omitted from the text result.
func (s *Server) toolGetAppState(ctx context.Context, a toolArgs) map[string]any {
	st, err := s.sp.GetAppState(ctx, a.BundleIdentifier)
	if err != nil {
		return toolError("get_app_state: " + err.Error())
	}
	s.setFresh(a.BundleIdentifier, st)

	// state.AppState's Screenshot field is tagged json:"-", so marshaling
	// the observation directly already omits it from the text result.
	body, err := json.Marshal(st)
	if err != nil {
		return toolError("get_app_state: " + err.Error())
	}
	return toolOK(textContent(string(body)))
}

// toolListApps returns the available apps as JSON text.
func (s *Server) toolListApps(ctx context.Context) map[string]any {
	apps, err := s.sp.ListApps(ctx)
	if err != nil {
		return toolError("list_apps: " + err.Error())
	}
	body, err := json.Marshal(apps)
	if err != nil {
		return toolError("list_apps: " + err.Error())
	}
	return toolOK(textContent(string(body)))
}

// ready is the outcome of the shared action precondition check: freshness,
// host preconditions, and approval.
type ready struct {
	state state.AppState
	err   string // non-empty means the action must not proceed
}

// requireReady runs the three preconditions every ACTION tool shares, in
// order: (1) a fresh cached observation exists for bundleID, (2) the host
// preconditions (screen unlocked, permissions granted) hold, (3) the app is
// approved. It returns the cached observation (for coordinate resolution)
// and, on failure, the exact tool-error message to surface.
func (s *Server) requireReady(ctx context.Context, bundleID string) ready {
	st, fresh := s.getFresh(bundleID)
	if !fresh {
		return ready{err: errNotFresh}
	}

	if err := s.orch.Ensure(ctx); err != nil {
		switch {
		case errors.Is(err, permissions.ErrPending):
			return ready{err: errPending}
		case errors.Is(err, permissions.ErrScreenLocked):
			return ready{err: errLocked}
		case errors.Is(err, permissions.ErrPermissionsMissing):
			return ready{err: err.Error()}
		default:
			return ready{err: err.Error()}
		}
	}

	decision, err := s.store.Get(ctx, bundleID)
	if err != nil {
		return ready{err: "approval: " + err.Error()}
	}
	if decision != approval.Approved {
		return ready{err: errNotApproved(bundleID)}
	}

	return ready{state: st}
}

// resolveXY returns the screen coordinates an action should target: if
// ElementIndex is present and non-negative, the center of that element's
// cached frame; otherwise the request's X/Y (missing treated as 0).
func resolveXY(st state.AppState, a toolArgs) (int, int, error) {
	if a.ElementIndex != nil && *a.ElementIndex >= 0 {
		el, ok := st.FindByIndex(*a.ElementIndex)
		if !ok {
			return 0, 0, fmt.Errorf("element index %d not found in the cached state for %q", *a.ElementIndex, st.BundleIdentifier)
		}
		cx := el.Frame.X + el.Frame.Width/2
		cy := el.Frame.Y + el.Frame.Height/2
		return int(cx), int(cy), nil
	}
	var x, y int
	if a.X != nil {
		x = *a.X
	}
	if a.Y != nil {
		y = *a.Y
	}
	return x, y, nil
}

// toolClick resolves the target coordinates and issues a click.
func (s *Server) toolClick(ctx context.Context, a toolArgs) map[string]any {
	r := s.requireReady(ctx, a.BundleIdentifier)
	if r.err != "" {
		return toolError(r.err)
	}
	x, y, err := resolveXY(r.state, a)
	if err != nil {
		return toolError("click: " + err.Error())
	}
	elementIndex := -1
	if a.ElementIndex != nil {
		elementIndex = *a.ElementIndex
	}
	err = s.act.Click(ctx, actor.ClickRequest{
		BundleIdentifier: a.BundleIdentifier,
		ElementIndex:     elementIndex,
		X:                x,
		Y:                y,
		Button:           a.Button,
	})
	s.markStale(a.BundleIdentifier)
	if err != nil {
		return toolError("click: " + err.Error())
	}
	return toolOK(textContent("click ok"))
}

// toolTypeText types literal text into the focused element.
func (s *Server) toolTypeText(ctx context.Context, a toolArgs) map[string]any {
	r := s.requireReady(ctx, a.BundleIdentifier)
	if r.err != "" {
		return toolError(r.err)
	}
	err := s.act.TypeText(ctx, actor.TypeRequest{BundleIdentifier: a.BundleIdentifier, Text: a.Text})
	s.markStale(a.BundleIdentifier)
	if err != nil {
		return toolError("type_text: " + err.Error())
	}
	return toolOK(textContent("type_text ok"))
}

// toolPressKey issues a key or key-chord press.
func (s *Server) toolPressKey(ctx context.Context, a toolArgs) map[string]any {
	r := s.requireReady(ctx, a.BundleIdentifier)
	if r.err != "" {
		return toolError(r.err)
	}
	err := s.act.PressKey(ctx, actor.KeyRequest{BundleIdentifier: a.BundleIdentifier, Keys: a.Keys})
	s.markStale(a.BundleIdentifier)
	if err != nil {
		return toolError("press_key: " + err.Error())
	}
	return toolOK(textContent("press_key ok"))
}

// toolScroll resolves the target coordinates and issues a scroll gesture.
func (s *Server) toolScroll(ctx context.Context, a toolArgs) map[string]any {
	r := s.requireReady(ctx, a.BundleIdentifier)
	if r.err != "" {
		return toolError(r.err)
	}
	x, y, err := resolveXY(r.state, a)
	if err != nil {
		return toolError("scroll: " + err.Error())
	}
	// With no explicit element/point, scroll over the app's window center rather
	// than at the resolveXY default of (0,0) — otherwise the gesture (and the
	// cursor) jumps to the display's top-left corner and scrolls the wrong thing.
	if a.ElementIndex == nil && a.X == nil && a.Y == nil {
		wf := r.state.WindowFrame
		x = int(wf.X + wf.Width/2)
		y = int(wf.Y + wf.Height/2)
	}
	elementIndex := -1
	if a.ElementIndex != nil {
		elementIndex = *a.ElementIndex
	}
	err = s.act.Scroll(ctx, actor.ScrollRequest{
		BundleIdentifier: a.BundleIdentifier,
		ElementIndex:     elementIndex,
		X:                x,
		Y:                y,
		Direction:        a.Direction,
		Pages:            a.Pages,
	})
	s.markStale(a.BundleIdentifier)
	if err != nil {
		return toolError("scroll: " + err.Error())
	}
	return toolOK(textContent("scroll ok"))
}

// toolDrag issues a press-move-release gesture between two points.
func (s *Server) toolDrag(ctx context.Context, a toolArgs) map[string]any {
	r := s.requireReady(ctx, a.BundleIdentifier)
	if r.err != "" {
		return toolError(r.err)
	}
	err := s.act.Drag(ctx, actor.DragRequest{
		BundleIdentifier: a.BundleIdentifier,
		FromX:            a.FromX,
		FromY:            a.FromY,
		ToX:              a.ToX,
		ToY:              a.ToY,
	})
	s.markStale(a.BundleIdentifier)
	if err != nil {
		return toolError("drag: " + err.Error())
	}
	return toolOK(textContent("drag ok"))
}

// toolPerformSecondaryAction resolves the target coordinates and issues a
// secondary (right-button) click.
func (s *Server) toolPerformSecondaryAction(ctx context.Context, a toolArgs) map[string]any {
	r := s.requireReady(ctx, a.BundleIdentifier)
	if r.err != "" {
		return toolError(r.err)
	}
	x, y, err := resolveXY(r.state, a)
	if err != nil {
		return toolError("perform_secondary_action: " + err.Error())
	}
	elementIndex := -1
	if a.ElementIndex != nil {
		elementIndex = *a.ElementIndex
	}
	err = s.act.PerformSecondaryAction(ctx, actor.SecondaryActionRequest{
		BundleIdentifier: a.BundleIdentifier,
		ElementIndex:     elementIndex,
		X:                x,
		Y:                y,
	})
	s.markStale(a.BundleIdentifier)
	if err != nil {
		return toolError("perform_secondary_action: " + err.Error())
	}
	return toolOK(textContent("perform_secondary_action ok"))
}

// toolList returns the advertised tool schemas.
func toolList() []map[string]any {
	bundleProp := map[string]any{"bundle_identifier": map[string]any{
		"type":        "string",
		"description": "The macOS application bundle identifier to target, e.g. com.apple.Notes.",
	}}
	elementCoord := merge(bundleProp, map[string]any{
		"element_index": map[string]any{"type": "integer", "description": "Index of an element from the last get_app_state observation; overrides x/y when present."},
		"x":             map[string]any{"type": "integer"},
		"y":             map[string]any{"type": "integer"},
	})
	return []map[string]any{
		schema("get_app_state", "Observe an app's window, accessibility element tree, and any operating instructions. Must be called before any other action on that app.", bundleProp, []string{"bundle_identifier"}),
		schema("list_apps", "List the apps available to target.", map[string]any{}, nil),
		schema("click", "Click an element (by index) or a point, in a previously observed app.", merge(elementCoord, map[string]any{
			"button": map[string]any{"type": "string", "enum": []string{"left", "right", "middle"}},
		}), []string{"bundle_identifier"}),
		schema("type_text", "Type literal text into the focused element of an app.", merge(bundleProp, map[string]any{
			"text": map[string]any{"type": "string"},
		}), []string{"bundle_identifier", "text"}),
		schema("press_key", "Press a key or key chord in an app.", merge(bundleProp, map[string]any{
			"keys": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}), []string{"bundle_identifier", "keys"}),
		schema("scroll", "Scroll at an element (by index) or a point, in a previously observed app.", merge(elementCoord, map[string]any{
			"direction": map[string]any{"type": "string", "enum": []string{"up", "down", "left", "right"}},
			"pages":     map[string]any{"type": "integer"},
		}), []string{"bundle_identifier", "direction", "pages"}),
		schema("drag", "Drag from one point to another in an app.", merge(bundleProp, map[string]any{
			"from_x": map[string]any{"type": "integer"},
			"from_y": map[string]any{"type": "integer"},
			"to_x":   map[string]any{"type": "integer"},
			"to_y":   map[string]any{"type": "integer"},
		}), []string{"bundle_identifier", "from_x", "from_y", "to_x", "to_y"}),
		schema("perform_secondary_action", "Perform a secondary (right-button) click on an element (by index) or a point, in a previously observed app.", elementCoord, []string{"bundle_identifier"}),
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
