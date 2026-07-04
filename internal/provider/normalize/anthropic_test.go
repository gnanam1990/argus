package normalize

import (
	"testing"
	"time"

	"github.com/gnanam1990/argus/pkg/action"
)

func TestAnthropicMappings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want action.Action
	}{
		{
			"left_click", `{"action":"left_click","coordinate":[10,20]}`,
			action.Action{Type: action.Click, Button: action.Left, Point: action.Point{X: 10, Y: 20}, Mark: action.NoMark},
		},
		{
			"right_click", `{"action":"right_click","coordinate":[5,5]}`,
			action.Action{Type: action.Click, Button: action.Right, Point: action.Point{X: 5, Y: 5}, Mark: action.NoMark},
		},
		{
			"middle_click", `{"action":"middle_click","coordinate":[1,2]}`,
			action.Action{Type: action.Click, Button: action.Middle, Point: action.Point{X: 1, Y: 2}, Mark: action.NoMark},
		},
		{
			"double_click", `{"action":"double_click","coordinate":[3,4]}`,
			action.Action{Type: action.DoubleClick, Button: action.Left, Point: action.Point{X: 3, Y: 4}, Mark: action.NoMark},
		},
		{
			"triple_click", `{"action":"triple_click","coordinate":[3,4]}`,
			action.Action{Type: action.TripleClick, Button: action.Left, Point: action.Point{X: 3, Y: 4}, Mark: action.NoMark},
		},
		{
			"mouse_move", `{"action":"mouse_move","coordinate":[7,8]}`,
			action.Action{Type: action.Move, Button: action.Left, Point: action.Point{X: 7, Y: 8}, Mark: action.NoMark},
		},
		{
			"left_mouse_down", `{"action":"left_mouse_down","coordinate":[7,8]}`,
			action.Action{Type: action.MouseDown, Button: action.Left, Point: action.Point{X: 7, Y: 8}, Mark: action.NoMark},
		},
		{
			"left_mouse_up", `{"action":"left_mouse_up","coordinate":[7,8]}`,
			action.Action{Type: action.MouseUp, Button: action.Left, Point: action.Point{X: 7, Y: 8}, Mark: action.NoMark},
		},
		{
			"type", `{"action":"type","text":"hello"}`,
			action.Action{Type: action.Type, Button: action.Left, Text: "hello", Mark: action.NoMark},
		},
		{
			"key", `{"action":"key","text":"ctrl+c"}`,
			action.Action{Type: action.Key, Button: action.Left, Keys: []string{"ctrl", "c"}, Mark: action.NoMark},
		},
		{
			"hold_key maps to key", `{"action":"hold_key","text":"Shift"}`,
			action.Action{Type: action.Key, Button: action.Left, Keys: []string{"shift"}, Mark: action.NoMark},
		},
		{
			"screenshot", `{"action":"screenshot"}`,
			action.Action{Type: action.Screenshot, Button: action.Left, Mark: action.NoMark},
		},
		{
			"cursor_position", `{"action":"cursor_position"}`,
			action.Action{Type: action.CursorPosition, Button: action.Left, Mark: action.NoMark},
		},
		{
			"drag", `{"action":"left_click_drag","start_coordinate":[0,0],"coordinate":[10,10]}`,
			action.Action{Type: action.Drag, Button: action.Left, Path: []action.Point{{X: 0, Y: 0}, {X: 10, Y: 10}}, Mark: action.NoMark},
		},
		{
			"scroll down", `{"action":"scroll","coordinate":[5,5],"scroll_direction":"down","scroll_amount":3}`,
			action.Action{Type: action.Scroll, Button: action.Left, Point: action.Point{X: 5, Y: 5}, DY: 3, Mark: action.NoMark},
		},
		{
			"scroll up", `{"action":"scroll","coordinate":[5,5],"scroll_direction":"up","scroll_amount":2}`,
			action.Action{Type: action.Scroll, Button: action.Left, Point: action.Point{X: 5, Y: 5}, DY: -2, Mark: action.NoMark},
		},
		{
			"scroll left", `{"action":"scroll","coordinate":[5,5],"scroll_direction":"left","scroll_amount":4}`,
			action.Action{Type: action.Scroll, Button: action.Left, Point: action.Point{X: 5, Y: 5}, DX: -4, Mark: action.NoMark},
		},
		{
			"wait", `{"action":"wait","duration":2}`,
			action.Action{Type: action.Wait, Button: action.Left, Dur: 2 * time.Second, Mark: action.NoMark},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Anthropic([]byte(tt.raw))
			if err != nil {
				t.Fatalf("Anthropic(%s) error: %v", tt.raw, err)
			}
			if !actionsEqual(got, tt.want) {
				t.Errorf("Anthropic(%s)\n got  %+v\n want %+v", tt.raw, got, tt.want)
			}
			// Every mapped action must be canonically valid.
			if err := got.Validate(); err != nil {
				t.Errorf("mapped action failed Validate: %v", err)
			}
		})
	}
}

func TestAnthropicScrollDefaults(t *testing.T) {
	t.Parallel()
	// No direction/amount → defaults to one notch down, keeping Scroll valid.
	got, err := Anthropic([]byte(`{"action":"scroll","coordinate":[1,1]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.DY != 1 || got.DX != 0 {
		t.Errorf("default scroll = (%d,%d), want (0,1)", got.DX, got.DY)
	}
}

func TestAnthropicWaitDefault(t *testing.T) {
	t.Parallel()
	got, err := Anthropic([]byte(`{"action":"wait"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Dur != time.Second {
		t.Errorf("default wait = %s, want 1s", got.Dur)
	}
}

func TestAnthropicErrors(t *testing.T) {
	t.Parallel()
	bad := []string{
		`{"action":"teleport"}`, // unknown action
		`{"action":"type"}`,     // empty text fails Validate
		`{"action":"key"}`,      // no keys fails Validate
		`not json`,              // parse error
	}
	for _, raw := range bad {
		if _, err := Anthropic([]byte(raw)); err == nil {
			t.Errorf("Anthropic(%s) = nil error, want error", raw)
		}
	}
}

func TestRepairIsSafeScreenshot(t *testing.T) {
	t.Parallel()
	a := Repair()
	if a.Type != action.Screenshot {
		t.Errorf("Repair() = %s, want screenshot", a.Type)
	}
	if err := a.Validate(); err != nil {
		t.Errorf("Repair() action must be valid: %v", err)
	}
}

// actionsEqual compares the fields the normalizer sets.
func actionsEqual(a, b action.Action) bool {
	if a.Type != b.Type || a.Button != b.Button || a.Point != b.Point ||
		a.Text != b.Text || a.DX != b.DX || a.DY != b.DY || a.Dur != b.Dur || a.Mark != b.Mark {
		return false
	}
	if len(a.Keys) != len(b.Keys) || len(a.Path) != len(b.Path) {
		return false
	}
	for i := range a.Keys {
		if a.Keys[i] != b.Keys[i] {
			return false
		}
	}
	for i := range a.Path {
		if a.Path[i] != b.Path[i] {
			return false
		}
	}
	return true
}
