// Package tui is a Bubble Tea live view of an agent run. It is driven entirely
// by the agent loop's middleware events (see middleware.go), so it needs no
// changes to the loop: the same OnLLMEnd/OnAction/OnUsage/OnActionResult hooks
// that feed telemetry feed the display, and risky-action approvals are answered
// inline in the TUI.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gnanam1990/argus/internal/pricing"
	"github.com/gnanam1990/argus/pkg/agent"
	"github.com/gnanam1990/argus/pkg/model"
)

// Messages the middleware/approver send into the program.
type (
	startMsg    struct{ task, provider, modelID string }
	thinkingMsg struct{}
	stepMsg     struct {
		index     int
		reasoning string
	}
	actionMsg struct {
		label string
		ok    bool
		errs  string
	}
	usageMsg struct{ in, out int }
	// ApprovalMsg asks the user to approve a risky action; the reply is sent on
	// the channel. Exported because the approver constructs it.
	ApprovalMsg struct {
		Label string
		Reply chan bool
	}
	// DoneMsg ends the run.
	DoneMsg struct {
		Reason    string
		Steps     int
		FinalText string
		Err       string // non-empty when the run failed
	}
)

type feedItem struct {
	kind string // reasoning | action | error
	text string
	ok   bool
}

// Model is the Bubble Tea model for a live run.
type Model struct {
	task     string
	provider string
	modelID  string

	step   int
	inTok  int
	outTok int
	feed   []feedItem

	started  time.Time
	now      func() time.Time
	thinking bool
	pending  *ApprovalMsg
	done     bool
	reason   string
	final    string
	errText  string

	cancel context.CancelFunc
	sp     spinner.Model
	width  int
}

// NewModel builds the model. cancel is invoked when the user quits mid-run.
func NewModel(task, provider, modelID string, cancel context.CancelFunc) *Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	return &Model{
		task: task, provider: provider, modelID: modelID,
		cancel: cancel, sp: sp, now: time.Now, width: 72,
	}
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd { return m.sp.Tick }

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case startMsg:
		m.task, m.provider, m.modelID = msg.task, msg.provider, msg.modelID
		m.started = m.now()
		return m, nil

	case thinkingMsg:
		m.thinking = true
		return m, nil

	case stepMsg:
		m.thinking = false
		m.step = msg.index
		if r := strings.TrimSpace(msg.reasoning); r != "" {
			m.push(feedItem{kind: "reasoning", text: r})
		}
		return m, nil

	case actionMsg:
		it := feedItem{kind: "action", text: msg.label, ok: msg.ok}
		if !msg.ok && msg.errs != "" {
			it.kind, it.text = "error", msg.label+": "+msg.errs
		}
		m.push(it)
		return m, nil

	case usageMsg:
		m.inTok += msg.in
		m.outTok += msg.out
		return m, nil

	case ApprovalMsg:
		am := msg
		m.pending = &am
		return m, nil

	case DoneMsg:
		m.done, m.reason, m.step, m.final = true, msg.Reason, msg.Steps, msg.FinalText
		m.errText = msg.Err
		m.thinking = false
		return m, nil

	case spinner.TickMsg:
		if m.done {
			// Stop re-arming the tick so the program idles after the run ends.
			return m, nil
		}
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pending != nil {
		switch strings.ToLower(msg.String()) {
		case "y":
			m.pending.Reply <- true
			m.pending = nil
			m.push(feedItem{kind: "action", text: "approved", ok: true})
		case "n", "enter", "esc":
			m.pending.Reply <- false
			m.pending = nil
			m.push(feedItem{kind: "error", text: "denied"})
		case "ctrl+c":
			// Stopping must always work: deny the pending action, cancel the
			// run, and quit.
			m.pending.Reply <- false
			m.pending = nil
			m.push(feedItem{kind: "error", text: "denied (run cancelled)"})
			if !m.done && m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
		return m, nil
	}
	switch msg.String() {
	case "ctrl+c", "q":
		if !m.done && m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	}
	if m.done {
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) push(it feedItem) {
	m.feed = append(m.feed, it)
	const maxFeed = 200
	if len(m.feed) > maxFeed {
		m.feed = m.feed[len(m.feed)-maxFeed:]
	}
}

// Styles.
var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	warnStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

// View implements tea.Model.
func (m *Model) View() string {
	var b strings.Builder

	elapsed := time.Duration(0)
	if !m.started.IsZero() {
		elapsed = m.now().Sub(m.started).Truncate(time.Second)
	}
	cost := ""
	if c, ok := pricing.Cost(m.modelID, model.Usage{InputTokens: m.inTok, OutputTokens: m.outTok}); ok {
		cost = fmt.Sprintf(" · $%.4f", c)
	}
	header := headerStyle.Render(fmt.Sprintf("argus · %s · step %d · %s · %d tok%s",
		m.modelID, m.step, elapsed, m.inTok+m.outTok, cost))
	b.WriteString(header + "\n")
	if m.task != "" {
		b.WriteString(dimStyle.Render("task: "+truncate(m.task, m.width-8)) + "\n")
	}
	b.WriteString("\n")

	// Feed (last N lines that fit).
	tail := m.feed
	const shown = 14
	if len(tail) > shown {
		tail = tail[len(tail)-shown:]
	}
	for _, it := range tail {
		b.WriteString(m.renderItem(it) + "\n")
	}
	if m.thinking && !m.done {
		b.WriteString(dimStyle.Render(m.sp.View()+" thinking…") + "\n")
	}

	// Approval prompt.
	if m.pending != nil {
		b.WriteString("\n" + warnStyle.Render("⚠ approve "+m.pending.Label+"?  [y/N] ") + "\n")
	}

	// Footer. The glyph tracks the outcome: only a clean completion is green.
	b.WriteString("\n")
	switch {
	case m.done && (m.errText != "" || m.reason == agent.ReasonError):
		b.WriteString(errStyle.Render(fmt.Sprintf("✗ %s in %d steps", m.reason, m.step)))
		if m.errText != "" {
			b.WriteString(" — " + truncate(m.errText, m.width-20))
		}
		b.WriteString(dimStyle.Render("   (press q)"))
	case m.done && m.reason == agent.ReasonCompleted:
		b.WriteString(okStyle.Render(fmt.Sprintf("✔ %s in %d steps", m.reason, m.step)))
		if m.final != "" {
			b.WriteString(" — " + truncate(m.final, m.width-20))
		}
		b.WriteString(dimStyle.Render("   (press q)"))
	case m.done:
		// terminated / max_steps / halted: over, but not a clean completion.
		b.WriteString(warnStyle.Render(fmt.Sprintf("⚠ %s in %d steps", m.reason, m.step)))
		if m.final != "" {
			b.WriteString(" — " + truncate(m.final, m.width-20))
		}
		b.WriteString(dimStyle.Render("   (press q)"))
	default:
		b.WriteString(dimStyle.Render("ctrl-c to stop"))
	}
	return boxStyle.Width(min(m.width-2, 100)).Render(b.String())
}

func (m *Model) renderItem(it feedItem) string {
	switch it.kind {
	case "action":
		return okStyle.Render("  ✓ ") + it.text
	case "error":
		return errStyle.Render("  ✗ ") + it.text
	default:
		return dimStyle.Render("▸ " + truncate(it.text, m.width-6))
	}
}

func truncate(s string, n int) string {
	if n < 4 {
		n = 4
	}
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
