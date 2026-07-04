package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/agent"
	"github.com/gnanam1990/argus/pkg/computer"
	compfake "github.com/gnanam1990/argus/pkg/computer/fake"
	"github.com/gnanam1990/argus/pkg/model"
	providerfake "github.com/gnanam1990/argus/pkg/model/fake"
)

// A terminate must stop the batch: nothing after it may execute, and history
// must still end with a paired tool-result message.
func TestRunTerminateStopsBatch(t *testing.T) {
	t.Parallel()
	prov := providerfake.New(
		model.ActionTurn(model.Usage{},
			action.Action{Type: action.Terminate},
			clickAt(10, 10),
		),
	)
	comp := compfake.New()
	r := agent.NewRunner(prov, comp)

	out, err := r.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Reason != agent.ReasonTerminated {
		t.Errorf("Reason = %q, want terminated", out.Reason)
	}
	for _, c := range comp.Calls() {
		if c.Method == "Click" {
			t.Error("click after terminate in the same batch must not execute")
		}
	}

	msgs := r.History().Messages
	last := msgs[len(msgs)-1]
	if last.Role != model.RoleTool {
		t.Fatalf("history must end with the tool results, got role %v", last.Role)
	}
	if len(last.Content) != 1 {
		t.Errorf("only the terminate should have a result, got %d", len(last.Content))
	}
}

// Every error path must stamp ReasonError so callers can trust the outcome.
func TestRunErrorSetsReasonError(t *testing.T) {
	t.Parallel()
	prov := providerfake.New().ThenError(errors.New("boom"))
	r := agent.NewRunner(prov, compfake.New())

	out, err := r.Run(context.Background(), "task")
	if err == nil {
		t.Fatal("expected error")
	}
	if out == nil || out.Reason != agent.ReasonError {
		t.Errorf("outcome = %+v, want Reason=error", out)
	}
}

// usageErrMW fails on the first usage report.
type usageErrMW struct{ agent.Base }

func (usageErrMW) OnUsage(context.Context, model.Usage) error { return errors.New("budget blown") }

// The turn that trips an aborting hook must still be accounted for.
func TestRunUsageAccountedBeforeHookAbort(t *testing.T) {
	t.Parallel()
	prov := providerfake.New(model.EndTurn("done", model.Usage{InputTokens: 100, OutputTokens: 50}))
	r := agent.NewRunner(prov, compfake.New(), agent.WithMiddleware(usageErrMW{}))

	out, err := r.Run(context.Background(), "task")
	if err == nil {
		t.Fatal("expected hook error")
	}
	if out.Steps != 1 || out.Usage.Total() != 150 {
		t.Errorf("steps=%d usage=%+v; the tipping turn must be counted", out.Steps, out.Usage)
	}
	if out.Reason != agent.ReasonError {
		t.Errorf("Reason = %q, want error", out.Reason)
	}
}

// captureMW records the Untrusted flag the approval gate observes.
type captureMW struct {
	agent.Base
	untrusted *[]bool
}

func (c captureMW) OnAction(_ context.Context, a *action.Action) (bool, error) {
	*c.untrusted = append(*c.untrusted, a.Untrusted)
	return true, nil
}

// Actions issued after an observation are conservatively untrusted: their
// values may derive from on-screen content.
func TestRunActionsUntrustedAfterObservation(t *testing.T) {
	t.Parallel()
	prov := providerfake.New(
		model.ActionTurn(model.Usage{}, clickAt(5, 5)),
		model.EndTurn("done", model.Usage{}),
	)
	var seen []bool
	r := agent.NewRunner(prov, compfake.New(), agent.WithMiddleware(captureMW{untrusted: &seen}))

	if _, err := r.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(seen) == 0 {
		t.Fatal("no actions observed")
	}
	for i, u := range seen {
		if !u {
			t.Errorf("action %d not marked untrusted after observation", i)
		}
	}
}

// WithCapabilities must accumulate across repeated options.
func TestWithCapabilitiesAccumulates(t *testing.T) {
	t.Parallel()
	prov := providerfake.New(
		model.ActionTurn(model.Usage{}, action.Action{Type: action.RunCommand, Text: "ls", Mark: action.NoMark}),
	)
	r := agent.NewRunner(prov, compfake.New(),
		agent.WithCapabilities(action.RunCommand),
		agent.WithCapabilities(action.ReadFile),
	)

	_, err := r.Run(context.Background(), "task")
	// The fake computer implements no Commander, so an *allowed* run_command
	// surfaces ErrUnsupported — reaching it (rather than ErrCapabilityDenied)
	// proves the first grant survived the second WithCapabilities option.
	if !errors.Is(err, computer.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported (capability gate passed)", err)
	}
}
