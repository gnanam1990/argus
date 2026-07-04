package normalize

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gnanam1990/argus/pkg/action"
)

// openaiInput covers both the OpenAI computer_call schema (discriminator in
// "type") and the emulated function-tool schema (discriminator in "action").
// X/Y are pointers so a click/move with the key omitted entirely can be told
// apart from one explicitly at (0, 0) — see xyPoint. DX/DY/ScrollX/ScrollY
// decode as float64 (not int) because models routinely emit fractional
// pixels/deltas; roundCoord rounds them for the canonical (integer) Action.
type openaiInput struct {
	Action   string   `json:"action"`
	Type     string   `json:"type"`
	X        *float64 `json:"x"`
	Y        *float64 `json:"y"`
	Button   string   `json:"button"`
	Text     string   `json:"text"`
	Keys     []string `json:"keys"`
	DX       float64  `json:"dx"`
	DY       float64  `json:"dy"`
	ScrollX  float64  `json:"scroll_x"`
	ScrollY  float64  `json:"scroll_y"`
	Seconds  float64  `json:"seconds"`
	Duration float64  `json:"duration"`
}

// OpenAI maps an OpenAI-style computer action (native computer_call or the
// emulated function tool) to a canonical action, validating the result.
func OpenAI(raw []byte) (action.Action, error) {
	var in openaiInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return action.Action{}, fmt.Errorf("normalize openai: %w", err)
	}

	verb := in.Action
	if verb == "" {
		verb = in.Type
	}

	a := action.Action{Mark: action.NoMark, Button: button(in.Button)}
	pt, hasPt := xyPoint(in)

	switch verb {
	case "click", "left_click":
		a.Type = action.Click
		if !hasPt {
			return action.Action{}, missingCoordinateOpenAI(verb)
		}
		a.Point = pt
	case "right_click":
		a.Type = action.Click
		a.Button = action.Right
		if !hasPt {
			return action.Action{}, missingCoordinateOpenAI(verb)
		}
		a.Point = pt
	case "middle_click":
		a.Type = action.Click
		a.Button = action.Middle
		if !hasPt {
			return action.Action{}, missingCoordinateOpenAI(verb)
		}
		a.Point = pt
	case "double_click":
		a.Type = action.DoubleClick
		if !hasPt {
			return action.Action{}, missingCoordinateOpenAI(verb)
		}
		a.Point = pt
	case "triple_click":
		a.Type = action.TripleClick
		if !hasPt {
			return action.Action{}, missingCoordinateOpenAI(verb)
		}
		a.Point = pt
	case "move", "mouse_move":
		a.Type = action.Move
		if !hasPt {
			return action.Action{}, missingCoordinateOpenAI(verb)
		}
		a.Point = pt
	case "type":
		a.Type = action.Type
		a.Text = in.Text
	case "key", "keypress":
		a.Type = action.Key
		a.Keys = in.Keys
		if len(a.Keys) == 0 {
			a.Keys = splitKeys(in.Text)
		}
	case "scroll":
		// Not guarded like the click/move family: Validate only requires a
		// non-zero delta for Scroll, and a missing coordinate here just means
		// "scroll wherever the pointer already is" rather than aiming an
		// unintended click at the screen corner.
		a.Type = action.Scroll
		a.Point = pt
		a.DX, a.DY = openaiScroll(in)
	case "wait":
		a.Type = action.Wait
		d := in.Seconds
		if d <= 0 {
			d = in.Duration
		}
		if d <= 0 {
			d = 1
		}
		a.Dur = time.Duration(d * float64(time.Second))
	case "screenshot":
		a.Type = action.Screenshot
	case "cursor_position":
		a.Type = action.CursorPosition
	case "terminate", "done", "finish":
		a.Type = action.Terminate
	default:
		return action.Action{}, fmt.Errorf("normalize openai: unknown action %q", verb)
	}

	if err := a.Validate(); err != nil {
		return action.Action{}, fmt.Errorf("normalize openai %q: %w", verb, err)
	}
	return a, nil
}

// xyPoint resolves the flat x/y fields into a rounded Point, reporting
// ok=false when either coordinate is absent from the payload entirely (as
// opposed to present and 0) — see missingCoordinateOpenAI.
func xyPoint(in openaiInput) (action.Point, bool) {
	if in.X == nil || in.Y == nil {
		return action.Point{}, false
	}
	return action.Point{X: roundCoord(*in.X), Y: roundCoord(*in.Y)}, true
}

// missingCoordinateOpenAI reports a pointer-family action with no usable
// coordinate; normalize.OpenAI's caller treats any error as Repair(), so a
// click/move with no x/y is re-observed instead of silently landing at
// Point{0, 0} (the macOS Apple-menu corner).
func missingCoordinateOpenAI(verb string) error {
	return fmt.Errorf("normalize openai %q: missing coordinate", verb)
}

func button(s string) action.Button {
	switch s {
	case "right":
		return action.Right
	case "middle", "wheel":
		return action.Middle
	default:
		return action.Left
	}
}

// openaiScroll prefers dx/dy, then scroll_x/scroll_y, defaulting to one notch.
// The chosen pair is rounded once at the end (rather than per-field) so the
// zero-fallback checks above compare the model's actual fractional deltas,
// not a value already rounded down to 0.
func openaiScroll(in openaiInput) (dx, dy int) {
	fx, fy := in.DX, in.DY
	if fx == 0 && fy == 0 {
		fx, fy = in.ScrollX, in.ScrollY
	}
	dx, dy = roundCoord(fx), roundCoord(fy)
	if dx == 0 && dy == 0 {
		dy = 1
	}
	return dx, dy
}
