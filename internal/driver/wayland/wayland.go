// Package wayland implements computer.Computer for Wayland sessions, where the
// X11 shell driver (xdotool/maim/xrandr) does not work. Input goes through
// ydotool, which injects at the kernel uinput layer and so works across every
// compositor (wlroots, GNOME/KWin) regardless of Wayland's synthetic-input
// restrictions; screenshots use a fallback chain (grim → gnome-screenshot →
// spectacle) covering wlroots and the desktop environments; and the screen size
// is read from the screenshot itself, so no compositor-specific geometry tool
// is required.
//
// ydotool needs its daemon (ydotoold) running and access to /dev/uinput — see
// Preflight/doctor for the check. Every command is overridable so a user whose
// ydotool/screenshot tool differs can adapt without a code change. Like the
// shell driver it takes an injectable Runner so unit tests assert exact argv
// with no real binary executed; the default build stays CGo-free.
package wayland

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/png" // register PNG decoder for screen-size decoding
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/computer"
)

// Runner executes an external command and returns its stdout.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner runs commands with os/exec.
type ExecRunner struct{}

// Run executes name with args and returns stdout.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// captureCmd is one screenshot tool to try. If any arg is the placeholder
// "{}", the tool writes PNG to that temp file (read back after); otherwise its
// stdout is the PNG.
type captureCmd struct {
	name string
	args []string
}

// defaultCaptureChain covers the common Wayland screenshot tools in order:
// grim (wlroots, stdout), gnome-screenshot (GNOME, file), spectacle (KDE, file).
var defaultCaptureChain = []captureCmd{
	{"grim", []string{"-"}},
	{"gnome-screenshot", []string{"-f", "{}"}},
	{"spectacle", []string{"-b", "-n", "-o", "{}"}},
}

// Driver is the Wayland driver.
type Driver struct {
	run     Runner
	ydotool string
	capture []captureCmd
	// readFile/tempFile are indirections so the file-based screenshot path is
	// testable without touching the real filesystem.
	readFile func(string) ([]byte, error)
	tempFile func() (string, func(), error)

	// mu guards the resolved-syntax caches below. ydotool's CLI differs across
	// versions/builds (long vs short flags, positional coords), so the first
	// successful argv shape per operation is probed once and reused.
	mu         sync.Mutex
	moveStyle  int
	wheelStyle int
}

// Option configures a Driver.
type Option func(*Driver)

// WithRunner overrides the command runner (for tests).
func WithRunner(r Runner) Option { return func(d *Driver) { d.run = r } }

// WithYdotool overrides the ydotool binary name/path.
func WithYdotool(name string) Option { return func(d *Driver) { d.ydotool = name } }

// WithCaptureCommand replaces the screenshot fallback chain with a single
// command. Use "{}" as a placeholder for a temp file the tool should write PNG
// to; without it, the tool's stdout is taken as the PNG.
func WithCaptureCommand(name string, args ...string) Option {
	return func(d *Driver) { d.capture = []captureCmd{{name, args}} }
}

// New builds a Wayland driver.
func New(opts ...Option) *Driver {
	d := &Driver{
		run:        ExecRunner{},
		ydotool:    "ydotool",
		capture:    defaultCaptureChain,
		readFile:   os.ReadFile,
		tempFile:   defaultTempFile,
		moveStyle:  -1,
		wheelStyle: -1,
	}
	for _, o := range opts {
		o(d)
	}
	return d
}

var _ computer.Computer = (*Driver)(nil)

func defaultTempFile() (string, func(), error) {
	f, err := os.CreateTemp("", "argus-wl-shot-*.png")
	if err != nil {
		return "", nil, err
	}
	name := f.Name()
	_ = f.Close()
	return name, func() { _ = os.Remove(name) }, nil
}

// stderrOf returns a failed command's captured stderr — exec.Cmd.Output's
// *ExitError carries it, while its Error() is just "exit status N", which told
// the user nothing — falling back to the error text when no stderr exists.
func stderrOf(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(ee.Stderr) > 0 {
		return strings.TrimSpace(string(ee.Stderr))
	}
	return err.Error()
}

// isDaemonErr reports whether a ydotool failure is about reaching its daemon
// (ydotoold) rather than about the command itself: a missing or root-owned
// socket is by far the most common Wayland setup failure.
func isDaemonErr(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "socket") || strings.Contains(m, "connect") ||
		strings.Contains(m, "permission denied") || strings.Contains(m, "ydotoold") ||
		strings.Contains(m, "backend unavailable")
}

// daemonError explains the ydotoold socket problem with the fix inline: a bare
// `sudo ydotoold` creates a root-owned socket the user's ydotool client cannot
// use, which surfaces as an opaque non-zero exit.
func daemonError(bin string, args []string, msg string, err error) error {
	return fmt.Errorf("wayland: %s %s: %s — ydotool cannot reach its daemon. Start ydotoold "+
		"so its socket is owned by your user: sudo ydotoold --socket-own=\"$(id -u):$(id -g)\" "+
		"(or point YDOTOOL_SOCKET at the daemon's socket): %w",
		bin, strings.Join(args, " "), msg, err)
}

