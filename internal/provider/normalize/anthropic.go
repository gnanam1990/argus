package normalize

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gnanam1990/argus/pkg/action"
)

// anthropicInput is the computer-use tool's action schema (computer_20251124).
// Coordinate/StartCoordinate decode as floats (not int) because models
// routinely emit fractional pixels (e.g. "coordinate":[820.5, 400]); see
// coordinate() and roundCoord.
type anthropicInput struct {
	Action          string    `json:"action"`
	Coordinate      []float64 `json:"coordinate"`
	StartCoordinate []float64 `json:"start_coordinate"`
	Text            string    `json:"text"`
	ScrollDirection string    `json:"scroll_direction"`
	ScrollAmount    int       `json:"scroll_amount"`
	Duration        float64   `json:"duration"`
}

// Anthropic maps an Anthropic computer-tool input JSON to a canonical action.
// It returns an error for unparseable input, an unknown action, a missing
// coordinate on a pointer-family action, or an action that fails canonical
// validation; callers substitute Repair() on error.
func Anthropic(raw []byte) (action.Action, error) {
	var in anthropicInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return action.Action{}, fmt.Errorf("normalize anthropic: %w", err)
	}

	a := action.Action{Mark: action.NoMark, Button: action.Left}
	// Resolved once: every case below that needs a single coordinate (all but
	// scroll and left_click_drag, which have their own rules) shares it.
	pt, ptErr := coordinate(in.Coordinate)

	switch in.Action {
	case "key", "hold_key":
		a.Type = action.Key
		a.Keys = splitKeys(in.Text)
	case "type":
		a.Type = action.Type
		a.Text = in.Text
	case "mouse_move":
		a.Type = action.Move
		if ptErr != nil {
			return action.Action{}, missingCoordinate(in.Action)
		}
		a.Point = pt
	case "left_click":
		a.Type = action.Click
		if ptErr != nil {
			return action.Action{}, missingCoordinate(in.Action)
		}
		a.Point = pt
	case "right_click":
		a.Type = action.Click
		a.Button = action.Right
		if ptErr != nil {
			return action.Action{}, missingCoordinate(in.Action)
		}
		a.Point = pt
	case "middle_click":
		a.Type = action.Click
		a.Button = action.Middle
		if ptErr != nil {
			return action.Action{}, missingCoordinate(in.Action)
		}
		a.Point = pt
	case "double_click":
		a.Type = action.DoubleClick
		if ptErr != nil {
			return action.Action{}, missingCoordinate(in.Action)
		}
		a.Point = pt
	case "triple_click":
		a.Type = action.TripleClick
		if ptErr != nil {
			return action.Action{}, missingCoordinate(in.Action)
		}
		a.Point = pt
	case "left_click_drag":
		a.Type = action.Drag
		start, startErr := coordinate(in.StartCoordinate)
		if startErr != nil || ptErr != nil {
			return action.Action{}, missingCoordinate(in.Action)
		}
		a.Path = []action.Point{start, pt}
	case "left_mouse_down":
		a.Type = action.MouseDown
		if ptErr != nil {
			return action.Action{}, missingCoordinate(in.Action)
		}
		a.Point = pt
	case "left_mouse_up":
		a.Type = action.MouseUp
		if ptErr != nil {
			return action.Action{}, missingCoordinate(in.Action)
		}
		a.Point = pt
	case "scroll":
		// Unlike the click/move/drag family, a missing coordinate here is not
		// guarded: scroll is valid on DX/DY alone (Validate has no point
		// requirement for it), and models commonly scroll "wherever the
		// pointer already is" without restating a coordinate.
		a.Type = action.Scroll
		a.Point = pt
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

// coordinate resolves a 2-element [x, y] coordinate pair, rounding each
// fractional pixel to the nearest integer. It reports an error when the pair
// is absent or short (as opposed to present and legitimately (0, 0)) so
// callers on the click/move/drag family can refuse to silently fall back to
// Point{0, 0} — the macOS Apple-menu corner — for a tool call that never gave
// a coordinate at all.
func coordinate(c []float64) (action.Point, error) {
	if len(c) < 2 {
		return action.Point{}, fmt.Errorf("missing coordinate")
	}
	return action.Point{X: roundCoord(c[0]), Y: roundCoord(c[1])}, nil
}

// missingCoordinate reports a pointer-family action with no usable
// coordinate; normalize.Anthropic's caller treats any error as Repair().
func missingCoordinate(verb string) error {
	return fmt.Errorf("normalize anthropic %q: missing coordinate", verb)
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

// splitKeys turns "ctrl+c" into ["ctrl","c"], lowercased and trimmed. A
// literal "+" key is written by doubling or trailing the separator (the
// classic escape for a delimiter that is also a valid token), so "ctrl++"
// means "ctrl" then a literal "+", and "+" alone means just the literal "+"
// key: an empty segment produced by splitting on "+" is itself the literal
// "+", except a final empty segment with nothing before it in the same gap,
// which is just "no token after the last separator" and is dropped.
func splitKeys(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	parts := strings.Split(text, "+")
	out := make([]string, 0, len(parts))
	for i, p := range parts {
		if p == "" {
			if i == len(parts)-1 && len(out) > 0 {
				continue // trailing separator: nothing follows it
			}
			out = append(out, "+")
			continue
		}
		if p = strings.TrimSpace(strings.ToLower(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}
