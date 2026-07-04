package fake

import (
	"context"
	"errors"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/model"
)

func TestPlaysBackTurnsInOrder(t *testing.T) {
	t.Parallel()
	t1 := model.ActionTurn(model.Usage{OutputTokens: 1}, action.Action{Type: action.Screenshot})
	t2 := model.EndTurn("done", model.Usage{OutputTokens: 2})
	p := New(t1, t2)

	ctx := context.Background()
	var conv model.Conversation

	got1, err := p.Step(ctx, &conv)
	if err != nil || got1 != t1 {
		t.Fatalf("step 1 = %v, %v; want t1, nil", got1, err)
	}
	got2, err := p.Step(ctx, &conv)
	if err != nil || got2 != t2 {
		t.Fatalf("step 2 = %v, %v; want t2, nil", got2, err)
	}
	if p.StepCount() != 2 {
		t.Errorf("StepCount = %d, want 2", p.StepCount())
	}
}

func TestExhaustionReturnsErr(t *testing.T) {
	t.Parallel()
	p := New(model.EndTurn("only", model.Usage{}))
	ctx := context.Background()
	var conv model.Conversation

	if _, err := p.Step(ctx, &conv); err != nil {
		t.Fatalf("first step erred: %v", err)
	}
	_, err := p.Step(ctx, &conv)
	if !errors.Is(err, ErrExhausted) {
		t.Errorf("exhausted step err = %v, want ErrExhausted", err)
	}
	// Calls are still recorded past exhaustion.
	if p.StepCount() != 2 {
		t.Errorf("StepCount = %d, want 2", p.StepCount())
	}
}

func TestThenAndThenError(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	end := model.EndTurn("ok", model.Usage{})
	p := New().Then(model.ActionTurn(model.Usage{}, action.Action{Type: action.Screenshot})).
		ThenError(boom).
		Then(end)

	ctx := context.Background()
	var conv model.Conversation

	if _, err := p.Step(ctx, &conv); err != nil {
		t.Fatalf("step 1 erred: %v", err)
	}
	if _, err := p.Step(ctx, &conv); !errors.Is(err, boom) {
		t.Fatalf("step 2 err = %v, want boom", err)
	}
	got, err := p.Step(ctx, &conv)
	if err != nil || got != end {
		t.Fatalf("step 3 = %v, %v; want end, nil", got, err)
	}
}

func TestRecordsIndependentSnapshot(t *testing.T) {
	t.Parallel()
	p := New(model.EndTurn("x", model.Usage{}))
	conv := &model.Conversation{System: "sys"}
	conv.AddUser(model.Text("original"))

	if _, err := p.Step(context.Background(), conv); err != nil {
		t.Fatalf("step erred: %v", err)
	}

	// Mutate the live conversation after the call.
	conv.Messages[0].Content[0].Text = "MUTATED"
	conv.AddUser(model.Text("appended"))

	calls := p.Calls()
	if len(calls) != 1 {
		t.Fatalf("Calls len = %d, want 1", len(calls))
	}
	snap := calls[0]
	if snap.Len() != 1 {
		t.Errorf("snapshot Len = %d, want 1 (post-call append must not leak)", snap.Len())
	}
	if got := snap.Messages[0].Content[0].Text; got != "original" {
		t.Errorf("snapshot text = %q, want %q (post-call mutation must not leak)", got, "original")
	}
}

func TestCapabilitiesDefaultAndOverride(t *testing.T) {
	t.Parallel()
	def := New().Capabilities()
	if !def.NativeComputerUse || !def.Streaming || !def.Vision {
		t.Errorf("default caps = %+v, want native+streaming+vision", def)
	}

	custom := model.Capabilities{Grounding: true, MaxImages: 3}
	p := New().WithCapabilities(custom)
	if p.Capabilities() != custom {
		t.Errorf("caps = %+v, want %+v", p.Capabilities(), custom)
	}
}

func TestLastOptions(t *testing.T) {
	t.Parallel()
	p := New(model.EndTurn("x", model.Usage{}))
	if _, ok := p.LastOptions(); ok {
		t.Error("LastOptions ok before any Step, want false")
	}
	var conv model.Conversation
	_, _ = p.Step(context.Background(), &conv, model.WithMaxTokens(512), model.WithSeed(7))
	opt, ok := p.LastOptions()
	if !ok {
		t.Fatal("LastOptions ok = false after Step")
	}
	if opt.MaxTokens != 512 || opt.Seed == nil || *opt.Seed != 7 {
		t.Errorf("LastOptions = %+v, want MaxTokens=512 Seed=7", opt)
	}
}