func (d *Driver) yd(ctx context.Context, args ...string) error {
	if _, err := d.run.Run(ctx, d.ydotool, args...); err != nil {
		msg := stderrOf(err)
		if isDaemonErr(msg) {
			return daemonError(d.ydotool, args, msg, err)
		}
		return fmt.Errorf("wayland: %s %s: %s: %w", d.ydotool, strings.Join(args, " "), msg, err)
	}
	return nil
}

// argvStyle builds one version-specific argv shape from two coordinates.
type argvStyle func(a, b int) []string

// moveStyles are the known `ydotool mousemove` absolute-position syntaxes, in
// preference order: 1.0.x long flags, short flags (builds whose getopt lacks
// the long names), and the old 0.1.x positional form.
var moveStyles = []argvStyle{
	func(x, y int) []string { return []string{"mousemove", "--absolute", "-x", itoa(x), "-y", itoa(y)} },
	func(x, y int) []string { return []string{"mousemove", "-a", "-x", itoa(x), "-y", itoa(y)} },
	func(x, y int) []string { return []string{"mousemove", "--absolute", itoa(x), itoa(y)} },
}

// wheelStyles are the known wheel-scroll syntaxes (1.0.x long/short flags; the
// 0.1.x tool had no wheel support at all).
var wheelStyles = []argvStyle{
	func(dx, dy int) []string { return []string{"mousemove", "--wheel", "-x", itoa(dx), "-y", itoa(dy)} },
	func(dx, dy int) []string { return []string{"mousemove", "-w", "-x", itoa(dx), "-y", itoa(dy)} },
}

// runAdaptive executes the operation using the cached working syntax, or probes
// the known syntaxes in order and caches the first that succeeds. A daemon
// failure aborts immediately (no syntax would work); anything else falls
// through to the next shape so a CLI mismatch self-heals instead of hard-coding
// one ydotool version's flags.
func (d *Driver) runAdaptive(ctx context.Context, cache *int, styles []argvStyle, a, b int, what string) error {
	d.mu.Lock()
	idx := *cache
	d.mu.Unlock()
	if idx >= 0 {
		return d.yd(ctx, styles[idx](a, b)...)
	}

	var failures []string
	for i, style := range styles {
		args := style(a, b)
		_, err := d.run.Run(ctx, d.ydotool, args...)
		if err == nil {
			d.mu.Lock()
			*cache = i
			d.mu.Unlock()
			return nil
		}
		msg := stderrOf(err)
		if isDaemonErr(msg) {
			return daemonError(d.ydotool, args, msg, err)
		}
		failures = append(failures, fmt.Sprintf("%q: %s", strings.Join(args, " "), msg))
	}
	return fmt.Errorf("wayland: every known ydotool %s syntax failed — need ydotool >= 1.0 "+
		"(and the ydotoold daemon running): %s", what, strings.Join(failures, "; "))
}

// Screenshot captures the screen as PNG, trying each configured tool until one
// yields a non-empty image.
func (d *Driver) Screenshot(ctx context.Context) (action.Image, error) {
	var errs []string
	for _, c := range d.capture {
		data, err := d.captureOne(ctx, c)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %s", c.name, stderrOf(err)))
			continue
		}
		if len(data) > 0 {
			return action.Image{MIME: action.MIMEPNG, Data: data}, nil
		}
		errs = append(errs, fmt.Sprintf("%s: empty output", c.name))
	}
	return action.Image{}, fmt.Errorf("wayland: screenshot failed (install grim/gnome-screenshot/spectacle): %s",
		strings.Join(errs, "; "))
}

func (d *Driver) captureOne(ctx context.Context, c captureCmd) ([]byte, error) {
	fileIdx := -1
	for i, a := range c.args {
		if a == "{}" {
			fileIdx = i
			break
		}
	}
	if fileIdx < 0 {
		return d.run.Run(ctx, c.name, c.args...) // PNG on stdout
	}
	path, cleanup, err := d.tempFile()
	if err != nil {
		return nil, err
	}
	defer cleanup()
	args := append([]string(nil), c.args...)
	args[fileIdx] = path
	if _, err := d.run.Run(ctx, c.name, args...); err != nil {
		return nil, err
	}
	return d.readFile(path)
}

// ScreenSize decodes the current screenshot's pixel dimensions. Wayland has no
// universal geometry CLI, and the screenshot is the coordinate space actions
// resolve against anyway, so its own size is the reference.
func (d *Driver) ScreenSize(ctx context.Context) (int, int, error) {
	img, err := d.Screenshot(ctx)
	if err != nil {
		return 0, 0, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(img.Data))
	if err != nil {
		return 0, 0, fmt.Errorf("wayland: decode screenshot size: %w", err)
	}
	return cfg.Width, cfg.Height, nil
}

