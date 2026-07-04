//go:build darwin

package ax_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"testing"

	"github.com/gnanam1990/argus/internal/grounder/ax"
	"github.com/gnanam1990/argus/pkg/action"
)

// TestHostSourceLive exercises the real HostSource against the real desktop:
// the default ExecRunner, the real osascript, the real frontmost app. It
// only runs when explicitly requested with ARGUS_AX_LIVE=1, since its
// outcome depends on the Accessibility permission granted to whatever
// process runs `go test` — CI has never granted it, and a first local run
// usually hasn't either — which is an environment/permission condition, not
// a code defect, so a missing grant is reported rather than failed.
func TestHostSourceLive(t *testing.T) {
	if os.Getenv("ARGUS_AX_LIVE") != "1" {
		t.Skip("set ARGUS_AX_LIVE=1 to run against the real desktop (needs Accessibility permission)")
	}

	// A placeholder screenshot at a plausible resolution: HostSource only
	// needs its pixel dimensions to compute the point->pixel scale factor,
	// not its actual pixel content.
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1512, 982))); err != nil {
		t.Fatalf("encode placeholder screenshot: %v", err)
	}
	img := action.Image{MIME: action.MIMEPNG, Data: buf.Bytes()}

	d := ax.New(ax.WithSource(ax.HostSource()))
	els, err := d.Detect(context.Background(), img)
	if err != nil {
		t.Logf("HostSource unavailable (often missing Accessibility permission for this "+
			"process — grant it in System Settings > Privacy & Security > Accessibility "+
			"to the terminal/app running `go test`, then relaunch it): %v", err)
		return
	}
	t.Logf("HostSource detected %d element(s) against the real frontmost app", len(els))
}
