// Package platform holds host-environment detection that the drivers and the
// doctor command depend on: which display server is running, the multi-monitor
// layout, and a preflight that fails fast with actionable guidance rather than
// letting a driver silently no-op.
package platform

import (
	"fmt"
	"os"
	"regexp"
	"strconv"

	"github.com/gnanam1990/argus/pkg/action"
)

// Display is one monitor's placement in the virtual screen, in pixels.
type Display struct {
	Name   string
	Bounds action.Rect
}

// DisplayServer reports the active display server ("x11", "wayland", or
// "unknown"), from XDG_SESSION_TYPE / WAYLAND_DISPLAY.
func DisplayServer() string { return displayServerFrom(os.Getenv) }

func displayServerFrom(getenv func(string) string) string {
	switch getenv("XDG_SESSION_TYPE") {
	case "wayland":
		return "wayland"
	case "x11":
		return "x11"
	}
	if getenv("WAYLAND_DISPLAY") != "" {
		return "wayland"
	}
	if getenv("DISPLAY") != "" {
		return "x11"
	}
	return "unknown"
}

// IsWayland reports whether the host is running Wayland.
func IsWayland() bool { return DisplayServer() == "wayland" }

// Preflight validates that the shell (X11) driver can actually drive the host,
// returning an actionable error otherwise. The environment is read via getenv
// for testability; pass os.Getenv in production.
func Preflight(getenv func(string) string) error {
	switch displayServerFrom(getenv) {
	case "wayland":
		return fmt.Errorf("platform: host control is X11-only; this is a Wayland session — " +
			"log into an X11 session, or drive an X11 desktop in a container (sandbox.kind \"docker\")")
	case "unknown":
		return fmt.Errorf("platform: no display detected (DISPLAY/WAYLAND_DISPLAY unset); " +
			"host control needs a running X server")
	default:
		return nil
	}
}

// monitorGeom matches an xrandr --listmonitors geometry token such as
// "1920/510x1080/290+0+0": width/…xheight/…+originX+originY.
var monitorGeom = regexp.MustCompile(`(\d+)/\d+x(\d+)/\d+\+(-?\d+)\+(-?\d+)`)

// ParseMonitors extracts the display layout from `xrandr --listmonitors`
// output. Origins matter: a click on a secondary monitor is offset by that
// monitor's origin, so scaling math must account for it.
func ParseMonitors(s string) []Display {
	var out []Display
	re := regexp.MustCompile(`^\s*\d+:\s+\S+\s+(\S+)\s+(\S+)\s*$`)
	for _, line := range splitLines(s) {
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		g := monitorGeom.FindStringSubmatch(m[1])
		if g == nil {
			continue
		}
		w, _ := strconv.Atoi(g[1])
		h, _ := strconv.Atoi(g[2])
		x, _ := strconv.Atoi(g[3])
		y, _ := strconv.Atoi(g[4])
		out = append(out, Display{
			Name:   m[2],
			Bounds: action.Rect{Min: action.Point{X: x, Y: y}, Max: action.Point{X: x + w, Y: y + h}},
		})
	}
	return out
}

// DisplayAt returns the display containing p, or nil.
func DisplayAt(displays []Display, p action.Point) *Display {
	for i := range displays {
		if displays[i].Bounds.Contains(p) {
			return &displays[i]
		}
	}
	return nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
