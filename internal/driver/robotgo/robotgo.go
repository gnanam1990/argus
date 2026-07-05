//go:build robotgo

// Package robotgo implements computer.Computer with the go-vgo/robotgo native
// backend. It is the functional driver on macOS and Windows (where the shell/
// X11 backend does not apply) and is guarded by the `robotgo` build tag so the
// default build stays CGo-free: it compiles only under `go build -tags robotgo`
// on the target OS, and is exercised in dedicated per-OS CI jobs.
package robotgo

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"runtime"
	"strings"

	"github.com/go-vgo/robotgo"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/computer"
)

// Driver drives the host via robotgo. It targets a single display (index 0 =
// primary): it captures and sizes that display, and offsets every input by the
// display's global origin so a click in the captured frame lands on the right
// screen in a multi-monitor layout.
type Driver struct {
	display int
	ox, oy  int // the target display's global (logical) origin
}

// Option configures a Driver.
type Option func(*Driver)

// WithDisplay selects which display to drive (0 = primary). An out-of-range
// index falls back to the primary.
func WithDisplay(n int) Option { return func(d *Driver) { d.display = n } }

// New builds a robotgo driver, resolving the target display's global origin so
// input coordinates (which robotgo interprets in the whole-desktop space) are
// offset from the captured display's local space.
func New(opts ...Option) *Driver {
	d := &Driver{}
	for _, o := range opts {
		o(d)
	}
	if d.display < 0 || d.display >= robotgo.DisplaysNum() {
		d.display = 0
	}
	d.ox, d.oy, _, _ = robotgo.GetDisplayBounds(d.display)
	return d
}

var _ computer.Computer = (*Driver)(nil)

// DisplayInfo is one monitor's index and global bounds (logical points).
type DisplayInfo struct {
	Index      int
	X, Y, W, H int
	Primary    bool
}

// Displays lists every attached display and its global bounds, for diagnostics.
func Displays() []DisplayInfo {
	n := robotgo.DisplaysNum()
	out := make([]DisplayInfo, 0, n)
	for i := 0; i < n; i++ {
		x, y, w, h := robotgo.GetDisplayBounds(i)
		out = append(out, DisplayInfo{Index: i, X: x, Y: y, W: w, H: h, Primary: x == 0 && y == 0})
	}
	return out
}

// g maps a coordinate in the captured display's local space to the global
// whole-desktop space robotgo's input functions expect.
func (d *Driver) g(x, y int) (int, int) { return x + d.ox, y + d.oy }

// DisplayBounds reports the driven display's global bounds in logical points,
// so callers working in global coordinates (the accessibility path) can align
// with the display this driver captures. Implements computer.DisplayBounder.
func (d *Driver) DisplayBounds() (x, y, w, h int) {
	_, _, w, h = robotgo.GetDisplayBounds(d.display)
	return d.ox, d.oy, w, h
}

