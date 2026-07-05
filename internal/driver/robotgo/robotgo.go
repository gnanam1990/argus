//go:build robotgo

// Package robotgo implements computer.Computer with the go-vgo/robotgo native
// backend. It is the functional driver on macOS and Windows (where the shell/
// X11 backend does not apply) and is guarded by the `robotgo` build tag so the
// default build stays CGo-free: it compiles only under `go build -tags robotgo`
// on the target OS, and is exercised in dedicated per-OS CI jobs.
package robotgo

import (
	"context"
	"fmt"
	"math"
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
	// smooth animates the pointer along an eased path to each target instead of
	// warping instantly, so a watching operator can follow the motion.
	smooth bool
	// bounds resolves a display's global logical bounds. It's a field (not a
	// direct robotgo call) so it's queried live on every use — handling a
	// mid-run display rearrange — and can be faked in tests. Defaults to
	// robotgo.GetDisplayBounds.
	bounds func(display int) (x, y, w, h int)
}

// Option configures a Driver.
type Option func(*Driver)

// WithDisplay selects which display to drive (0 = primary). An out-of-range
// index falls back to the primary.
func WithDisplay(n int) Option { return func(d *Driver) { d.display = n } }

// WithSmooth enables (or disables) animated pointer motion. When on, the cursor
// glides to each target over a short eased path instead of warping instantly.
func WithSmooth(on bool) Option { return func(d *Driver) { d.smooth = on } }

