package model

import (
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
)

func TestStopReasonString(t *testing.T) {
	t.Parallel()
	cases := map[StopReason]string{
		StopEnd:       "end",
		StopAction:    "action",
		StopMaxTokens: "max_tokens",
		StopUnknown:   "unknown",
		StopReason(9): "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("StopReason(%d).String() = %q, want %q", s, got, want)
		}
	}
}

func TestTurnText(t *testing.T) {
	t.Parallel()
	turn := &Turn{Message: AssistantMessage(
		Text("first"),
		ActionUse("call-0", action.Action{Type: action.Screenshot}),
		Text("second"),
	)}
	if got := turn.Text(); got != "first\nsecond" {
		t.Errorf("Text() = %q, want %q", got, "first\nsecond")
	}

	empty := &Turn{Message: AssistantMessage(ActionUse("call-0", action.Action{Type: action.Screenshot}))}
	if got := empty.Text(); got != "" {
		t.Errorf("Text() with no text parts = %q, want empty", got)
	}
}

func TestTurnActionUsesAndHasActions(t *testing.T) {
	t.Parallel()

	withActions := &Turn{Message: AssistantMessage(
		Text("thinking"),
		ActionUse("call-0", action.Action{Type: action.Click, Button: action.Left}),
		ActionUse("call-1", action.Action{Type: action.Type, Text: "hi"}),
	)}
	uses := withActions.ActionUses()
	if len(uses) != 2 {
		t.Fatalf("ActionUses len = %d, want 2", len(uses))
	}
	if uses[0].CallID != "call-0" || uses[1].CallID != "call-1" {
		t.Errorf("ActionUses call ids = %q,%q", uses[0].CallID, uses[1].CallID)
	}
	if !withActions.HasActions() {
		t.Error("HasActions = false, want true")
	}

	textOnly := &Turn{Message: AssistantMessage(Text("done"))}
	if textOnly.HasActions() {
		t.Error("HasActions = true for text-only turn, want false")
	}
	if len(textOnly.ActionUses()) != 0 {
		t.Error("ActionUses should be empty for text-only turn")
	}
}

func TestEndTurn(t *testing.T) {
	t.Parallel()
	u := Usage{InputTokens: 10, OutputTokens: 5}
	turn := EndTurn("all done", u)
	if turn.Stop != StopEnd {
		t.Errorf("Stop = %s, want end", turn.Stop)
	}
	if turn.HasActions() {
		t.Error("EndTurn must not have actions")
	}
	if turn.Text() != "all done" {
		t.Errorf("Text = %q, want %q", turn.Text(), "all done")
	}
	if turn.Usage != u {
		t.Errorf("Usage = %+v, want %+v", turn.Usage, u)
	}
	if turn.Message.Role != RoleAssistant {
		t.Errorf("Role = %s, want assistant", turn.Message.Role)
	}
}

func TestActionTurn(t *testing.T) {
	t.Parallel()
	turn := ActionTurn(Usage{OutputTokens: 3},
		action.Action{Type: action.Click, Button: action.Left},
		action.Action{Type: action.Type, Text: "hello"},
	)
	if turn.Stop != StopAction {
		t.Errorf("Stop = %s, want action", turn.Stop)
	}
	uses := turn.ActionUses()
	if len(uses) != 2 {
		t.Fatalf("ActionUses len = %d, want 2", len(uses))
	}
	// Call ids are stable and positional.
	if uses[0].CallID != "call-0" || uses[1].CallID != "call-1" {
		t.Errorf("call ids = %q, %q; want call-0, call-1", uses[0].CallID, uses[1].CallID)
	}
	if uses[0].Action.Type != action.Click || uses[1].Action.Type != action.Type {
		t.Errorf("action types = %s, %s", uses[0].Action.Type, uses[1].Action.Type)
	}
}
