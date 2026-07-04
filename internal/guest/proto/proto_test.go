package proto

import (
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
)

func TestButtonRoundTrip(t *testing.T) {
	t.Parallel()
	for _, b := range []action.Button{action.Left, action.Right, action.Middle} {
		if got := Button(ButtonName(b)); got != b {
			t.Errorf("round-trip %v → %q → %v", b, ButtonName(b), got)
		}
	}
	if Button("nonsense") != action.Left {
		t.Error("unknown button should default to left")
	}
}

func TestCommands(t *testing.T) {
	t.Parallel()
	if len(Commands()) != 14 {
		t.Errorf("Commands() = %d, want 14", len(Commands()))
	}
}