// New builds a robotgo driver for the given display. The display's global
// origin is resolved live on each input/capture call (see origin) rather than
// cached, so rearranging or unplugging monitors mid-run doesn't leave every
// click offset against a stale origin.
func New(opts ...Option) *Driver {
	d := &Driver{}
	for _, o := range opts {
		o(d)
	}
	if d.display < 0 || d.display >= robotgo.DisplaysNum() {
		d.display = 0
	}
	if d.bounds == nil {
		d.bounds = robotgo.GetDisplayBounds
	}
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

// origin resolves the driven display's global (logical) top-left, live on each
// call so a mid-run display rearrange/unplug can't leave a stale offset.
func (d *Driver) origin() (int, int) {
	x, y, _, _ := d.bounds(d.display)
	return x, y
}

// g maps a coordinate in the captured display's local space to the global
// whole-desktop space robotgo's input functions expect.
func (d *Driver) g(x, y int) (int, int) {
	ox, oy := d.origin()
	return x + ox, y + oy
}

// moveSettleMS is how long to wait after warping the cursor before posting a
// button or scroll event. robotgo.Move warps the pointer asynchronously; a
// click issued in the same breath posts before the warp is registered and
// carries the pre-move location, so macOS drops it (the pointer visibly lands
// on the target yet nothing is pressed). A short settle makes the following
// event land at the moved position. 40ms sufficed in testing; 80ms is a safe
// margin under load and is negligible for interactive computer-use pacing.
const moveSettleMS = 80

// dragStepMS spaces intermediate drag waypoints so the OS/app sees a stream of
// motion events over time rather than an instantaneous jump.
const dragStepMS = 16

const (
	// glidePxStep is the target travel per interpolation step; the step count is
	// distance/glidePxStep so long moves aren't jerky and short ones stay quick.
	glidePxStep = 22
	// glideStepMS is the pause between interpolation steps (drives the visible
	// speed); glideMaxStep caps steps so a cross-screen move can't drag on.
	glideStepMS  = 8
	glideMaxStep = 60
)

// easeInOut is smoothstep: eases the motion in and out so the glide accelerates
// and decelerates instead of moving at a constant, robotic rate.
func easeInOut(t float64) float64 { return t * t * (3 - 2*t) }

// glidePath returns the intermediate points from (cx,cy) to (gx,gy) for an
// eased glide, always ending exactly on (gx,gy). Pure so it can be tested
// without touching real hardware.
func glidePath(cx, cy, gx, gy int) []action.Point {
	dist := math.Hypot(float64(gx-cx), float64(gy-cy))
	steps := int(dist) / glidePxStep
	if steps < 1 {
		steps = 1
	}
	if steps > glideMaxStep {
		steps = glideMaxStep
	}
	pts := make([]action.Point, 0, steps)
	for i := 1; i <= steps; i++ {
		t := easeInOut(float64(i) / float64(steps))
		pts = append(pts, action.Point{
			X: cx + int(math.Round(float64(gx-cx)*t)),
			Y: cy + int(math.Round(float64(gy-cy)*t)),
		})
	}
	// Guarantee the last point is exactly the target (rounding could fall short).
	if n := len(pts); n == 0 || pts[n-1] != (action.Point{X: gx, Y: gy}) {
		pts = append(pts, action.Point{X: gx, Y: gy})
	}
	return pts
}

// warpTo moves the pointer to global (gx,gy): a single warp, or an animated
// glide from the current position when smooth motion is enabled.
func (d *Driver) warpTo(gx, gy int) {
	if !d.smooth {
		robotgo.Move(gx, gy)
		return
	}
	cx, cy := robotgo.Location()
	for _, p := range glidePath(cx, cy, gx, gy) {
		robotgo.Move(p.X, p.Y)
		robotgo.MilliSleep(glideStepMS)
	}
}

// moveTo moves the cursor to the global position for (x, y) and lets the move
// register before the caller posts a button/scroll event. Use this instead of
// a bare robotgo.Move wherever an input event immediately follows the move.
func (d *Driver) moveTo(x, y int) {
	gx, gy := d.g(x, y)
	d.warpTo(gx, gy)
	robotgo.MilliSleep(moveSettleMS)
}

// DisplayBounds reports the driven display's global bounds in logical points,
// so callers working in global coordinates (the accessibility path) can align
// with the display this driver captures. Implements computer.DisplayBounder.
func (d *Driver) DisplayBounds() (x, y, w, h int) {
	return d.bounds(d.display)
}

// Screenshot captures the driven display. The actual capture is
// platform-specific (see captureDisplay): macOS uses the system screencapture
// targeting the display's global rect because robotgo's capture returns the
// main display regardless of index on current macOS.
func (d *Driver) Screenshot(ctx context.Context) (action.Image, error) {
	return d.captureDisplay(ctx)
}

// ScreenSize returns the target display's logical size. The executor derives
// the model→screen scale from this versus the captured pixel dimensions, so a
// HiDPI display is handled the same way as the primary.
func (d *Driver) ScreenSize(_ context.Context) (int, int, error) {
	_, _, w, h := d.bounds(d.display)
	return w, h, nil
}

// MoveMouse moves the pointer (glides when smooth motion is enabled).
func (d *Driver) MoveMouse(_ context.Context, x, y int) error {
	gx, gy := d.g(x, y)
	d.warpTo(gx, gy)
	return nil
}

// Click moves to (x,y) and clicks `clicks` times.
func (d *Driver) Click(_ context.Context, x, y int, b action.Button, clicks int) error {
	d.moveTo(x, y)
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
	d.moveTo(x, y)
	if err := robotgo.Toggle(buttonName(b), "down"); err != nil {
		return fmt.Errorf("robotgo mousedown: %w", err)
	}
	return nil
}

// MouseUp releases a button at (x,y).
func (d *Driver) MouseUp(_ context.Context, x, y int, b action.Button) error {
	d.moveTo(x, y)
	if err := robotgo.Toggle(buttonName(b), "up"); err != nil {
		return fmt.Errorf("robotgo mouseup: %w", err)
	}
	return nil
}

// Drag presses at the first point, moves through the rest, and releases. The
// press and release each follow a settle so neither is dropped; intermediate
// waypoints move without a settle to keep the drag smooth.
func (d *Driver) Drag(_ context.Context, path []action.Point, b action.Button) error {
	if len(path) < 2 {
		return fmt.Errorf("robotgo: drag needs >= 2 points")
	}
	name := buttonName(b)
	d.moveTo(path[0].X, path[0].Y)
	if err := robotgo.Toggle(name, "down"); err != nil {
		return fmt.Errorf("robotgo drag down: %w", err)
	}
	for _, p := range path[1:] {
		gx, gy := d.g(p.X, p.Y)
		if d.smooth {
			// Glide the held-button motion so the drag is visible and emits a
			// stream of mouseDragged events (glidePath already paces itself).
			d.warpTo(gx, gy)
			continue
		}
		robotgo.Move(gx, gy)
		// Space the intermediate motion over real wall-clock time: many drag
		// targets (HTML5 drag-and-drop, canvas apps, Finder) drive their state
		// machine off a stream of mouseDragged events and ignore a burst of
		// same-instant moves.
		robotgo.MilliSleep(dragStepMS)
	}
	robotgo.MilliSleep(moveSettleMS)
	if err := robotgo.Toggle(name, "up"); err != nil {
		return fmt.Errorf("robotgo drag up: %w", err)
	}
	return nil
}

// Scroll moves to (x,y) and scrolls by (dx,dy).
func (d *Driver) Scroll(_ context.Context, x, y, dx, dy int) error {
	d.moveTo(x, y)
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

// TypeText types literal text. Characters outside the Basic Multilingual Plane
// (emoji, supplementary-plane CJK) are typed by pasting from the clipboard,
// because robotgo's per-character Unicode typing truncates code points to 16
// bits on macOS and would emit garbage for them. Pure-BMP text takes the direct
// synthetic-keystroke path (which most text fields handle more faithfully than a
// paste). Note this inserts a literal newline for "\n"; callers that need the
// Return key to fire (e.g. submit-on-enter) must use KeyPress("enter").
func (d *Driver) TypeText(_ context.Context, text string) error {
	if hasNonBMP(text) {
		return d.pasteText(text)
	}
	robotgo.Type(text)
	return nil
}

// hasNonBMP reports whether s contains any rune above U+FFFF.
func hasNonBMP(s string) bool {
	for _, r := range s {
		if r > 0xFFFF {
			return true
		}
	}
	return false
}

// pasteText types text by round-tripping the clipboard (write, Cmd/Ctrl+V),
// restoring the prior clipboard contents afterward on a best-effort basis.
func (d *Driver) pasteText(text string) error {
	prev, _ := robotgo.ReadAll() // best effort; empty on error
	if err := robotgo.WriteAll(text); err != nil {
		return fmt.Errorf("robotgo clipboard write: %w", err)
	}
	mod := "cmd"
	if runtime.GOOS != "darwin" {
		mod = "ctrl"
	}
	err := robotgo.KeyTap("v", mod)
	robotgo.MilliSleep(moveSettleMS) // let the paste land before restoring the clipboard
	_ = robotgo.WriteAll(prev)       // restore; ignore failure so a clipboard hiccup doesn't fail the type
	if err != nil {
		return fmt.Errorf("robotgo paste: %w", err)
	}
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
	x, y := robotgo.Location()
	ox, oy := d.origin()
	return x - ox, y - oy, nil
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
