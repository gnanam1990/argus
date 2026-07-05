//go:build !robotgo

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/gnanam1990/argus/internal/platform"
)

// displayServer reports the host display server (X11 shell driver build).
func displayServer() string { return platform.DisplayServer() }

// preflight validates the host can actually be driven by the default backend.
// On Wayland that's the ydotool-based driver (so a Wayland session is allowed,
// unlike the X11-only platform.Preflight, provided ydotool is installed); on
// X11 it's the shell driver.
func preflight() error {
	if platform.IsWayland() {
		if _, err := exec.LookPath("ydotool"); err != nil {
			return fmt.Errorf("platform: Wayland session but 'ydotool' is not installed — " +
				"install ydotool and run its daemon (ydotoold) so Argus can inject input, " +
				"plus a screenshot tool (grim, gnome-screenshot, or spectacle)")
		}
		return nil
	}
	return platform.Preflight(os.Getenv)
}

// captureCheck verifies the active backend's external tools are installed, so
// doctor catches a missing binary before the first real run does.
func captureCheck() string {
	if platform.IsWayland() {
		return waylandToolCheck()
	}
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

// waylandToolCheck reports which Wayland backend tools are missing.
func waylandToolCheck() string {
	var missing []string
	if _, err := exec.LookPath("ydotool"); err != nil {
		missing = append(missing, "ydotool (input — also run the ydotoold daemon)")
	}
	haveShot := false
	for _, tool := range []string{"grim", "gnome-screenshot", "spectacle"} {
		if _, err := exec.LookPath(tool); err == nil {
			haveShot = true
			break
		}
	}
	if !haveShot {
		missing = append(missing, "a screenshot tool (grim, gnome-screenshot, or spectacle)")
	}
	if len(missing) > 0 {
		return "missing tools: " + strings.Join(missing, ", ")
	}
	return ""
}

// displaysInfo is empty on the X11 build: the shell driver captures the whole
// virtual screen, so there is no per-display selection to report.
func displaysInfo() string { return "" }
