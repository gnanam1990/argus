package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/agent"
	compfake "github.com/gnanam1990/argus/pkg/computer/fake"
	"github.com/gnanam1990/argus/pkg/model"
	providerfake "github.com/gnanam1990/argus/pkg/model/fake"
)

// observe() must fail the run instead of silently keeping a stale/identity
// scale when the driver can't report its screen size (H6). The provider is
// never consulted: the initial observe happens before the first Step call.
func TestRunScreenSizeErrorFailsRun(t *testing.T) {
	t.Parallel()
	boom := errors.New("no display")
	prov := providerfake.New(model.EndTurn("done", model.Usage{}))
	comp := compfake.New().WithScreenSizeError(boom)
	r := agent.NewRunner(prov, comp)

	out, err := r.Run(context.Background(), "task")
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("err = %v, want wrapped %v", err, boom)
	}
	if out == nil || out.Reason != agent.ReasonError {
		t.Errorf("outcome = %+v, want Reason=error", out)
	}
}

// observe() must fail the run when the screenshot bytes can't be decoded
// instead of silently skipping the scale update (H6).
func TestRunCorruptScreenshotFailsRun(t *testing.T) {
	t.Parallel()
	prov := providerfake.New(model.EndTurn("done", model.Usage{}))
	comp := compfake.New().WithScreenshot(
		action.Image{MIME: action.MIMEPNG, Data: []byte("not a png")}, 100, 100)
	r := agent.NewRunner(prov, comp)

	out, err := r.Run(context.Background(), "task")
	if err == nil {
		t.Fatal("expected an error for an undecodable screenshot")
	}
	if out == nil || out.Reason != agent.ReasonError {
		t.Errorf("outcome = %+v, want Reason=error", out)
	}
}

// An explicitly empty screenshot has nothing to size from and must not fail
// the run over it — only a non-empty-but-undecodable screenshot should.
func TestRunEmptyScreenshotDoesNotFailRun(t *testing.T) {
	t.Parallel()
	prov := providerfake.New(model.EndTurn("done", model.Usage{}))
	comp := compfake.New().WithScreenshot(action.Image{MIME: action.MIMEPNG, Data: nil}, 100, 100)
	r := agent.NewRunner(prov, comp)

	out, err := r.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Reason != agent.ReasonCompleted {
		t.Errorf("Reason = %q, want completed", out.Reason)
	}
}
