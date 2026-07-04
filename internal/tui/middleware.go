package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gnanam1990/argus/internal/middleware"
	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/agent"
	"github.com/gnanam1990/argus/pkg/model"
)

// sender is the subset of *tea.Program the TUI middleware needs (for testing).
type sender interface {
	Send(tea.Msg)
}

// NewProgram wraps a Model in a full-screen Bubble Tea program.
func NewProgram(m *Model) *tea.Program {
	return tea.NewProgram(m, tea.WithAltScreen())
}

// Middleware feeds agent-loop events into the TUI. It renders only; approval is
// handled by the Approver.
type Middleware struct {
	agent.Base
	send     sender
	provider string
	modelID  string
	step     int
}

// NewMiddleware builds the display middleware.
func NewMiddleware(s sender, provider, modelID string) *Middleware {
	return &Middleware{send: s, provider: provider, modelID: modelID}
}

// OnRunStart announces the task.
func (m *Middleware) OnRunStart(_ context.Context, task string) error {
	m.send.Send(startMsg{task: task, provider: m.provider, modelID: m.modelID})
	return nil
}

// OnLLMStart shows the thinking spinner.
func (m *Middleware) OnLLMStart(_ context.Context, _ *model.Conversation) error {
	m.send.Send(thinkingMsg{})
	return nil
}

// OnLLMEnd records the model's reasoning for this step.
func (m *Middleware) OnLLMEnd(_ context.Context, turn *model.Turn) error {
	m.step++
	m.send.Send(stepMsg{index: m.step, reasoning: turn.Text()})
	return nil
}

// OnActionResult shows an executed action.
func (m *Middleware) OnActionResult(_ context.Context, a action.Action, _ action.Result) error {
	m.send.Send(actionMsg{label: actionLabel(a), ok: true})
	return nil
}

// OnUsage accumulates token usage.
func (m *Middleware) OnUsage(_ context.Context, u model.Usage) error {
	m.send.Send(usageMsg{in: u.InputTokens, out: u.OutputTokens})
	return nil
}

// approver answers risky-action approvals inside the TUI.
type approver struct{ send sender }

// Approver builds a middleware.Approver that prompts in the TUI.
func Approver(s sender) middleware.Approver { return approver{send: s} }

// Approve sends an approval request to the TUI and waits for the reply.
func (a approver) Approve(ctx context.Context, act action.Action) (bool, error) {
	reply := make(chan bool, 1)
	a.send.Send(ApprovalMsg{Label: actionLabel(act), Reply: reply})
	select {
	case ok := <-reply:
		return ok, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func actionLabel(a action.Action) string {
	switch a.Type {
	case action.Click, action.DoubleClick, action.TripleClick, action.Move, action.MouseDown, action.MouseUp:
		if a.HasMark() {
			return fmt.Sprintf("%s mark %d", a.Type, a.Mark)
		}
		return fmt.Sprintf("%s (%d,%d)", a.Type, a.Point.X, a.Point.Y)
	case action.Type:
		return fmt.Sprintf("type %q", truncate(a.Text, 30))
	case action.Key:
		return "key " + strings.Join(a.Keys, "+")
	case action.RunCommand:
		return fmt.Sprintf("run_command %q", truncate(a.Text, 40))
	case action.ReadFile, action.WriteFile:
		return fmt.Sprintf("%s %s", a.Type, a.Text)
	case action.Scroll:
		return fmt.Sprintf("scroll (%d,%d)", a.DX, a.DY)
	default:
		return a.Type.String()
	}
}
