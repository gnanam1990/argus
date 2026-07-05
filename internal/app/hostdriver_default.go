//go:build !robotgo && !darwin

package app

import (
	"github.com/gnanam1990/argus/internal/driver/shell"
	"github.com/gnanam1990/argus/internal/driver/wayland"
	"github.com/gnanam1990/argus/internal/platform"
	"github.com/gnanam1990/argus/pkg/computer"
)

// hostDriver returns the default (CGo-free) host driver for the session's
// display server: the Wayland driver (ydotool + grim/…) on a Wayland session,
// otherwise the X11 shell driver (xdotool/maim/xrandr). The display index and
// smooth flag are accepted for signature parity with the robotgo build but not
// used here — these backends capture the whole virtual screen and don't animate
// motion. Build with -tags robotgo for the native macOS/Windows per-display
// backend.
func hostDriver(_ int, _ bool) computer.Computer {
	if platform.IsWayland() {
		return wayland.New()
	}
	return shell.New()
}
