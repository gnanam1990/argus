package normalize

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gnanam1990/argus/pkg/action"
)

// openaiInput covers both the OpenAI computer_call schema (discriminator in
// "type") and the emulated function-tool schema (discriminator in "action").
type openaiInput struct {
	Action   string   `json:"action"`
	Type     string   `json:"type"`
	X        int      `json:"x"`
	Y        int      `json:"y"`
	Button   string   `json:"button"`
	Text     string   `json:"text"`
	Keys     []string `json:"keys"`
	DX       int      `json:"dx"`
	DY       int      `json:"dy"`
	ScrollX  int      `json:"scroll_x"`
	ScrollY  int      `json:"scroll_y"`
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
	pt := action.Point{X: in.X, Y: in.Y}

	switch verb {
	case "click", "left_click":
		a.Type = action.Click
		a.Point = pt
	case "right_click":
		a.Type = action.Click
		a.Button = action.Right
		a.Point = pt
	case "middle_click":
		a.Type = action.Click
		a.Button = action.Middle
		a.Point = pt
	case "double_click":
		a.Type = action.DoubleClick
		a.Point = pt
	case "triple_click":
		a.Type = action.TripleClick
		a.Point = pt
	case "move", "mouse_move":
		a.Type = action.Move
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
func openaiScroll(in openaiInput) (dx, dy int) {
	dx, dy = in.DX, in.DY
	if dx == 0 && dy == 0 {
		dx, dy = in.ScrollX, in.ScrollY
	}
	if dx == 0 && dy == 0 {
		dy = 1
	}
	return dx, dy
}
