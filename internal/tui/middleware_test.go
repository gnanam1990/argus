package tui

import (
	"context"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/model"
)

// sliceSender records every message for inspection.
type sliceSender struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (s *sliceSender) Send(m tea.Msg) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, m)
}

func (s *sliceSender) all() []tea.Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]tea.Msg(nil), s.msgs...)
}

func TestMiddlewareEvents(t *testing.T) {
	t.Parallel()
	fs := &sliceSender{}
	mw := NewMiddleware(fs, "openai", "gpt-5.5")
	ctx := context.Background()

	if err := mw.OnRunStart(ctx, "click submit"); err != nil {
		t.Fatal(err)
	}
	if err := mw.OnLLMStart(ctx, &model.Conversation{}); err != nil {
		t.Fatal(err)
	}
	turn := &model.Turn{
		Message: model.AssistantMessage(model.Text("clicking the button")),
		Usage:   model.Usage{InputTokens: 100, OutputTokens: 20},
	}
	if err := mw.OnLLMEnd(ctx, turn); err != nil {
		t.Fatal(err)
	}
	act := action.Action{Type: action.Click, Point: action.Point{X: 820, Y: 540}, Mark: action.NoMark}
	if err := mw.OnActionResult(ctx, act, action.Result{}); err != nil {
		t.Fatal(err)
	}
	if err := mw.OnUsage(ctx, model.Usage{InputTokens: 100, OutputTokens: 20}); err != nil {
		t.Fatal(err)
	}

	msgs := fs.all()
	if len(msgs) != 5 {
		t.Fatalf("want 5 messages, got %d: %#v", len(msgs), msgs)
	}
	if sm, ok := msgs[0].(startMsg); !ok || sm.task != "click submit" || sm.modelID != "gpt-5.5" {
		t.Errorf("msg0 = %#v", msgs[0])
	}
	if _, ok := msgs[1].(thinkingMsg); !ok {
		t.Errorf("msg1 = %#v", msgs[1])
	}
	if st, ok := msgs[2].(stepMsg); !ok || st.index != 1 || st.reasoning != "clicking the button" {
		t.Errorf("msg2 = %#v", msgs[2])
	}
	if am, ok := msgs[3].(actionMsg); !ok || am.label != "click (820,540)" || !am.ok {
		t.Errorf("msg3 = %#v", msgs[3])
	}
	if um, ok := msgs[4].(usageMsg); !ok || um.in != 100 || um.out != 20 {
		t.Errorf("msg4 = %#v", msgs[4])
	}
}

func TestMiddlewareStepCounter(t *testing.T) {
	t.Parallel()
	fs := &sliceSender{}
	mw := NewMiddleware(fs, "openai", "m")
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_ = mw.OnLLMEnd(ctx, &model.Turn{})
	}
	msgs := fs.all()
	for i, msg := range msgs {
		if st := msg.(stepMsg); st.index != i+1 {
			t.Errorf("step %d has index %d", i, st.index)
		}
	}
}

// chanSender hands each message off on a channel so a blocking approver can be
// driven from the test goroutine.
type chanSender struct{ ch chan tea.Msg }

func (c chanSender) Send(m tea.Msg) { c.ch <- m }

func TestApproverApprove(t *testing.T) {
	t.Parallel()
	cs := chanSender{ch: make(chan tea.Msg, 1)}
	ap := Approver(cs)
	act := action.Action{Type: action.RunCommand, Text: "rm -rf build"}

	res := make(chan bool, 1)
	go func() {
		ok, err := ap.Approve(context.Background(), act)
		if err != nil {
			t.Errorf("approve err: %v", err)
		}
		res <- ok
	}()

	msg := <-cs.ch
	am, ok := msg.(ApprovalMsg)
	if !ok {
		t.Fatalf("expected ApprovalMsg, got %#v", msg)
	}
	if am.Label != `run_command "rm -rf build"` {
		t.Errorf("label = %q", am.Label)
	}
	am.Reply <- true
	if !<-res {
		t.Fatal("expected approval")
	}
}

func TestApproverContextCancel(t *testing.T) {
	t.Parallel()
	cs := chanSender{ch: make(chan tea.Msg, 1)}
	ap := Approver(cs)
	ctx, cancel := context.WithCancel(context.Background())

	res := make(chan error, 1)
	go func() {
		_, err := ap.Approve(ctx, action.Action{Type: action.Click, Mark: action.NoMark})
		res <- err
	}()

	<-cs.ch // approval request emitted
	cancel()
	if err := <-res; err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestActionLabel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a    action.Action
		want string
	}{
		{action.Action{Type: action.Click, Point: action.Point{X: 1, Y: 2}, Mark: action.NoMark}, "click (1,2)"},
		{action.Action{Type: action.Click, Mark: 7}, "click mark 7"},
		{action.Action{Type: action.RunCommand, Text: "ls"}, `run_command "ls"`},
		{action.Action{Type: action.Key, Keys: []string{"ctrl", "c"}}, "key ctrl+c"},
		{action.Action{Type: action.Type, Text: "hi"}, `type "hi"`},
		{action.Action{Type: action.Scroll, DX: 0, DY: 3, Mark: action.NoMark}, "scroll (0,3)"},
	}
	for _, c := range cases {
		if got := actionLabel(c.a); got != c.want {
			t.Errorf("actionLabel(%v) = %q, want %q", c.a.Type, got, c.want)
		}
	}
}
