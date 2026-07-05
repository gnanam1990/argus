package agent_test

import (
	"bytes"
	"context"
	"image"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/agent"
	compfake "github.com/gnanam1990/argus/pkg/computer/fake"
	"github.com/gnanam1990/argus/pkg/model"
	providerfake "github.com/gnanam1990/argus/pkg/model/fake"
	"github.com/gnanam1990/argus/pkg/trajectory"
)

func imgDims(t *testing.T, img action.Image) (int, int) {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(img.Data))
	if err != nil {
		t.Fatalf("decode image: %v", err)
	}
	return cfg.Width, cfg.Height
}

// Downscaling the frame sent to the model must not move where clicks land:
// the executor's scale is derived from the SENT frame's dimensions, so a model
// click in downscaled space maps back to full screen space.
func TestRunScalesFromProcessedFrame(t *testing.T) {
	t.Parallel()
	// Capture and screen are both 100x100 (would be 1x). A processor shrinks
	// the sent frame to 50x50, so the scale must become 2x and a click at
	// (10,10) in the model's view lands at (20,20) on screen.
	small := pngOf(t, 50, 50)
	prov := providerfake.New(
		model.ActionTurn(model.Usage{}, clickAt(10, 10)),
		model.EndTurn("done", model.Usage{}),
	)
	comp := compfake.New().WithScreenshot(pngOf(t, 100, 100), 100, 100)
	r := agent.NewRunner(prov, comp, agent.WithScreenshotProcessor(func(action.Image) (action.Image, error) {
		return small, nil
	}))

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
		t.Errorf("click at (%d,%d), want (20,20): scale must derive from the 50x50 sent frame", click.X, click.Y)
	}
}

// The processor's output is what reaches the model; the full-resolution capture
// is what the trajectory records.
func TestRunProcessorFeedsModelButRecordsOriginal(t *testing.T) {
	t.Parallel()
	small := pngOf(t, 40, 40)
	prov := providerfake.New(model.EndTurn("done", model.Usage{}))
	comp := compfake.New().WithScreenshot(pngOf(t, 80, 80), 80, 80)
	rec := trajectory.NewMemory(trajectory.NewManifest("task"))
	r := agent.NewRunner(prov, comp,
		agent.WithTrajectory(rec),
		agent.WithScreenshotProcessor(func(action.Image) (action.Image, error) { return small, nil }),
	)
	if _, err := r.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Conversation image (what the model saw) is the 40x40 processed frame.
	var sawImage *action.Image
	for _, m := range r.History().Messages {
		for _, c := range m.Content {
			if c.Kind == model.KindImage {
				img := c.Image
				sawImage = &img
			}
		}
	}
	if sawImage == nil {
		t.Fatal("no image in conversation")
	}
	if w, h := imgDims(t, *sawImage); w != 40 || h != 40 {
		t.Errorf("model saw %dx%d, want the 40x40 processed frame", w, h)
	}
	// Trajectory recorded the full 80x80 capture.
	steps := rec.Steps()
	if len(steps) == 0 {
		t.Fatal("nothing recorded")
	}
	if w, h := imgDims(t, steps[0].Screenshot); w != 80 || h != 80 {
		t.Errorf("trajectory recorded %dx%d, want the full 80x80 capture", w, h)
	}
}
