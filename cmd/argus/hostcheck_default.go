//go:build !robotgo

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/gnanam1990/argus/internal/platform"
)

// displayServer reports the host display server (X11 shell driver build).
func displayServer() string { return platform.DisplayServer() }

// preflight validates the host can actually be driven by the default backend.
// On macOS/Windows the CGo-free build has no working backend at all, so say
// that (and the fix) instead of a misleading X11 message. On Wayland the
// ydotool-based driver applies (so a Wayland session is allowed, unlike the
// X11-only platform.Preflight, provided ydotool is installed); on X11 it's the
// shell driver.
func preflight() error {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return fmt.Errorf("platform: this build has no native %s backend (CGo-free X11 build) — "+
			"rebuild with `make build-robotgo` (CGO_ENABLED=1 go build -tags robotgo)", runtime.GOOS)
	}
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
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return "no native backend in this build — rebuild with `make build-robotgo`"
	}
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

// waylandToolCheck reports which Wayland backend tools are missing, including
// whether the ydotoold daemon looks reachable (its absent or root-owned socket
// is the most common Wayland setup failure and otherwise only surfaces as an
// opaque non-zero ydotool exit at first action).
func waylandToolCheck() string {
	var missing []string
	if _, err := exec.LookPath("ydotool"); err != nil {
		missing = append(missing, "ydotool (input — also run the ydotoold daemon)")
	} else if s := ydotoolSocketIssue(); s != "" {
		missing = append(missing, s)
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

// ydotoolSocketIssue checks the ydotoold daemon socket at the paths the ydotool
// client uses ($YDOTOOL_SOCKET, then the runtime dir, then /tmp) and reports
// what's wrong: no socket (daemon not running) or a socket this user can't
// write (started with a bare `sudo ydotoold`, so it's root-owned).
func ydotoolSocketIssue() string {
	var candidates []string
	if p := os.Getenv("YDOTOOL_SOCKET"); p != "" {
		candidates = append(candidates, p)
	}
	if rd := os.Getenv("XDG_RUNTIME_DIR"); rd != "" {
		candidates = append(candidates, rd+"/.ydotool_socket")
	}
	candidates = append(candidates, "/tmp/.ydotool_socket")

	for _, p := range candidates {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		// Socket exists — verify this user can actually use it.
		if f, err := os.OpenFile(p, os.O_RDWR, 0); err == nil {
			_ = f.Close()
			return ""
		}
		return "ydotoold socket at " + p + " is not writable by this user — restart the daemon with: " +
			`sudo ydotoold --socket-own="$(id -u):$(id -g)"`
	}
	return "ydotoold daemon (no socket found — start it with: " +
		`sudo ydotoold --socket-own="$(id -u):$(id -g)")`
}

// displaysInfo is empty on the X11 build: the shell driver captures the whole
// virtual screen, so there is no per-display selection to report.
func displaysInfo() string { return "" }
