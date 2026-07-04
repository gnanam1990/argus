// Package shell implements computer.Computer on Linux/X11 by shelling out to
// xdotool, a screenshot tool (maim by default), and xrandr. It uses an
// injectable Runner so unit tests assert exact argv and decode fixture output
// without executing any real binary — the default build is CGo-free.
package shell

import (
	"context"
	"fmt"
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

// Run executes name with args and returns combined stdout.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// Driver is the X11 shell driver.
type Driver struct {
	run           Runner
	screenshotCmd []string
}

// Option configures a Driver.
type Option func(*Driver)

// WithRunner overrides the command runner (for tests).
func WithRunner(r Runner) Option { return func(d *Driver) { d.run = r } }

// WithScreenshotCommand overrides the screenshot command (must write PNG to
// stdout). Default: maim.
func WithScreenshotCommand(cmd ...string) Option {
	return func(d *Driver) { d.screenshotCmd = cmd }
}

// New builds a shell driver.
func New(opts ...Option) *Driver {
	d := &Driver{run: ExecRunner{}, screenshotCmd: []string{"maim"}}
	for _, o := range opts {
		o(d)
	}
	return d
}

var _ computer.Computer = (*Driver)(nil)

func (d *Driver) xdotool(ctx context.Context, args ...string) error {
	_, err := d.run.Run(ctx, "xdotool", args...)
	if err != nil {
		return fmt.Errorf("shell: xdotool %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// Screenshot captures the screen as PNG via the configured command.
func (d *Driver) Screenshot(ctx context.Context) (action.Image, error) {
	out, err := d.run.Run(ctx, d.screenshotCmd[0], d.screenshotCmd[1:]...)
	if err != nil {
		return action.Image{}, fmt.Errorf("shell: screenshot: %w", err)
	}
	return action.Image{MIME: action.MIMEPNG, Data: out}, nil
}

// ScreenSize parses the current resolution from xrandr.
func (d *Driver) ScreenSize(ctx context.Context) (int, int, error) {
	out, err := d.run.Run(ctx, "xrandr")
	if err != nil {
		return 0, 0, fmt.Errorf("shell: xrandr: %w", err)
	}
	return parseXrandr(string(out))
}

func parseXrandr(s string) (int, int, error) {
	idx := strings.Index(s, "current ")
	if idx < 0 {
		return 0, 0, fmt.Errorf("shell: xrandr: no current resolution")
	}
	var w, h int
	if _, err := fmt.Sscanf(s[idx+len("current "):], "%d x %d", &w, &h); err != nil {
		return 0, 0, fmt.Errorf("shell: xrandr parse: %w", err)
	}
	return w, h, nil
}

// MoveMouse moves the pointer.
func (d *Driver) MoveMouse(ctx context.Context, x, y int) error {
	return d.xdotool(ctx, "mousemove", itoa(x), itoa(y))
}

// Click moves to (x,y) and clicks the button `clicks` times.
func (d *Driver) Click(ctx context.Context, x, y int, b action.Button, clicks int) error {
	if clicks <= 0 {
		clicks = 1
	}
	args := []string{"mousemove", itoa(x), itoa(y), "click"}
	if clicks > 1 {
		args = append(args, "--repeat", itoa(clicks))
	}
	return d.xdotool(ctx, append(args, buttonNum(b))...)
}

// MouseDown presses a button at (x,y).
func (d *Driver) MouseDown(ctx context.Context, x, y int, b action.Button) error {
	return d.xdotool(ctx, "mousemove", itoa(x), itoa(y), "mousedown", buttonNum(b))
}

// MouseUp releases a button at (x,y).
func (d *Driver) MouseUp(ctx context.Context, x, y int, b action.Button) error {
	return d.xdotool(ctx, "mousemove", itoa(x), itoa(y), "mouseup", buttonNum(b))
}

// Drag presses at the first path point, moves through the rest, and releases.
func (d *Driver) Drag(ctx context.Context, path []action.Point, b action.Button) error {
	if len(path) < 2 {
		return fmt.Errorf("shell: drag needs >= 2 points")
	}
	btn := buttonNum(b)
	args := []string{"mousemove", itoa(path[0].X), itoa(path[0].Y), "mousedown", btn}
	for _, p := range path[1:] {
		args = append(args, "mousemove", itoa(p.X), itoa(p.Y))
	}
	args = append(args, "mouseup", btn)
	return d.xdotool(ctx, args...)
}

// Scroll moves to (x,y) and emits wheel clicks. Vertical uses buttons 4/5,
// horizontal 6/7.
func (d *Driver) Scroll(ctx context.Context, x, y, dx, dy int) error {
	if err := d.MoveMouse(ctx, x, y); err != nil {
		return err
	}
	if dy != 0 {
		btn, n := "5", dy // down
		if dy < 0 {
			btn, n = "4", -dy // up
		}
		if err := d.wheel(ctx, btn, n); err != nil {
			return err
		}
	}
	if dx != 0 {
		btn, n := "7", dx // right
		if dx < 0 {
			btn, n = "6", -dx // left
		}
		if err := d.wheel(ctx, btn, n); err != nil {
			return err
		}
	}
	return nil
}

func (d *Driver) wheel(ctx context.Context, btn string, n int) error {
	args := []string{"click"}
	if n > 1 {
		args = append(args, "--repeat", itoa(n))
	}
	return d.xdotool(ctx, append(args, btn)...)
}

// TypeText types literal text.
func (d *Driver) TypeText(ctx context.Context, text string) error {
	return d.xdotool(ctx, "type", "--", text)
}

// KeyPress presses a key chord (keys joined with +).
func (d *Driver) KeyPress(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return d.xdotool(ctx, "key", strings.Join(mapKeys(keys), "+"))
}

// KeyDown presses and holds a key.
func (d *Driver) KeyDown(ctx context.Context, key string) error {
	return d.xdotool(ctx, "keydown", mapKey(key))
}

// KeyUp releases a key.
func (d *Driver) KeyUp(ctx context.Context, key string) error {
	return d.xdotool(ctx, "keyup", mapKey(key))
}

// CursorPosition reads the pointer location from xdotool.
func (d *Driver) CursorPosition(ctx context.Context) (int, int, error) {
	out, err := d.run.Run(ctx, "xdotool", "getmouselocation", "--shell")
	if err != nil {
		return 0, 0, fmt.Errorf("shell: getmouselocation: %w", err)
	}
	return parseMouseLocation(string(out))
}

func parseMouseLocation(s string) (int, int, error) {
	var x, y int
	xok, yok := false, false
	for _, line := range strings.Split(s, "\n") {
		switch {
		case strings.HasPrefix(line, "X="):
			if _, err := fmt.Sscanf(line, "X=%d", &x); err == nil {
				xok = true
			}
		case strings.HasPrefix(line, "Y="):
			if _, err := fmt.Sscanf(line, "Y=%d", &y); err == nil {
				yok = true
			}
		}
	}
	if !xok || !yok {
		return 0, 0, fmt.Errorf("shell: could not parse mouse location %q", s)
	}
	return x, y, nil
}

// Close is a no-op for the shell driver.
func (d *Driver) Close() error { return nil }

func buttonNum(b action.Button) string {
	switch b {
	case action.Right:
		return "3"
	case action.Middle:
		return "2"
	default:
		return "1"
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

// keymap translates canonical key names to X keysyms for the common cases.
var keymap = map[string]string{
	"enter": "Return", "return": "Return", "esc": "Escape", "escape": "Escape",
	"tab": "Tab", "space": "space", "backspace": "BackSpace", "delete": "Delete",
	"up": "Up", "down": "Down", "left": "Left", "right": "Right",
	"home": "Home", "end": "End", "pageup": "Prior", "pagedown": "Next",
	"cmd": "super", "win": "super", "meta": "super", "option": "alt", "opt": "alt",
}

func mapKey(k string) string {
	if v, ok := keymap[strings.ToLower(k)]; ok {
		return v
	}
	return k
}

func mapKeys(keys []string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = mapKey(k)
	}
	return out
}
