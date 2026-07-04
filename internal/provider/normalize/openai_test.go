package normalize

import (
	"testing"
	"time"

	"github.com/gnanam1990/argus/pkg/action"
)

func TestOpenAIMappings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want action.Action
	}{
		{
			"emulated click", `{"action":"click","x":10,"y":20}`,
			action.Action{Type: action.Click, Button: action.Left, Point: action.Point{X: 10, Y: 20}, Mark: action.NoMark},
		},
		{
			"native computer_call click", `{"type":"click","x":3,"y":4,"button":"right"}`,
			action.Action{Type: action.Click, Button: action.Right, Point: action.Point{X: 3, Y: 4}, Mark: action.NoMark},
		},
		{
			"double_click", `{"action":"double_click","x":1,"y":1}`,
			action.Action{Type: action.DoubleClick, Button: action.Left, Point: action.Point{X: 1, Y: 1}, Mark: action.NoMark},
		},
		{
			"type", `{"action":"type","text":"hi"}`,
			action.Action{Type: action.Type, Button: action.Left, Text: "hi", Mark: action.NoMark},
		},
		{
			"keypress with keys", `{"type":"keypress","keys":["ctrl","c"]}`,
			action.Action{Type: action.Key, Button: action.Left, Keys: []string{"ctrl", "c"}, Mark: action.NoMark},
		},
		{
			"key from text", `{"action":"key","text":"ctrl+v"}`,
			action.Action{Type: action.Key, Button: action.Left, Keys: []string{"ctrl", "v"}, Mark: action.NoMark},
		},
		{
			"scroll dy", `{"action":"scroll","x":5,"y":5,"dy":3}`,
			action.Action{Type: action.Scroll, Button: action.Left, Point: action.Point{X: 5, Y: 5}, DY: 3, Mark: action.NoMark},
		},
		{
			"scroll native scroll_y", `{"type":"scroll","x":5,"y":5,"scroll_y":-2}`,
			action.Action{Type: action.Scroll, Button: action.Left, Point: action.Point{X: 5, Y: 5}, DY: -2, Mark: action.NoMark},
		},
		{
			"move", `{"action":"move","x":7,"y":8}`,
			action.Action{Type: action.Move, Button: action.Left, Point: action.Point{X: 7, Y: 8}, Mark: action.NoMark},
		},
		{
			"wait seconds", `{"action":"wait","seconds":2}`,
			action.Action{Type: action.Wait, Button: action.Left, Dur: 2 * time.Second, Mark: action.NoMark},
		},
		{
			"screenshot", `{"action":"screenshot"}`,
			action.Action{Type: action.Screenshot, Button: action.Left, Mark: action.NoMark},
		},
		{
			"terminate", `{"action":"terminate"}`,
			action.Action{Type: action.Terminate, Button: action.Left, Mark: action.NoMark},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := OpenAI([]byte(tt.raw))
			if err != nil {
				t.Fatalf("OpenAI(%s): %v", tt.raw, err)
			}
			if !actionsEqual(got, tt.want) {
				t.Errorf("OpenAI(%s)\n got  %+v\n want %+v", tt.raw, got, tt.want)
			}
			if err := got.Validate(); err != nil {
				t.Errorf("mapped action invalid: %v", err)
			}
		})
	}
}

func TestOpenAIErrors(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{`{"action":"levitate"}`, `{"action":"type"}`, `not json`} {
		if _, err := OpenAI([]byte(raw)); err == nil {
			t.Errorf("OpenAI(%s) = nil error, want error", raw)
		}
	}
}

func TestOpenAIScrollDefault(t *testing.T) {
	t.Parallel()
	got, err := OpenAI([]byte(`{"action":"scroll","x":1,"y":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.DY != 1 {
		t.Errorf("default scroll dy = %d, want 1", got.DY)
	}
}
