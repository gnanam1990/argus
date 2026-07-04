package normalize

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gnanam1990/argus/pkg/action"
)

// anthropicInput is the computer-use tool's action schema (computer_20251124).
type anthropicInput struct {
	Action          string  `json:"action"`
	Coordinate      []int   `json:"coordinate"`
	StartCoordinate []int   `json:"start_coordinate"`
	Text            string  `json:"text"`
	ScrollDirection string  `json:"scroll_direction"`
	ScrollAmount    int     `json:"scroll_amount"`
	Duration        float64 `json:"duration"`
}

// Anthropic maps an Anthropic computer-tool input JSON to a canonical action.
// It returns an error for unparseable input, an unknown action, or an action
// that fails canonical validation; callers substitute Repair() on error.
func Anthropic(raw []byte) (action.Action, error) {
	var in anthropicInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return action.Action{}, fmt.Errorf("normalize anthropic: %w", err)
	}

	a := action.Action{Mark: action.NoMark, Button: action.Left}

	switch in.Action {
	case "key", "hold_key":
		a.Type = action.Key
		a.Keys = splitKeys(in.Text)
	case "type":
		a.Type = action.Type
		a.Text = in.Text
	case "mouse_move":
		a.Type = action.Move
		a.Point = point(in.Coordinate)
	case "left_click":
		a.Type = action.Click
		a.Point = point(in.Coordinate)
	case "right_click":
		a.Type = action.Click
		a.Button = action.Right
		a.Point = point(in.Coordinate)
	case "middle_click":
		a.Type = action.Click
		a.Button = action.Middle
		a.Point = point(in.Coordinate)
	case "double_click":
		a.Type = action.DoubleClick
		a.Point = point(in.Coordinate)
	case "triple_click":
		a.Type = action.TripleClick
		a.Point = point(in.Coordinate)
	case "left_click_drag":
		a.Type = action.Drag
		a.Path = []action.Point{point(in.StartCoordinate), point(in.Coordinate)}
	case "left_mouse_down":
		a.Type = action.MouseDown
		a.Point = point(in.Coordinate)
	case "left_mouse_up":
		a.Type = action.MouseUp
		a.Point = point(in.Coordinate)
	case "scroll":
		a.Type = action.Scroll
		a.Point = point(in.Coordinate)
		a.DX, a.DY = scrollDelta(in.ScrollDirection, in.ScrollAmount)
	case "screenshot":
		a.Type = action.Screenshot
	case "cursor_position":
		a.Type = action.CursorPosition
	case "wait":
		a.Type = action.Wait
		d := in.Duration
		if d <= 0 {
			d = 1
		}
		a.Dur = time.Duration(d * float64(time.Second))
	default:
		return action.Action{}, fmt.Errorf("normalize anthropic: unknown action %q", in.Action)
	}

	if err := a.Validate(); err != nil {
		return action.Action{}, fmt.Errorf("normalize anthropic %q: %w", in.Action, err)
	}
	return a, nil
}

func point(c []int) action.Point {
	if len(c) >= 2 {
		return action.Point{X: c[0], Y: c[1]}
	}
	return action.Point{}
}

// scrollDelta converts a direction + amount into signed wheel deltas. A missing
// amount defaults to one notch so the resulting Scroll is always non-zero.
func scrollDelta(dir string, amount int) (dx, dy int) {
	if amount <= 0 {
		amount = 1
	}
	switch dir {
	case "up":
		return 0, -amount
	case "down":
		return 0, amount
	case "left":
		return -amount, 0
	case "right":
		return amount, 0
	default:
		return 0, amount
	}
}

// splitKeys turns "ctrl+c" into ["ctrl","c"], lowercased and trimmed.
func splitKeys(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	parts := strings.Split(text, "+")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(strings.ToLower(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}
