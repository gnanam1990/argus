package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func testModel() *Model {
	m := NewModel("do the thing", "openai", "gpt-5.5", func() {})
	m.now = func() time.Time { return time.Unix(2000, 0) }
	// Widen so the bordered box does not wrap short test strings.
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	return m
}

func key(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestModelLifecycle(t *testing.T) {
	t.Parallel()
	m := testModel()

	m.Update(startMsg{task: "click submit", provider: "openai", modelID: "gpt-5.5"})
	if m.task != "click submit" || m.started.IsZero() {
		t.Fatalf("startMsg not applied: task=%q started=%v", m.task, m.started)
	}

	m.Update(thinkingMsg{})
	if !m.thinking {
		t.Fatal("thinkingMsg should set thinking")
	}

	m.Update(stepMsg{index: 1, reasoning: "clicking the button"})
	if m.step != 1 || m.thinking {
		t.Fatalf("stepMsg: step=%d thinking=%v", m.step, m.thinking)
	}
	if len(m.feed) != 1 || m.feed[0].kind != "reasoning" {
		t.Fatalf("reasoning not pushed: %+v", m.feed)
	}

	// Empty reasoning should not add a feed item.
	m.Update(stepMsg{index: 2, reasoning: "   "})
	if len(m.feed) != 1 {
		t.Fatalf("blank reasoning should be skipped: %+v", m.feed)
	}

	m.Update(actionMsg{label: "click (820,540)", ok: true})
	if last := m.feed[len(m.feed)-1]; last.kind != "action" || last.text != "click (820,540)" {
		t.Fatalf("actionMsg not pushed: %+v", last)
	}

	m.Update(usageMsg{in: 1000, out: 200})
	m.Update(usageMsg{in: 5, out: 2})
	if m.inTok != 1005 || m.outTok != 202 {
		t.Fatalf("usage not accumulated: in=%d out=%d", m.inTok, m.outTok)
	}

	m.Update(DoneMsg{Reason: "completed", Steps: 4, FinalText: "done"})
	if !m.done || m.reason != "completed" || m.step != 4 {
		t.Fatalf("DoneMsg not applied: %+v", m)
	}
}

func TestModelApprovalKeys(t *testing.T) {
	t.Parallel()
	m := testModel()

	reply := make(chan bool, 1)
	m.Update(ApprovalMsg{Label: `run_command "rm -rf build"`, Reply: reply})
	if m.pending == nil {
		t.Fatal("ApprovalMsg should set pending")
	}

	m.Update(key("y"))
	select {
	case ok := <-reply:
		if !ok {
			t.Fatal("y should approve")
		}
	default:
		t.Fatal("y should send a reply")
	}
	if m.pending != nil {
		t.Fatal("pending should clear after reply")
	}
	if last := m.feed[len(m.feed)-1]; last.kind != "action" {
		t.Fatalf("approval should push an approved item: %+v", last)
	}

	// Deny path.
	reply2 := make(chan bool, 1)
	m.Update(ApprovalMsg{Label: "click (1,2)", Reply: reply2})
	m.Update(key("n"))
	if got := <-reply2; got {
		t.Fatal("n should deny")
	}
	if last := m.feed[len(m.feed)-1]; last.kind != "error" {
		t.Fatalf("deny should push an error item: %+v", last)
	}
}

func TestModelQuitCancels(t *testing.T) {
	t.Parallel()
	canceled := false
	m := NewModel("t", "openai", "gpt-5.5", func() { canceled = true })

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !canceled {
		t.Fatal("ctrl+c should cancel the run")
	}
	if cmd == nil {
		t.Fatal("ctrl+c should return a quit command")
	}
}

func TestModelQuitAfterDoneDoesNotCancel(t *testing.T) {
	t.Parallel()
	canceled := false
	m := NewModel("t", "openai", "gpt-5.5", func() { canceled = true })
	m.Update(DoneMsg{Reason: "completed", Steps: 1})

	_, cmd := m.Update(key("q"))
	if canceled {
		t.Fatal("quitting after completion must not cancel")
	}
	if cmd == nil {
		t.Fatal("q should still quit the program")
	}
}

func TestViewContent(t *testing.T) {
	t.Parallel()
	m := testModel()
	m.Update(startMsg{task: "click submit", provider: "openai", modelID: "gpt-5.5"})
	m.Update(stepMsg{index: 3, reasoning: "thinking about it"})
	m.Update(actionMsg{label: "click (820,540)", ok: true})
	reply := make(chan bool, 1)
	m.Update(ApprovalMsg{Label: `run_command "rm -rf build"`, Reply: reply})

	v := m.View()
	for _, want := range []string{"argus", "gpt-5.5", "step 3", "click (820,540)", "approve", "rm -rf build"} {
		if !strings.Contains(v, want) {
			t.Errorf("View missing %q\n%s", want, v)
		}
	}
}

func TestViewDoneFooter(t *testing.T) {
	t.Parallel()
	m := testModel()
	m.Update(DoneMsg{Reason: "completed", Steps: 4, FinalText: "all set"})
	v := m.View()
	if !strings.Contains(v, "completed") || !strings.Contains(v, "all set") {
		t.Errorf("done footer missing content:\n%s", v)
	}
}

func TestFeedCap(t *testing.T) {
	t.Parallel()
	m := testModel()
	for i := 0; i < 300; i++ {
		m.push(feedItem{kind: "action", text: "x"})
	}
	if len(m.feed) > 200 {
		t.Fatalf("feed should be capped at 200, got %d", len(m.feed))
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()
	if got := truncate("hello world", 5); got != "hell…" {
		t.Errorf("truncate = %q", got)
	}
	if got := truncate("hi", 10); got != "hi" {
		t.Errorf("short string should pass through, got %q", got)
	}
	if got := truncate("line\nbreak", 100); got != "line break" {
		t.Errorf("newlines should be flattened, got %q", got)
	}
}