// Screenshot captures the target display and encodes it as PNG.
func (d *Driver) Screenshot(_ context.Context) (action.Image, error) {
	img, err := robotgo.CaptureImg(d.display)
	if err != nil {
		return action.Image{}, captureError(err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return action.Image{}, fmt.Errorf("robotgo encode: %w", err)
	}
	return action.Image{MIME: action.MIMEPNG, Data: buf.Bytes()}, nil
}

// ScreenSize returns the target display's logical size. The executor derives
// the model→screen scale from this versus the captured pixel dimensions, so a
// HiDPI display is handled the same way as the primary.
func (d *Driver) ScreenSize(_ context.Context) (int, int, error) {
	_, _, w, h := robotgo.GetDisplayBounds(d.display)
	return w, h, nil
}

// MoveMouse moves the pointer.
func (d *Driver) MoveMouse(_ context.Context, x, y int) error {
	robotgo.Move(d.g(x, y))
	return nil
}

// Click moves to (x,y) and clicks `clicks` times.
func (d *Driver) Click(_ context.Context, x, y int, b action.Button, clicks int) error {
	robotgo.Move(d.g(x, y))
	if clicks <= 0 {
		clicks = 1
	}
	for i := 0; i < clicks; i++ {
		if err := robotgo.Click(buttonName(b), false); err != nil {
			return fmt.Errorf("robotgo click: %w", err)
		}
	}
	return nil
}

// MouseDown presses a button at (x,y).
func (d *Driver) MouseDown(_ context.Context, x, y int, b action.Button) error {
	robotgo.Move(d.g(x, y))
	if err := robotgo.Toggle(buttonName(b), "down"); err != nil {
		return fmt.Errorf("robotgo mousedown: %w", err)
	}
	return nil
}

// MouseUp releases a button at (x,y).
func (d *Driver) MouseUp(_ context.Context, x, y int, b action.Button) error {
	robotgo.Move(d.g(x, y))
	if err := robotgo.Toggle(buttonName(b), "up"); err != nil {
		return fmt.Errorf("robotgo mouseup: %w", err)
	}
	return nil
}

// Drag presses at the first point, moves through the rest, and releases.
func (d *Driver) Drag(_ context.Context, path []action.Point, b action.Button) error {
	if len(path) < 2 {
		return fmt.Errorf("robotgo: drag needs >= 2 points")
	}
	name := buttonName(b)
	robotgo.Move(d.g(path[0].X, path[0].Y))
	if err := robotgo.Toggle(name, "down"); err != nil {
		return fmt.Errorf("robotgo drag down: %w", err)
	}
	for _, p := range path[1:] {
		robotgo.Move(d.g(p.X, p.Y))
	}
	if err := robotgo.Toggle(name, "up"); err != nil {
		return fmt.Errorf("robotgo drag up: %w", err)
	}
	return nil
}

// Scroll moves to (x,y) and scrolls by (dx,dy).
func (d *Driver) Scroll(_ context.Context, x, y, dx, dy int) error {
	robotgo.Move(d.g(x, y))
	rx, ry := scrollArgs(dx, dy)
	robotgo.Scroll(rx, ry)
	return nil
}

// scrollArgs converts our canonical scroll delta — positive DY scrolls down,
// positive DX scrolls right (pkg/action.Action's DX/DY doc; shell.go's Scroll
// fires X11 button 5 for dy>0 and button 7 for dx>0 to match) — into the
// arguments robotgo.Scroll(x, y) expects, which run the opposite convention
// on both axes. Verified directly in the vendored source
// (github.com/go-vgo/robotgo@v1.0.2):
//
//   - robotgo.go:902-922 ScrollDir: direction "down" calls Scroll(0, -x) and
//     "up" calls Scroll(0, x) — so positive y is UP, negative y is DOWN. The
//     same function's "left" calls Scroll(x, 0) and "right" calls
//     Scroll(-x, 0) — so positive x is LEFT, negative x is RIGHT.
//   - mouse/mouse_c.h:240-293 scrollMouseXY(x, y), the single C primitive
//     every backend shares behind cgo build tags, confirms both axes per OS:
//   - X11 (USE_X11): ydir defaults to button 4 (up), only switching to
//     button 5 (down) "if (y < 0)"; xdir defaults to button 6 (left), only
//     switching to button 7 (right) "if (x < 0)".
//   - macOS (IS_MACOSX): CGEventCreateScrollWheelEvent(source,
//     kCGScrollEventUnitPixel, 2, y, x) — wheel1 (vertical) is bound to y,
//     wheel2 (horizontal) to x, the same sign convention as X11 above.
//   - Windows (IS_WINDOWS): MOUSEEVENTF_WHEEL mouseData = WHEEL_DELTA * y;
//     per the Win32 API, a positive value is the wheel rotated forward, away
//     from the user (up) — again positive y is up.
//
// So both axes are negated: canonical +DY (down) needs a negative robotgo y,
// and canonical +DX (right) needs a negative robotgo x.
func scrollArgs(dx, dy int) (x, y int) {
	return -dx, -dy
}

// TypeText types literal text.
func (d *Driver) TypeText(_ context.Context, text string) error {
	robotgo.TypeStr(text)
	return nil
}

// KeyPress taps a key chord (last element is the key, the rest are modifiers).
func (d *Driver) KeyPress(_ context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	translated, err := translateKeys(keys)
	if err != nil {
		return err
	}
	key := translated[len(translated)-1]
	mods := make([]interface{}, 0, len(translated)-1)
	for _, m := range translated[:len(translated)-1] {
		mods = append(mods, m)
	}
	if err := robotgo.KeyTap(key, mods...); err != nil {
		return fmt.Errorf("robotgo keytap: %w", err)
	}
	return nil
}

// KeyDown presses and holds a key.
func (d *Driver) KeyDown(_ context.Context, key string) error {
	k, err := translateKey(key)
	if err != nil {
		return err
	}
	if err := robotgo.KeyToggle(k, "down"); err != nil {
		return fmt.Errorf("robotgo keydown: %w", err)
	}
	return nil
}

// KeyUp releases a key.
func (d *Driver) KeyUp(_ context.Context, key string) error {
	k, err := translateKey(key)
	if err != nil {
		return err
	}
	if err := robotgo.KeyToggle(k, "up"); err != nil {
		return fmt.Errorf("robotgo keyup: %w", err)
	}
	return nil
}

// CursorPosition returns the pointer location in the target display's local
// space (global position minus the display origin), matching the coordinate
// space of Screenshot/ScreenSize.
func (d *Driver) CursorPosition(_ context.Context) (int, int, error) {
	x, y := robotgo.GetMousePos()
	return x - d.ox, y - d.oy, nil
}

// Close is a no-op.
func (d *Driver) Close() error { return nil }

// captureError wraps a failed capture with an actionable hint. On macOS a
// failure is almost always the Screen Recording permission (it blocks every
// capture API, including Apple's screencapture), not a bug.
func captureError(err error) error {
	if runtime.GOOS == "darwin" {
		return fmt.Errorf("robotgo capture failed (%w): grant Screen Recording to the "+
			"terminal/app that launched argus (System Settings > Privacy & Security > "+
			"Screen Recording), then fully quit and reopen it", err)
	}
	return fmt.Errorf("robotgo capture: %w", err)
}

func buttonName(b action.Button) string {
	switch b {
	case action.Right:
		return "right"
	case action.Middle:
		return "center"
	default:
		return "left"
	}
}

// keyAliases maps canonical cross-driver key names (see shell.go's keymap,
// the reference for the shared vocabulary) to the name robotgo v1.0.2
// actually understands. Everything else in the canonical vocabulary (ctrl,
// shift, enter, esc, tab, arrows, space, backspace, delete, ...) is already a
// robotgo key name and passes straight through translateKey below.
//
//   - win, meta -> cmd: robotgo's own "cmd" already resolves to the correct
//     per-OS "OS key" internally (K_META = kVK_Command on macOS, VK_LWIN on
//     Windows, XK_Super_L on X11 — github.com/go-vgo/robotgo@v1.0.2
//     key/keycode.h:55,170,329), so one unconditional mapping is correct on
//     every platform without a runtime.GOOS branch.
//   - option, opt -> alt: robotgo has no "option"/"opt" entry; "alt" is its
//     name for the same physical key (key.go:254 `"alt": C.K_ALT`).
//   - return -> enter: robotgo's keyNames has "enter" (key.go:212) but no
//     "return" entry at all, even though shell.go treats them as synonyms —
//     left untranslated, "return" would hit the exact silent fallthrough
//     this file guards against below.
var keyAliases = map[string]string{
	"win":    "cmd",
	"meta":   "cmd",
	"option": "alt",
	"opt":    "alt",
	"return": "enter",
}

// robotgoKeyNames mirrors the string keys of robotgo v1.0.2's internal
// keyNames map (github.com/go-vgo/robotgo@v1.0.2 key.go:209-326), the only
// multi-character names its checkKeyCodes (key.go:346-370) recognizes.
//
// checkKeyCodes for any k that is neither a single character (resolved
// separately via keyCodeForChar) nor a keyNames member falls through
// returning keycode 0 with a NIL error. That 0 is not a safe no-op: toggleKeyCode
// (key/keypress_c.h:164-194, esp. line 184) passes the raw MMKeyCode straight
// to CGEventCreateKeyboardEvent with no validation, and macOS defines virtual
// keycode 0 as kVK_ANSI_A — the "a" key (a stable Carbon/HIToolbox platform
// constant, not something this repo defines). So an unrecognized name like a
// typo or an alias robotgo doesn't know would silently tap "a" instead of
// failing. This set is what lets translateKey reject those instead.
var robotgoKeyNames = map[string]bool{
	"backspace": true, "delete": true, "enter": true, "tab": true,
	"esc": true, "escape": true,
	"up": true, "down": true, "left": true, "right": true,
	"home": true, "end": true, "pageup": true, "pagedown": true,

	"f1": true, "f2": true, "f3": true, "f4": true, "f5": true, "f6": true,
	"f7": true, "f8": true, "f9": true, "f10": true, "f11": true, "f12": true,
	"f13": true, "f14": true, "f15": true, "f16": true, "f17": true,
	"f18": true, "f19": true, "f20": true, "f21": true, "f22": true,
	"f23": true, "f24": true,

	"cmd": true, "lcmd": true, "rcmd": true, "command": true,
	"alt": true, "lalt": true, "ralt": true,
	"ctrl": true, "lctrl": true, "rctrl": true, "control": true,
	"shift": true, "lshift": true, "rshift": true, "right_shift": true,
	"capslock": true, "space": true, "print": true, "printscreen": true,
	"insert": true, "menu": true,

	"audio_mute": true, "audio_vol_down": true, "audio_vol_up": true,
	"audio_play": true, "audio_stop": true, "audio_pause": true,
	"audio_prev": true, "audio_next": true, "audio_rewind": true,
	"audio_forward": true, "audio_repeat": true, "audio_random": true,

	"num0": true, "num1": true, "num2": true, "num3": true, "num4": true,
	"num5": true, "num6": true, "num7": true, "num8": true, "num9": true,
	"num_lock": true,
	"numpad_0": true, "numpad_1": true, "numpad_2": true, "numpad_3": true,
	"numpad_4": true, "numpad_5": true, "numpad_6": true, "numpad_7": true,
	"numpad_8": true, "numpad_9": true, "numpad_lock": true,
	"num.": true, "num+": true, "num-": true, "num*": true, "num/": true,
	"num_clear": true, "num_enter": true, "num_equal": true,

	"lights_mon_up": true, "lights_mon_down": true, "lights_kbd_toggle": true,
	"lights_kbd_up": true, "lights_kbd_down": true,
}

// translateKey maps a canonical key name to the name robotgo v1.0.2 expects
// and rejects anything robotgo would otherwise silently misinterpret (see
// keyAliases and robotgoKeyNames above for the evidence). Single characters
// pass through case-sensitively — robotgo resolves them via keyCodeForChar,
// not keyNames, and case selects the shifted glyph — while recognized
// multi-character names pass through lowercased, since robotgo's own
// keyNames lookup is an exact, case-sensitive map match.
func translateKey(name string) (string, error) {
	lower := strings.ToLower(name)
	if alias, ok := keyAliases[lower]; ok {
		return alias, nil
	}
	if len(name) == 1 {
		return name, nil
	}
	if robotgoKeyNames[lower] {
		return lower, nil
	}
	return "", fmt.Errorf("robotgo: unknown key %q", name)
}

// translateKeys applies translateKey across a full chord (shell.go's
// mapKeys is the equivalent for xdotool), failing on the first unrecognized
// element — a modifier is just as capable of silently mistyping or silently
// dropping as the primary key.
func translateKeys(keys []string) ([]string, error) {
	out := make([]string, len(keys))
	for i, k := range keys {
		t, err := translateKey(k)
		if err != nil {
			return nil, err
		}
		out[i] = t
	}
	return out, nil
}
