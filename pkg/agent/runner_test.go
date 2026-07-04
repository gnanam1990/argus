package agent_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/agent"
	"github.com/gnanam1990/argus/pkg/computer"
	compfake "github.com/gnanam1990/argus/pkg/computer/fake"
	"github.com/gnanam1990/argus/pkg/grounder"
	grounderfake "github.com/gnanam1990/argus/pkg/grounder/fake"
	"github.com/gnanam1990/argus/pkg/model"
	providerfake "github.com/gnanam1990/argus/pkg/model/fake"
	"github.com/gnanam1990/argus/pkg/trajectory"
)

// pngOf builds a decodable w×h PNG so the loop's scale computation has real
// image dimensions to work with.
func pngOf(t *testing.T, w, h int) action.Image {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.White)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return action.Image{MIME: action.MIMEPNG, Data: buf.Bytes()}
}

func clickAt(x, y int) action.Action {
	return action.Action{Type: action.Click, Button: action.Left, Point: action.Point{X: x, Y: y}, Mark: action.NoMark}
}

func TestRunCompletesAndRecords(t *testing.T) {
	t.Parallel()
	prov := providerfake.New(
		model.ActionTurn(model.Usage{OutputTokens: 5}, clickAt(10, 10)),
		model.EndTurn("all done", model.Usage{InputTokens: 3, OutputTokens: 2}),
	)
	comp := compfake.New()
	rec := trajectory.NewMemory(trajectory.NewManifest("task"))
	r := agent.NewRunner(prov, comp, agent.WithTrajectory(rec))

	out, err := r.Run(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Reason != agent.ReasonCompleted {
		t.Errorf("Reason = %q, want completed", out.Reason)
	}
	if out.Steps != 2 {
		t.Errorf("Steps = %d, want 2", out.Steps)
	}
	if out.FinalText != "all done" {
		t.Errorf("FinalText = %q", out.FinalText)
	}
	if out.Usage.OutputTokens != 7 {
		t.Errorf("cumulative output tokens = %d, want 7", out.Usage.OutputTokens)
	}
	if rec.Len() != 2 {
		t.Fatalf("recorded steps = %d, want 2", rec.Len())
	}
	steps := rec.Steps()
	if len(steps[0].Actions) != 1 || steps[0].Actions[0].Type != action.Click {
		t.Errorf("step 0 actions = %+v", steps[0].Actions)
	}
	if steps[1].Text != "all done" {
		t.Errorf("step 1 text = %q", steps[1].Text)
	}
}

func TestRunClickIsScaledFromScreenshotSize(t *testing.T) {
	t.Parallel()
	// screenshot 50x50, screen 100x100 → scale 2x. Click at (10,10) → (20,20).
	prov := providerfake.New(
		model.ActionTurn(model.Usage{}, clickAt(10, 10)),
		model.EndTurn("done", model.Usage{}),
	)
	comp := compfake.New().WithScreenshot(pngOf(t, 50, 50), 100, 100)
	r := agent.NewRunner(prov, comp)

	if _, err := r.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var click *compfake.Call
	for _, c := range comp.Calls() {
		c := c
		if c.Method == "Click" {
			click = &c
		}
	}
	if click == nil {
		t.Fatal("no Click recorded")
	}
	if click.X != 20 || click.Y != 20 {
		t.Errorf("click at (%d,%d), want (20,20) after 2x scale", click.X, click.Y)
	}
}

func TestRunTerminateAction(t *testing.T) {
	t.Parallel()
	prov := providerfake.New(
		model.ActionTurn(model.Usage{}, action.Action{Type: action.Terminate}),
	)
	r := agent.NewRunner(prov, compfake.New())
	out, err := r.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Reason != agent.ReasonTerminated {
		t.Errorf("Reason = %q, want terminated", out.Reason)
	}
}

func TestRunMaxSteps(t *testing.T) {
	t.Parallel()
	// Provider always requests another click; only WithMaxSteps stops it.
	prov := providerfake.New().
		Then(model.ActionTurn(model.Usage{}, clickAt(1, 1))).
		Then(model.ActionTurn(model.Usage{}, clickAt(1, 1))).
		Then(model.ActionTurn(model.Usage{}, clickAt(1, 1)))
	r := agent.NewRunner(prov, compfake.New(), agent.WithMaxSteps(2))
	out, err := r.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Reason != agent.ReasonMaxSteps {
		t.Errorf("Reason = %q, want max_steps", out.Reason)
	}
	if out.Steps != 2 {
		t.Errorf("Steps = %d, want 2", out.Steps)
	}
}

// haltMW halts the run at OnRunContinue after N steps.
type haltMW struct {
	agent.Base
	after int
}

func (h haltMW) OnRunContinue(_ context.Context, st *agent.State) (bool, error) {
	return st.Step < h.after, nil
}

func TestRunHaltedByMiddleware(t *testing.T) {
	t.Parallel()
	prov := providerfake.New().
		Then(model.ActionTurn(model.Usage{}, clickAt(1, 1))).
		Then(model.ActionTurn(model.Usage{}, clickAt(1, 1)))
	r := agent.NewRunner(prov, compfake.New(), agent.WithMiddleware(haltMW{after: 1}))
	out, err := r.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Reason != agent.ReasonHalted {
		t.Errorf("Reason = %q, want halted", out.Reason)
	}
	if out.Steps != 1 {
		t.Errorf("Steps = %d, want 1", out.Steps)
	}
}

// denyMW denies every action at the approval gate.
type denyMW struct{ agent.Base }

func (denyMW) OnAction(context.Context, *action.Action) (bool, error) { return false, nil }

func TestRunApprovalGateSkipsAction(t *testing.T) {
	t.Parallel()
	prov := providerfake.New(
		model.ActionTurn(model.Usage{}, clickAt(10, 10)),
		model.EndTurn("done", model.Usage{}),
	)
	comp := compfake.New()
	r := agent.NewRunner(prov, comp, agent.WithMiddleware(denyMW{}))
	out, err := r.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Reason != agent.ReasonCompleted {
		t.Errorf("Reason = %q, want completed", out.Reason)
	}
	for _, c := range comp.Calls() {
		if c.Method == "Click" {
			t.Error("denied click must not reach the computer")
		}
	}
}

// recordMW records the order hooks fire in.
type recordMW struct {
	agent.Base
	events *[]string
}

func (m recordMW) OnRunStart(context.Context, string) error {
	*m.events = append(*m.events, "run_start")
	return nil
}
func (m recordMW) OnLLMStart(context.Context, *model.Conversation) error {
	*m.events = append(*m.events, "llm_start")
	return nil
}
func (m recordMW) OnLLMEnd(context.Context, *model.Turn) error {
	*m.events = append(*m.events, "llm_end")
	return nil
}
func (m recordMW) OnAction(_ context.Context, _ *action.Action) (bool, error) {
	*m.events = append(*m.events, "action")
	return true, nil
}
func (m recordMW) OnActionResult(context.Context, action.Action, action.Result) error {
	*m.events = append(*m.events, "action_result")
	return nil
}
func (m recordMW) OnScreenshot(context.Context, action.Image) error {
	*m.events = append(*m.events, "screenshot")
	return nil
}
func (m recordMW) OnUsage(context.Context, model.Usage) error {
	*m.events = append(*m.events, "usage")
	return nil
}

func TestRunMiddlewareHookOrder(t *testing.T) {
	t.Parallel()
	prov := providerfake.New(
		model.ActionTurn(model.Usage{}, clickAt(1, 1)),
		model.EndTurn("done", model.Usage{}),
	)
	var events []string
	r := agent.NewRunner(prov, compfake.New(), agent.WithMiddleware(recordMW{events: &events}))
	if _, err := r.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// First few events must be: run_start, screenshot (initial observe),
	// llm_start, llm_end, usage, action, action_result, ...
	want := []string{"run_start", "screenshot", "llm_start", "llm_end", "usage", "action", "action_result"}
	if len(events) < len(want) {
		t.Fatalf("events = %v, want prefix %v", events, want)
	}
	for i, w := range want {
		if events[i] != w {
			t.Errorf("events[%d] = %q, want %q (full: %v)", i, events[i], w, events)
		}
	}
}

func TestRunReObservesAfterAction(t *testing.T) {
	t.Parallel()
	prov := providerfake.New(
		model.ActionTurn(model.Usage{}, clickAt(1, 1)),
		model.EndTurn("done", model.Usage{}),
	)
	comp := compfake.New()
	r := agent.NewRunner(prov, comp)
	if _, err := r.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Two observations (initial + re-observe) → two Screenshot calls.
	screenshots := 0
	for _, c := range comp.Calls() {
		if c.Method == "Screenshot" {
			screenshots++
		}
	}
	if screenshots != 2 {
		t.Errorf("Screenshot calls = %d, want 2 (initial + re-observe)", screenshots)
	}
	// The re-observation appears as a user image immediately after the tool
	// (action-result) message.
	msgs := r.History().Messages
	reobserved := false
	for i, m := range msgs {
		if m.Role == model.RoleTool && i+1 < len(msgs) {
			next := msgs[i+1]
			if next.Role == model.RoleUser && len(next.Content) > 0 && next.Content[0].Kind == model.KindImage {
				reobserved = true
			}
		}
	}
	if !reobserved {
		t.Errorf("expected a user image observation after the tool message; history: %+v", msgs)
	}
}

func TestRunWithGrounderResolvesMark(t *testing.T) {
	t.Parallel()
	// Non-native provider → grounding engages. Model clicks mark 3, whose box
	// centers at (10,10); scale 1 → click (10,10).
	prov := providerfake.New(
		model.ActionTurn(model.Usage{}, action.Action{Type: action.Click, Button: action.Left, Mark: 3}),
		model.EndTurn("done", model.Usage{}),
	).WithCapabilities(model.Capabilities{NativeComputerUse: false, Vision: true})

	gr := grounderfake.NewGrounder(grounder.Element{
		ID: 3, Box: action.Rect{Min: action.Point{X: 0, Y: 0}, Max: action.Point{X: 20, Y: 20}},
		Interactable: true, Confidence: 0.9,
	})
	comp := compfake.New()
	r := agent.NewRunner(prov, comp,
		agent.WithSystemPrompt("you are a test agent"),
		agent.WithGrounder(gr, grounderfake.Marker{}, 0.5),
		agent.WithModelOptions(model.WithMaxTokens(100)),
	)

	out, err := r.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Reason != agent.ReasonCompleted {
		t.Errorf("Reason = %q", out.Reason)
	}
	if r.History().System != "you are a test agent" {
		t.Errorf("system prompt not set: %q", r.History().System)
	}
	var click *compfake.Call
	for _, c := range comp.Calls() {
		c := c
		if c.Method == "Click" {
			click = &c
		}
	}
	if click == nil || click.X != 10 || click.Y != 10 {
		t.Errorf("mark click = %+v, want (10,10)", click)
	}
}

func TestRunProviderError(t *testing.T) {
	t.Parallel()
	prov := providerfake.New().ThenError(errors.New("provider boom"))
	r := agent.NewRunner(prov, compfake.New())
	out, err := r.Run(context.Background(), "task")
	if err == nil {
		t.Fatal("expected provider error")
	}
	if out.Reason != "error" {
		t.Errorf("Reason = %q, want error", out.Reason)
	}
}

func TestRunGatedActionDeniedByDefault(t *testing.T) {
	t.Parallel()
	prov := providerfake.New(model.ActionTurn(model.Usage{}, action.Action{Type: action.RunCommand, Text: "ls"}))
	r := agent.NewRunner(prov, compfake.New())
	if _, err := r.Run(context.Background(), "task"); !errors.Is(err, computer.ErrCapabilityDenied) {
		t.Errorf("err = %v, want ErrCapabilityDenied", err)
	}
}

func TestRunWithCapabilitiesPassesGate(t *testing.T) {
	t.Parallel()
	prov := providerfake.New(model.ActionTurn(model.Usage{}, action.Action{Type: action.RunCommand, Text: "ls"}))
	r := agent.NewRunner(prov, compfake.New(), agent.WithCapabilities(action.RunCommand))
	// Passes the capability gate; a bare Computer can't run commands → unsupported.
	if _, err := r.Run(context.Background(), "task"); !errors.Is(err, computer.ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported (gate passed, executor can't run it)", err)
	}
}

func TestRunCancelledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	prov := providerfake.New(model.EndTurn("x", model.Usage{}))
	r := agent.NewRunner(prov, compfake.New())
	// Initial observe may succeed (fake ignores ctx), but the loop's ctx check
	// stops before stepping; either way Run returns the cancellation error.
	_, err := r.Run(ctx, "task")
	if err == nil {
		t.Error("expected context cancellation error")
	}
}
