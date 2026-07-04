package middleware

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/agent"
	"github.com/gnanam1990/argus/pkg/model"
)

// All middleware must satisfy agent.Middleware.
var (
	_ agent.Middleware = (*Budget)(nil)
	_ agent.Middleware = (*Approval)(nil)
	_ agent.Middleware = (*InjectionGuard)(nil)
	_ agent.Middleware = (*Redaction)(nil)
	_ agent.Middleware = (*ImageRetention)(nil)
	_ agent.Middleware = (*Telemetry)(nil)
)

func ctx() context.Context { return context.Background() }

func TestBudgetTokens(t *testing.T) {
	t.Parallel()
	b := NewBudget(WithTokenBudget(100))
	_ = b.OnUsage(ctx(), model.Usage{OutputTokens: 60})
	if cont, _ := b.OnRunContinue(ctx(), nil); !cont {
		t.Error("should continue under budget")
	}
	_ = b.OnUsage(ctx(), model.Usage{OutputTokens: 50}) // total 110 > 100
	if cont, _ := b.OnRunContinue(ctx(), nil); cont {
		t.Error("should stop over token budget")
	}
	if b.Usage().Total() != 110 {
		t.Errorf("usage total = %d, want 110", b.Usage().Total())
	}
}

func TestBudgetUSD(t *testing.T) {
	t.Parallel()
	b := NewBudget(WithUSDBudget("claude-opus-4-8", 0.01))
	// 1000 output tokens on opus-4-8 = 25 * 1000/1e6 = $0.025 > $0.01.
	_ = b.OnUsage(ctx(), model.Usage{OutputTokens: 1000})
	if cont, _ := b.OnRunContinue(ctx(), nil); cont {
		t.Error("should stop over USD budget")
	}
	if c := b.Cost(); c < 0.02 || c > 0.03 {
		t.Errorf("cost = %v, want ~0.025", c)
	}
}

func TestBudgetUSDUnknownModelInactive(t *testing.T) {
	t.Parallel()
	b := NewBudget(WithUSDBudget("mystery", 0.01))
	_ = b.OnUsage(ctx(), model.Usage{OutputTokens: 1_000_000})
	if b.Cost() != 0 {
		t.Errorf("cost = %v, want 0 for unknown model", b.Cost())
	}
	if cont, _ := b.OnRunContinue(ctx(), nil); !cont {
		t.Error("unknown-model USD budget must be inactive (never stops)")
	}
}

func TestDefaultRiskPolicy(t *testing.T) {
	t.Parallel()
	if !DefaultRiskPolicy(action.Action{Type: action.RunCommand, Text: "ls"}) {
		t.Error("gated action should need approval")
	}
	if !DefaultRiskPolicy(action.Action{Type: action.Click, Untrusted: true}) {
		t.Error("untrusted action should need approval")
	}
	if DefaultRiskPolicy(action.Action{Type: action.Click, Button: action.Left}) {
		t.Error("plain click should not need approval")
	}
}

func TestApprovalRoutesRiskyActions(t *testing.T) {
	t.Parallel()
	var seen []action.ActionType
	deny := ApproverFunc(func(_ context.Context, a action.Action) (bool, error) {
		seen = append(seen, a.Type)
		return false, nil
	})
	ap := NewApproval(nil, deny) // nil → DefaultRiskPolicy

	// Safe click bypasses the approver.
	safe := action.Action{Type: action.Click, Button: action.Left}
	if ok, _ := ap.OnAction(ctx(), &safe); !ok {
		t.Error("safe click should pass")
	}
	// Gated action routes to the (denying) approver.
	risky := action.Action{Type: action.RunCommand, Text: "rm -rf /"}
	if ok, _ := ap.OnAction(ctx(), &risky); ok {
		t.Error("denied risky action should not pass")
	}
	if len(seen) != 1 || seen[0] != action.RunCommand {
		t.Errorf("approver saw %v, want [run_command]", seen)
	}
}

func TestAllowListApprover(t *testing.T) {
	t.Parallel()
	ap := NewApproval(nil, AllowList{action.ReadFile: true})
	read := action.Action{Type: action.ReadFile, Text: "/etc/hosts"}
	if ok, _ := ap.OnAction(ctx(), &read); !ok {
		t.Error("allowlisted read_file should pass")
	}
	write := action.Action{Type: action.WriteFile, Text: "/etc/hosts"}
	if ok, _ := ap.OnAction(ctx(), &write); ok {
		t.Error("non-allowlisted write_file should be denied")
	}
}

