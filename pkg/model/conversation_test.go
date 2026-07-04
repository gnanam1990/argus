package model

import (
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
)

func TestRoleString(t *testing.T) {
	t.Parallel()
	cases := map[Role]string{
		RoleSystem:    "system",
		RoleUser:      "user",
		RoleAssistant: "assistant",
		RoleTool:      "tool",
		Role(99):      "unknown",
	}
	for r, want := range cases {
		if got := r.String(); got != want {
			t.Errorf("Role(%d).String() = %q, want %q", r, got, want)
		}
	}
}

func TestContentKindString(t *testing.T) {
	t.Parallel()
	cases := map[ContentKind]string{
		KindText:         "text",
		KindImage:        "image",
		KindActionUse:    "action_use",
		KindActionResult: "action_result",
		ContentKind(99):  "unknown",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("ContentKind(%d).String() = %q, want %q", k, got, want)
		}
	}
}

func TestContentConstructors(t *testing.T) {
	t.Parallel()

	if c := Text("hi"); c.Kind != KindText || c.Text != "hi" {
		t.Errorf("Text() = %+v", c)
	}
	img := action.Image{MIME: action.MIMEPNG, Data: []byte{1}}
	if c := ImageContent(img); c.Kind != KindImage || c.Image.MIME != action.MIMEPNG {
		t.Errorf("ImageContent() = %+v", c)
	}
	a := action.Action{Type: action.Click, Button: action.Left}
	if c := ActionUse("call-0", a); c.Kind != KindActionUse || c.CallID != "call-0" || c.Action.Type != action.Click {
		t.Errorf("ActionUse() = %+v", c)
	}
	r := action.Result{Output: "ok"}
	if c := ActionResult("call-0", r); c.Kind != KindActionResult || c.CallID != "call-0" || c.Result.Output != "ok" {
		t.Errorf("ActionResult() = %+v", c)
	}
}

func TestConversationAddHelpers(t *testing.T) {
	t.Parallel()
	var conv Conversation
	conv.System = "you are a test"
	conv.AddUser(Text("do the thing"))
	conv.AddAssistant(ActionUse("call-0", action.Action{Type: action.Screenshot}))
	conv.AddTool(ActionResult("call-0", action.Result{}))

	if conv.Len() != 3 {
		t.Fatalf("Len = %d, want 3", conv.Len())
	}
	roles := []Role{RoleUser, RoleAssistant, RoleTool}
	for i, want := range roles {
		if conv.Messages[i].Role != want {
			t.Errorf("message[%d].Role = %s, want %s", i, conv.Messages[i].Role, want)
		}
	}
}

func TestConversationCloneIsDeepAndIndependent(t *testing.T) {
	t.Parallel()
	orig := &Conversation{System: "sys"}
	orig.AddAssistant(ActionUse("call-0", action.Action{
		Type: action.Key,
		Keys: []string{"ctrl", "c"},
		Path: []action.Point{{X: 1, Y: 2}},
	}))
	orig.AddTool(ActionResult("call-0", action.Result{
		Screenshot: action.Image{MIME: action.MIMEPNG, Data: []byte{0xAA, 0xBB}},
	}))

	clone := orig.Clone()

	// Mutate every mutable field of the original after cloning.
	orig.System = "mutated"
	orig.Messages[0].Content[0].Action.Keys[0] = "MUTATED"
	orig.Messages[0].Content[0].Action.Path[0].X = 999
	orig.Messages[1].Content[0].Result.Screenshot.Data[0] = 0xFF
	orig.AddUser(Text("appended after clone"))

	// The clone must be untouched.
	if clone.System != "sys" {
		t.Errorf("clone.System = %q, want %q", clone.System, "sys")
	}
	if clone.Len() != 2 {
		t.Errorf("clone.Len = %d, want 2 (append to original must not affect clone)", clone.Len())
	}
	if got := clone.Messages[0].Content[0].Action.Keys[0]; got != "ctrl" {
		t.Errorf("clone key mutated: got %q, want %q", got, "ctrl")
	}
	if got := clone.Messages[0].Content[0].Action.Path[0].X; got != 1 {
		t.Errorf("clone path mutated: got %d, want 1", got)
	}
	if got := clone.Messages[1].Content[0].Result.Screenshot.Data[0]; got != 0xAA {
		t.Errorf("clone screenshot mutated: got %#x, want 0xAA", got)
	}
}

func TestConversationCloneNil(t *testing.T) {
	t.Parallel()
	var c *Conversation
	if c.Clone() != nil {
		t.Error("nil.Clone() should be nil")
	}
	empty := (&Conversation{}).Clone()
	if empty == nil || empty.Len() != 0 {
		t.Error("empty conversation should clone to an empty, non-nil conversation")
	}
}