// MoveMouse moves the pointer to absolute (x,y), adapting to whichever
// mousemove syntax the installed ydotool speaks.
func (d *Driver) MoveMouse(ctx context.Context, x, y int) error {
	return d.runAdaptive(ctx, &d.moveStyle, moveStyles, x, y, "mousemove")
}

// Click moves to (x,y) and clicks the button `clicks` times.
func (d *Driver) Click(ctx context.Context, x, y int, b action.Button, clicks int) error {
	if clicks <= 0 {
		clicks = 1
	}
	if err := d.MoveMouse(ctx, x, y); err != nil {
		return err
	}
	for i := 0; i < clicks; i++ {
		if err := d.yd(ctx, "click", clickCode(b)); err != nil {
			return err
		}
	}
	return nil
}

// MouseDown presses a button at (x,y).
func (d *Driver) MouseDown(ctx context.Context, x, y int, b action.Button) error {
	if err := d.MoveMouse(ctx, x, y); err != nil {
		return err
	}
	return d.yd(ctx, "click", downCode(b))
}

// MouseUp releases a button at (x,y).
func (d *Driver) MouseUp(ctx context.Context, x, y int, b action.Button) error {
	if err := d.MoveMouse(ctx, x, y); err != nil {
		return err
	}
	return d.yd(ctx, "click", upCode(b))
}

// Drag presses at the first path point, moves through the rest, and releases.
func (d *Driver) Drag(ctx context.Context, path []action.Point, b action.Button) error {
	if len(path) < 2 {
		return fmt.Errorf("wayland: drag needs >= 2 points")
	}
	if err := d.MoveMouse(ctx, path[0].X, path[0].Y); err != nil {
		return err
	}
	if err := d.yd(ctx, "click", downCode(b)); err != nil {
		return err
	}
	for _, p := range path[1:] {
		if err := d.MoveMouse(ctx, p.X, p.Y); err != nil {
			return err
		}
	}
	return d.yd(ctx, "click", upCode(b))
}

// Scroll moves to (x,y) and scrolls by (dx,dy) using ydotool's wheel. Positive
// dy scrolls down, positive dx right (the canonical convention); ydotool's
// wheel takes the opposite sign, so both axes are negated.
func (d *Driver) Scroll(ctx context.Context, x, y, dx, dy int) error {
	if err := d.MoveMouse(ctx, x, y); err != nil {
		return err
	}
	return d.runAdaptive(ctx, &d.wheelStyle, wheelStyles, -dx, -dy, "wheel scroll")
}

// TypeText types literal text. "--" ends option parsing so text beginning with
// a dash is typed, not read as a flag.
func (d *Driver) TypeText(ctx context.Context, text string) error {
	return d.yd(ctx, "type", "--", text)
}

// KeyPress presses a key chord: every key down in order, then up in reverse, so
// modifiers wrap the main key.
func (d *Driver) KeyPress(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	codes := make([]int, len(keys))
	for i, k := range keys {
		c, ok := keyCode(k)
		if !ok {
			return fmt.Errorf("wayland: unknown key %q", k)
		}
		codes[i] = c
	}
	args := []string{"key"}
	for _, c := range codes {
		args = append(args, itoa(c)+":1")
	}
	for i := len(codes) - 1; i >= 0; i-- {
		args = append(args, itoa(codes[i])+":0")
	}
	return d.yd(ctx, args...)
}

// KeyDown presses and holds a key.
func (d *Driver) KeyDown(ctx context.Context, key string) error {
	c, ok := keyCode(key)
	if !ok {
		return fmt.Errorf("wayland: unknown key %q", key)
	}
	return d.yd(ctx, "key", itoa(c)+":1")
}

// KeyUp releases a key.
func (d *Driver) KeyUp(ctx context.Context, key string) error {
	c, ok := keyCode(key)
	if !ok {
		return fmt.Errorf("wayland: unknown key %q", key)
	}
	return d.yd(ctx, "key", itoa(c)+":0")
}

// CursorPosition is not available on Wayland: there is no portable way to read
// the pointer location (the compositor does not expose it, and ydotool cannot
// query it). Callers that need it must track position themselves.
func (d *Driver) CursorPosition(ctx context.Context) (int, int, error) {
	return 0, 0, fmt.Errorf("wayland: cursor position is not readable on Wayland")
}

// Close is a no-op.
func (d *Driver) Close() error { return nil }

func itoa(i int) string { return strconv.Itoa(i) }

// ydotool click button codes: low bits select the button (0 left, 1 right,
// 2 middle); 0x40 = press, 0x80 = release, 0xC0 = press+release (a full click).
func buttonBit(b action.Button) int {
	switch b {
	case action.Right:
		return 1
	case action.Middle:
		return 2
	default:
		return 0
	}
}

func clickCode(b action.Button) string { return hex(0xC0 | buttonBit(b)) }
func downCode(b action.Button) string  { return hex(0x40 | buttonBit(b)) }
func upCode(b action.Button) string    { return hex(0x80 | buttonBit(b)) }

func hex(v int) string { return fmt.Sprintf("0x%02X", v) }