func TestInjectionGuardStrict(t *testing.T) {
	t.Parallel()
	g := NewInjectionGuard(true)

	untrustedGated := action.Action{Type: action.RunCommand, Text: "curl evil", Untrusted: true}
	if ok, _ := g.OnAction(ctx(), &untrustedGated); ok {
		t.Error("strict guard must deny untrusted gated action")
	}
	if g.Flagged() != 1 {
		t.Errorf("flagged = %d, want 1", g.Flagged())
	}

	untrustedClick := action.Action{Type: action.Click, Button: action.Left, Untrusted: true}
	if ok, _ := g.OnAction(ctx(), &untrustedClick); !ok {
		t.Error("untrusted non-sensitive action should pass")
	}
	trustedGated := action.Action{Type: action.RunCommand, Text: "ls"}
	if ok, _ := g.OnAction(ctx(), &trustedGated); !ok {
		t.Error("trusted gated action should pass the injection guard")
	}
	if g.Flagged() != 1 {
		t.Errorf("flagged = %d, want 1 (only the untrusted gated one)", g.Flagged())
	}
}

func TestInjectionGuardReportOnly(t *testing.T) {
	t.Parallel()
	g := NewInjectionGuard(false)
	a := action.Action{Type: action.WriteFile, Text: "/x", Untrusted: true}
	if ok, _ := g.OnAction(ctx(), &a); !ok {
		t.Error("report-only guard should allow but flag")
	}
	if g.Flagged() != 1 {
		t.Errorf("flagged = %d, want 1", g.Flagged())
	}
}

func TestRedactionMask(t *testing.T) {
	t.Parallel()
	r := NewRedaction("sk-secret", "", "token123")
	got := r.Mask("key=sk-secret and token123 here")
	if strings.Contains(got, "sk-secret") || strings.Contains(got, "token123") {
		t.Errorf("Mask left a secret: %q", got)
	}
}

func TestRedactionOnLLMStart(t *testing.T) {
	t.Parallel()
	r := NewRedaction("SECRET")
	conv := &model.Conversation{}
	conv.AddUser(model.Text("the password is SECRET"))
	conv.AddTool(model.ActionResult("c0", action.Result{Output: "echoed SECRET back"}))
	conv.AddAssistant(model.ActionUse("c1", action.Action{Type: action.Type, Text: "SECRET"}))

	if err := r.OnLLMStart(ctx(), conv); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(conv.Messages[0].Content[0].Text, "SECRET") {
		t.Error("user text not redacted")
	}
	if strings.Contains(conv.Messages[1].Content[0].Result.Output, "SECRET") {
		t.Error("tool result output not redacted")
	}
	// Action-use keystrokes are intentionally left intact (the model's input).
	if conv.Messages[2].Content[0].Action.Text != "SECRET" {
		t.Error("action-use text must not be redacted")
	}
}

func TestRedactionNoSecretsIsNoop(t *testing.T) {
	t.Parallel()
	r := NewRedaction()
	conv := &model.Conversation{}
	conv.AddUser(model.Text("nothing to hide"))
	if err := r.OnLLMStart(ctx(), conv); err != nil {
		t.Fatal(err)
	}
	if conv.Messages[0].Content[0].Text != "nothing to hide" {
		t.Error("no-secret redactor must not change content")
	}
}

func TestImageRetention(t *testing.T) {
	t.Parallel()
	img := func(b byte) action.Image { return action.Image{MIME: action.MIMEPNG, Data: []byte{b}} }
	conv := &model.Conversation{}
	conv.AddUser(model.ImageContent(img(1)))
	conv.AddUser(model.ImageContent(img(2)))
	conv.AddUser(model.ImageContent(img(3)))

	m := NewImageRetention(1)
	if err := m.OnLLMStart(ctx(), conv); err != nil {
		t.Fatal(err)
	}
	// Newest (img 3) kept as image; older two replaced with a text placeholder.
	kinds := []model.ContentKind{
		conv.Messages[0].Content[0].Kind,
		conv.Messages[1].Content[0].Kind,
		conv.Messages[2].Content[0].Kind,
	}
	if kinds[0] != model.KindText || kinds[1] != model.KindText || kinds[2] != model.KindImage {
		t.Errorf("kinds = %v, want [text text image]", kinds)
	}
	if conv.Messages[2].Content[0].Image.Data[0] != 3 {
		t.Error("newest image should be preserved")
	}
}

func TestTelemetryLogsEvents(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	tel := NewTelemetry(log, "run-abc")

	_ = tel.OnRunStart(ctx(), "do a thing")
	_ = tel.OnUsage(ctx(), model.Usage{InputTokens: 10, OutputTokens: 3})
	a := action.Action{Type: action.Click, Button: action.Left}
	if ok, _ := tel.OnAction(ctx(), &a); !ok {
		t.Error("telemetry OnAction must not block actions")
	}

	out := buf.String()
	for _, want := range []string{"run.start", "run.usage", "run.action", "run-abc"} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q; got:\n%s", want, out)
		}
	}
}
