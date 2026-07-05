//go:build !robotgo && (darwin || windows)

package app

import (
	"fmt"
	"os"
	"runtime"

	"github.com/gnanam1990/argus/internal/driver/shell"
	"github.com/gnanam1990/argus/pkg/computer"
)

// hostDriver on a macOS or Windows build WITHOUT -tags robotgo returns the X11
// shell driver, which has no working backend on either OS (it shells out to
// xdotool/maim/xrandr). That combination compiles fine but every capture/click
// fails only at first use, so warn loudly at construction to turn a silent
// footgun into an obvious one. Rebuild with `make build-robotgo` (or
// `-tags robotgo`, CGO enabled) for the native per-display backend.
func hostDriver(_ int, _ bool) computer.Computer {
	fmt.Fprintf(os.Stderr, "argus: WARNING — built without -tags robotgo on %s; "+
		"desktop control and screen capture are non-functional (the X11 shell driver "+
		"has no backend here). Rebuild with `make build-robotgo`.\n", runtime.GOOS)
	return shell.New()
}
