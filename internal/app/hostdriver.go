package app

import "github.com/gnanam1990/argus/pkg/computer"

// HostDriver returns the host computer driver appropriate for this build and
// platform: the native robotgo backend under -tags robotgo, otherwise the
// CGo-free Wayland or X11 shell driver by display server (with a loud warning
// on OSes the CGo-free build cannot actually drive). display selects the
// monitor (robotgo builds only); smooth animates pointer motion. Exported so
// commands that don't assemble the full app (argus-mcp's raw driver mode) pick
// the same platform-correct driver instead of hardcoding one backend.
func HostDriver(display int, smooth bool) computer.Computer {
	return hostDriver(display, smooth)
}
