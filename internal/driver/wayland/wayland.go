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
	"fmt"
	"image"
	_ "image/png" // register PNG decoder for screen-size decoding
	"os"
	"os/exec"
	"strconv"
	"strings"

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
		run:      ExecRunner{},
		ydotool:  "ydotool",
		capture:  defaultCaptureChain,
		readFile: os.ReadFile,
		tempFile: defaultTempFile,
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

func (d *Driver) yd(ctx context.Context, args ...string) error {
	if _, err := d.run.Run(ctx, d.ydotool, args...); err != nil {
		return fmt.Errorf("wayland: %s %s: %w", d.ydotool, strings.Join(args, " "), err)
	}
	return nil
}

// Screenshot captures the screen as PNG, trying each configured tool until one
// yields a non-empty image.
func (d *Driver) Screenshot(ctx context.Context) (action.Image, error) {
	var errs []string
	for _, c := range d.capture {
		data, err := d.captureOne(ctx, c)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", c.name, err))
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

// MoveMouse moves the pointer to absolute (x,y).
func (d *Driver) MoveMouse(ctx context.Context, x, y int) error {
	return d.yd(ctx, "mousemove", "--absolute", "-x", itoa(x), "-y", itoa(y))
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
	return d.yd(ctx, "mousemove", "--wheel", "-x", itoa(-dx), "-y", itoa(-dy))
}

// TypeText types literal text.
func (d *Driver) TypeText(ctx context.Context, text string) error {
	return d.yd(ctx, "type", text)
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
