//go:build robotgo && darwin

package robotgo

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/gnanam1990/argus/pkg/action"
)

// captureDisplay screenshots the driven display with the system screencapture
// tool, targeting the display's global point rectangle (-R x,y,w,h).
//
// This replaces robotgo.CaptureImg on macOS because robotgo's capture is built
// on CGDisplayCreateImage, which is obsoleted as of macOS 15 and returns the
// MAIN display's content for every display index — so on a multi-monitor setup
// a capture of display 1 or 2 would silently show display 0. Targeting the
// display's global rectangle captures exactly the driven monitor. screencapture
// writes native pixels (2x on a Retina display), which is the screenshot-pixel
// space the grounder's scale math expects.
func (d *Driver) captureDisplay(ctx context.Context) (action.Image, error) {
	x, y, w, h := d.bounds(d.display)

	f, err := os.CreateTemp("", "argus-shot-*.png")
	if err != nil {
		return action.Image{}, fmt.Errorf("robotgo screenshot: temp file: %w", err)
	}
	name := f.Name()
	_ = f.Close()
	defer os.Remove(name)

	// -x: silent (no capture sound), -o: omit window shadow, -t png, -R region.
	cmd := exec.CommandContext(ctx, "screencapture", "-x", "-o", "-t", "png",
		fmt.Sprintf("-R%d,%d,%d,%d", x, y, w, h), name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return action.Image{}, captureError(fmt.Errorf("screencapture: %w: %s", err, out))
	}

	data, err := os.ReadFile(name)
	if err != nil {
		return action.Image{}, fmt.Errorf("robotgo screenshot: read: %w", err)
	}
	if len(data) == 0 {
		// screencapture exits 0 but writes nothing when Screen Recording is
		// denied; surface that as the actionable permission error.
		return action.Image{}, captureError(fmt.Errorf("screencapture produced no image"))
	}
	return action.Image{MIME: action.MIMEPNG, Data: data}, nil
}
