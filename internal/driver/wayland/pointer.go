package wayland

import (
	"context"
	"encoding/json"
	"os"
)

// pointerKind is how the driver positions the cursor. ydotool's "absolute"
// mousemove is not truly absolute on Wayland — it warps to a corner then moves
// relative, and the compositor's pointer acceleration then scales that motion,
// so the cursor lands off-target (big buttons still hit, small controls miss).
// Hyprland and Sway can set the cursor to an exact logical position, which is
// immune to acceleration; we prefer those when detectable.
type pointerKind int

const (
	pointerYdotool  pointerKind = iota // ydotool mousemove --absolute (fallback)
	pointerHyprland                    // hyprctl dispatch movecursor (exact)
	pointerSway                        // swaymsg seat cursor set (exact)
)

// detectPointer picks the positioning backend from the environment: Hyprland
// and Sway expose exact cursor placement, everything else falls back to
// ydotool. Overridable via ARGUS_WL_POINTER=ydotool|hyprland|sway.
func detectPointer(getenv func(string) string) pointerKind {
	switch getenv("ARGUS_WL_POINTER") {
	case "ydotool":
		return pointerYdotool
	case "hyprland":
		return pointerHyprland
	case "sway":
		return pointerSway
	}
	if getenv("HYPRLAND_INSTANCE_SIGNATURE") != "" {
		return pointerHyprland
	}
	if getenv("SWAYSOCK") != "" {
		return pointerSway
	}
	return pointerYdotool
}

// scaleFor returns how many screenshot pixels map to one logical point. grim
// captures physical pixels while hyprctl/swaymsg position in logical points, so
// on a scaled output (e.g. 2x HiDPI, or fractional scaling) a screenshot-pixel
// click coordinate must be divided by the scale before being handed to the
// compositor. Returns 1.0 when it can't be determined (unscaled is the common
// case, and 1.0 is the safe identity).
func (d *Driver) scaleFor(ctx context.Context) float64 {
	switch d.pointer {
	case pointerHyprland:
		if s, ok := hyprScale(ctx, d.run); ok {
			return s
		}
	case pointerSway:
		if s, ok := swayScale(ctx, d.run); ok {
			return s
		}
	}
	return 1.0
}

// hyprScale reads the focused monitor's scale from `hyprctl monitors -j`.
func hyprScale(ctx context.Context, run Runner) (float64, bool) {
	out, err := run.Run(ctx, "hyprctl", "-j", "monitors")
	if err != nil {
		return 0, false
	}
	var mons []struct {
		Focused bool    `json:"focused"`
		Scale   float64 `json:"scale"`
	}
	if json.Unmarshal(out, &mons) != nil {
		return 0, false
	}
	for _, m := range mons {
		if m.Focused && m.Scale > 0 {
			return m.Scale, true
		}
	}
	if len(mons) > 0 && mons[0].Scale > 0 {
		return mons[0].Scale, true
	}
	return 0, false
}

// swayScale reads the focused output's scale from `swaymsg -t get_outputs -j`.
func swayScale(ctx context.Context, run Runner) (float64, bool) {
	out, err := run.Run(ctx, "swaymsg", "-t", "get_outputs", "-r")
	if err != nil {
		return 0, false
	}
	var outs []struct {
		Focused bool    `json:"focused"`
		Scale   float64 `json:"scale"`
	}
	if json.Unmarshal(out, &outs) != nil {
		return 0, false
	}
	for _, o := range outs {
		if o.Focused && o.Scale > 0 {
			return o.Scale, true
		}
	}
	if len(outs) > 0 && outs[0].Scale > 0 {
		return outs[0].Scale, true
	}
	return 0, false
}

// moveCursor positions the pointer at screenshot-pixel (xPx, yPx) using the
// detected backend, converting to logical points via the output scale. The
// exact-positioning compositors are used when available; otherwise it falls
// through to ydotool's absolute move.
func (d *Driver) moveCursor(ctx context.Context, xPx, yPx int) error {
	if d.pointer == pointerYdotool {
		return d.runAdaptive(ctx, &d.moveStyle, moveStyles, xPx, yPx, "mousemove")
	}

	scale := d.scaleFor(ctx)
	if scale <= 0 {
		scale = 1
	}
	lx := int(float64(xPx)/scale + 0.5)
	ly := int(float64(yPx)/scale + 0.5)

	var err error
	switch d.pointer {
	case pointerHyprland:
		_, err = d.run.Run(ctx, "hyprctl", "dispatch", "movecursor", itoa(lx), itoa(ly))
	case pointerSway:
		_, err = d.run.Run(ctx, "swaymsg", "seat", "-", "cursor", "set", itoa(lx), itoa(ly))
	}
	if err != nil {
		// Compositor positioning failed at runtime — fall back to ydotool so a
		// transient hyprctl/swaymsg hiccup doesn't make the action unusable.
		return d.runAdaptive(ctx, &d.moveStyle, moveStyles, xPx, yPx, "mousemove")
	}
	return nil
}

// pointerName is the human label for doctor/diagnostics.
func (k pointerKind) String() string {
	switch k {
	case pointerHyprland:
		return "hyprland (exact)"
	case pointerSway:
		return "sway (exact)"
	default:
		return "ydotool (relative — acceleration may skew clicks)"
	}
}

// osGetenv is indirected so tests can inject an environment.
var osGetenv = os.Getenv

// PointerBackend names the cursor-positioning backend that would be used in the
// current environment, for diagnostics (argus doctor).
func PointerBackend() string { return detectPointer(os.Getenv).String() }
