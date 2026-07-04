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

	"github.com/go-vgo/robotgo"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/computer"
)

// Driver drives the host via robotgo.
type Driver struct{}

// New builds a robotgo driver.
func New() *Driver { return &Driver{} }

var _ computer.Computer = (*Driver)(nil)

// Screenshot captures the screen and encodes it as PNG.
func (d *Driver) Screenshot(_ context.Context) (action.Image, error) {
	img, err := robotgo.CaptureImg()
	if err != nil {
		return action.Image{}, fmt.Errorf("robotgo capture: %w", err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return action.Image{}, fmt.Errorf("robotgo encode: %w", err)
	}
	return action.Image{MIME: action.MIMEPNG, Data: buf.Bytes()}, nil
}

// ScreenSize returns the primary display size.
func (d *Driver) ScreenSize(_ context.Context) (int, int, error) {
	w, h := robotgo.GetScreenSize()
	return w, h, nil
}

// MoveMouse moves the pointer.
func (d *Driver) MoveMouse(_ context.Context, x, y int) error {
	robotgo.Move(x, y)
	return nil
}

// Click moves to (x,y) and clicks `clicks` times.
func (d *Driver) Click(_ context.Context, x, y int, b action.Button, clicks int) error {
	robotgo.Move(x, y)
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
	robotgo.Move(x, y)
	if err := robotgo.Toggle(buttonName(b), "down"); err != nil {
		return fmt.Errorf("robotgo mousedown: %w", err)
	}
	return nil
}

// MouseUp releases a button at (x,y).
func (d *Driver) MouseUp(_ context.Context, x, y int, b action.Button) error {
	robotgo.Move(x, y)
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
	robotgo.Move(path[0].X, path[0].Y)
	if err := robotgo.Toggle(name, "down"); err != nil {
		return fmt.Errorf("robotgo drag down: %w", err)
	}
	for _, p := range path[1:] {
		robotgo.Move(p.X, p.Y)
	}
	if err := robotgo.Toggle(name, "up"); err != nil {
		return fmt.Errorf("robotgo drag up: %w", err)
	}
	return nil
}

// Scroll moves to (x,y) and scrolls by (dx,dy).
func (d *Driver) Scroll(_ context.Context, x, y, dx, dy int) error {
	robotgo.Move(x, y)
	robotgo.Scroll(dx, dy)
	return nil
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
	key := keys[len(keys)-1]
	mods := make([]interface{}, 0, len(keys)-1)
	for _, m := range keys[:len(keys)-1] {
		mods = append(mods, m)
	}
	if err := robotgo.KeyTap(key, mods...); err != nil {
		return fmt.Errorf("robotgo keytap: %w", err)
	}
	return nil
}

// KeyDown presses and holds a key.
func (d *Driver) KeyDown(_ context.Context, key string) error {
	if err := robotgo.KeyToggle(key, "down"); err != nil {
		return fmt.Errorf("robotgo keydown: %w", err)
	}
	return nil
}

// KeyUp releases a key.
func (d *Driver) KeyUp(_ context.Context, key string) error {
	if err := robotgo.KeyToggle(key, "up"); err != nil {
		return fmt.Errorf("robotgo keyup: %w", err)
	}
	return nil
}

// CursorPosition returns the pointer location.
func (d *Driver) CursorPosition(_ context.Context) (int, int, error) {
	x, y := robotgo.GetMousePos()
	return x, y, nil
}

// Close is a no-op.
func (d *Driver) Close() error { return nil }

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
