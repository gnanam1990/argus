package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gnanam1990/argus/pkg/agent"
	compfake "github.com/gnanam1990/argus/pkg/computer/fake"
	"github.com/gnanam1990/argus/pkg/model"
	providerfake "github.com/gnanam1990/argus/pkg/model/fake"
)

// The completing turn is the final word even when empty — a stale earlier
// step's text must not survive as FinalText.
func TestRunFinalTextNotStale(t *testing.T) {
	t.Parallel()
	// First turn carries reasoning text; the completing turn says nothing.
	first := model.ActionTurn(model.Usage{}, clickAt(1, 1))
	first.Message.Content = append([]model.Content{model.Text("checking the page")}, first.Message.Content...)
	prov := providerfake.New(first, model.EndTurn("", model.Usage{}))

	r := agent.NewRunner(prov, compfake.New())
	out, err := r.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.FinalText != "" {
		t.Errorf("FinalText = %q, want empty (the completing turn said nothing)", out.FinalText)
	}
}

// Cancellation between batched actions must stop execution even when the
// underlying driver ignores contexts.
func TestExecutorHonorsCancelledContext(t *testing.T) {
	t.Parallel()
	prov := providerfake.New(model.EndTurn("done", model.Usage{}))
	r := agent.NewRunner(prov, compfake.New())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out, err := r.Run(ctx, "task")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if out.Reason != agent.ReasonError {
		t.Errorf("Reason = %q, want error", out.Reason)
	}
}
