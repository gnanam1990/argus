package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// ctrl+c must always stop the run, even while an approval prompt is pending:
// the pending action is denied and the run cancelled.
func TestCtrlCDuringPendingApprovalCancels(t *testing.T) {
	t.Parallel()
	canceled := false
	m := NewModel("t", "openai", "gpt-5.5", func() { canceled = true })

	reply := make(chan bool, 1)
	m.Update(ApprovalMsg{Label: "run_command \"x\"", Reply: reply})

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !canceled {
		t.Fatal("ctrl+c during pending approval must cancel the run")
	}
	if cmd == nil {
		t.Fatal("ctrl+c must quit")
	}
	select {
	case ok := <-reply:
		if ok {
			t.Fatal("pending approval must be denied on ctrl+c")
		}
	default:
		t.Fatal("pending approval must receive a reply on ctrl+c")
	}
	if m.pending != nil {
		t.Fatal("pending must clear")
	}
}

// A failed run must render as a failure, not a green checkmark.
func TestViewErrorFooter(t *testing.T) {
	t.Parallel()
	m := testModel()
	m.Update(DoneMsg{Reason: "error", Steps: 2, Err: "provider step: codex api error (status 400)"})

	v := m.View()
	if !strings.Contains(v, "✗") {
		t.Errorf("error outcome must show ✗:\n%s", v)
	}
	if !strings.Contains(v, "codex api error") {
		t.Errorf("error text must be shown:\n%s", v)
	}
	if strings.Contains(v, "✔") {
		t.Errorf("error outcome must not show the success glyph:\n%s", v)
	}
}

// Non-completed, non-error endings render the warning glyph.
func TestViewWarnFooter(t *testing.T) {
	t.Parallel()
	m := testModel()
	m.Update(DoneMsg{Reason: "max_steps", Steps: 40})

	v := m.View()
	if !strings.Contains(v, "⚠") || strings.Contains(v, "✔") {
		t.Errorf("max_steps must render ⚠, not ✔:\n%s", v)
	}
}

// After the run ends the spinner must stop re-arming ticks.
func TestSpinnerStopsWhenDone(t *testing.T) {
	t.Parallel()
	m := testModel()
	m.Update(DoneMsg{Reason: "completed", Steps: 1})

	_, cmd := m.Update(spinner.TickMsg{})
	if cmd != nil {
		t.Error("spinner tick must not re-arm after done")
	}
}
