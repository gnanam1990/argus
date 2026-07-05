//go:build !robotgo

package main

import (
	"os"
	"os/exec"
	"strings"

	"github.com/gnanam1990/argus/internal/platform"
)

// displayServer reports the host display server (X11 shell driver build).
func displayServer() string { return platform.DisplayServer() }

// preflight validates the host can be driven by the X11 shell driver.
func preflight() error { return platform.Preflight(os.Getenv) }

// captureCheck verifies the shell driver's external tools are installed, so
// doctor catches a missing binary before the first real run does.
func captureCheck() string {
	var missing []string
	for _, tool := range []string{"xdotool", "xrandr"} {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	haveShot := false
	for _, tool := range []string{"maim", "scrot", "import"} {
		if _, err := exec.LookPath(tool); err == nil {
			haveShot = true
			break
		}
	}
	if !haveShot {
		missing = append(missing, "a screenshot tool (maim, scrot, or import)")
	}
	if len(missing) > 0 {
		return "missing tools: " + strings.Join(missing, ", ")
	}
	return ""
}

// displaysInfo is empty on the X11 build: the shell driver captures the whole
// virtual screen, so there is no per-display selection to report.
func displaysInfo() string { return "" }
