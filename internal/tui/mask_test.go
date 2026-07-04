package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/model"
)

// Registered secrets must never render in the live feed: reasoning, action
// labels, and approval prompts all pass through the mask.
func TestMiddlewareMasksDisplayedText(t *testing.T) {
	t.Parallel()
	mask := func(s string) string { return strings.ReplaceAll(s, "hunter2", "«redacted»") }

	fs := &sliceSender{}
	mw := NewMiddleware(fs, "openai", "m")
	mw.SetMask(mask)
	ctx := context.Background()

	turn := &model.Turn{Message: model.AssistantMessage(model.Text("the password is hunter2"))}
	if err := mw.OnLLMEnd(ctx, turn); err != nil {
		t.Fatal(err)
	}
	typeAct := action.Action{Type: action.Type, Text: "hunter2", Mark: action.NoMark}
	if err := mw.OnActionResult(ctx, typeAct, action.Result{}); err != nil {
		t.Fatal(err)
	}

	for _, msg := range fs.all() {
		switch m := msg.(type) {
		case stepMsg:
			if strings.Contains(m.reasoning, "hunter2") {
				t.Errorf("reasoning leaked secret: %q", m.reasoning)
			}
		case actionMsg:
			if strings.Contains(m.label, "hunter2") {
				t.Errorf("action label leaked secret: %q", m.label)
			}
		}
	}
}

func TestMaskedApproverMasksLabel(t *testing.T) {
	t.Parallel()
	mask := func(s string) string { return strings.ReplaceAll(s, "hunter2", "«redacted»") }
	cs := chanSender{ch: make(chan tea.Msg, 1)}

	ap := MaskedApprover(cs, mask)
	go func() {
		_, _ = ap.Approve(context.Background(), action.Action{Type: action.RunCommand, Text: "echo hunter2"})
	}()

	msg := <-cs.ch
	am, ok := msg.(ApprovalMsg)
	if !ok {
		t.Fatalf("expected ApprovalMsg, got %#v", msg)
	}
	if strings.Contains(am.Label, "hunter2") {
		t.Errorf("approval label leaked secret: %q", am.Label)
	}
	am.Reply <- false
}
