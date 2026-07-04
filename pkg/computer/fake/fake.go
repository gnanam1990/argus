// Package fake provides a computer.Computer implementation that records every
// call and returns canned observations, so the executor and the agent loop can
// be tested with no display, no input synthesis, and no OS permissions.
package fake

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"sync"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/computer"
)

// Call is a recorded invocation of a Computer method. Only the fields relevant
// to Method are populated.
type Call struct {
	Method string
	X, Y   int
	Button action.Button
	Clicks int
	Text   string
	Keys   []string
	Path   []action.Point
	DX, DY int
}

// Computer is a recording, concurrency-safe fake driver. The zero value is not
// usable; construct with New.
type Computer struct {
	mu            sync.Mutex
	screenshot    action.Image
	w, h          int
	cursor        action.Point
	err           error
	screenSizeErr error
	calls         []Call
	closed        bool
}

var _ computer.Computer = (*Computer)(nil)

// New returns a fake with a decodable 100x100 PNG screenshot and a matching
// 100x100 screen (identity scale), so callers that don't care about scaling
// (most of the suite) never have to think about it, while still exercising
// the real image-decode path agent.Runner.observe relies on to compute scale.
func New() *Computer {
	return &Computer{
		screenshot: action.Image{MIME: action.MIMEPNG, Data: blankPNG(100, 100)},
		w:          100,
		h:          100,
	}
}

// blankPNG encodes a real, decodable w×h PNG. Unlike a hand-rolled byte
// fixture (e.g. just the magic-number prefix), this always round-trips
// through image.DecodeConfig, which is what observe() uses to size the
// screenshot for scale computation.
func blankPNG(w, h int) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		panic("computer/fake: encode default screenshot: " + err.Error())
	}
	return buf.Bytes()
}

// WithScreenshot sets the image and screen size returned by observations.
func (f *Computer) WithScreenshot(img action.Image, w, h int) *Computer {
	f.screenshot, f.w, f.h = img, w, h
	return f
}

// WithCursor sets the position returned by CursorPosition.
func (f *Computer) WithCursor(x, y int) *Computer {
	f.cursor = action.Point{X: x, Y: y}
	return f
}

// WithError makes every method return err, for exercising error paths.
func (f *Computer) WithError(err error) *Computer {
	f.err = err
	return f
}

// WithScreenSizeError makes only ScreenSize return err, while every other
// method (notably Screenshot) still succeeds. Unlike WithError's blanket
// failure, this isolates observe()'s scale-computation error path (H6) from
// screenshot capture.
func (f *Computer) WithScreenSizeError(err error) *Computer {
	f.screenSizeErr = err
	return f
}

func (f *Computer) record(c Call) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
	return f.err
}

// Screenshot returns the canned image.
func (f *Computer) Screenshot(context.Context) (action.Image, error) {
	if err := f.record(Call{Method: "Screenshot"}); err != nil {
		return action.Image{}, err
	}
	return f.screenshot, nil
}

// ScreenSize returns the canned screen dimensions.
func (f *Computer) ScreenSize(context.Context) (int, int, error) {
	if err := f.record(Call{Method: "ScreenSize"}); err != nil {
		return 0, 0, err
	}
	if f.screenSizeErr != nil {
		return 0, 0, f.screenSizeErr
	}
	return f.w, f.h, nil
}

// MoveMouse records a move.
func (f *Computer) MoveMouse(_ context.Context, x, y int) error {
	return f.record(Call{Method: "MoveMouse", X: x, Y: y})
}

// Click records a click.
func (f *Computer) Click(_ context.Context, x, y int, b action.Button, clicks int) error {
	return f.record(Call{Method: "Click", X: x, Y: y, Button: b, Clicks: clicks})
}

// MouseDown records a button press.
func (f *Computer) MouseDown(_ context.Context, x, y int, b action.Button) error {
	return f.record(Call{Method: "MouseDown", X: x, Y: y, Button: b})
}

// MouseUp records a button release.
func (f *Computer) MouseUp(_ context.Context, x, y int, b action.Button) error {
	return f.record(Call{Method: "MouseUp", X: x, Y: y, Button: b})
}

// Drag records a drag along path.
func (f *Computer) Drag(_ context.Context, path []action.Point, b action.Button) error {
	cp := make([]action.Point, len(path))
	copy(cp, path)
	return f.record(Call{Method: "Drag", Path: cp, Button: b})
}

// Scroll records a scroll.
func (f *Computer) Scroll(_ context.Context, x, y, dx, dy int) error {
	return f.record(Call{Method: "Scroll", X: x, Y: y, DX: dx, DY: dy})
}

// TypeText records typed text.
func (f *Computer) TypeText(_ context.Context, text string) error {
	return f.record(Call{Method: "TypeText", Text: text})
}

// KeyPress records a chord.
func (f *Computer) KeyPress(_ context.Context, keys ...string) error {
	cp := make([]string, len(keys))
	copy(cp, keys)
	return f.record(Call{Method: "KeyPress", Keys: cp})
}

// KeyDown records a key press-and-hold.
func (f *Computer) KeyDown(_ context.Context, key string) error {
	return f.record(Call{Method: "KeyDown", Keys: []string{key}})
}

// KeyUp records a key release.
func (f *Computer) KeyUp(_ context.Context, key string) error {
	return f.record(Call{Method: "KeyUp", Keys: []string{key}})
}

// CursorPosition returns the canned cursor position.
func (f *Computer) CursorPosition(context.Context) (int, int, error) {
	if err := f.record(Call{Method: "CursorPosition"}); err != nil {
		return 0, 0, err
	}
	return f.cursor.X, f.cursor.Y, nil
}

// Close marks the fake closed.
func (f *Computer) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	f.calls = append(f.calls, Call{Method: "Close"})
	return f.err
}

// Closed reports whether Close was called.
func (f *Computer) Closed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// Calls returns a copy of the recorded call log.
func (f *Computer) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Call, len(f.calls))
	copy(out, f.calls)
	return out
}

// Last returns the most recent recorded call and true, or a zero Call and
// false if none have been recorded.
func (f *Computer) Last() (Call, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return Call{}, false
	}
	return f.calls[len(f.calls)-1], true
}
