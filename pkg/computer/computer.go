// Package computer defines the single driver seam the agent loop depends on to
// observe and manipulate a desktop, plus the ActionExecutor that bridges
// canonical actions onto that seam in exactly one place. Coordinate scaling,
// set-of-marks resolution, and the capability allowlist all live in the
// executor so no other code has to reason about screen geometry or safety.
//
// LocalComputer (native driver) and RemoteComputer (WebSocket to an in-sandbox
// server) both satisfy Computer, so the loop is identical whether it drives the
// host or a sandbox.
package computer

import (
	"context"
	"io"

	"github.com/gnanam1990/argus/pkg/action"
)

// Screenshotter observes the screen.
type Screenshotter interface {
	// Screenshot captures the current screen as an encoded image.
	Screenshot(ctx context.Context) (action.Image, error)
	// ScreenSize reports the screen dimensions in driver (screen) pixels.
	ScreenSize(ctx context.Context) (w, h int, err error)
}

// InputController synthesizes mouse and keyboard input. All coordinates are in
// driver (screen) space; the ActionExecutor converts from model space before
// calling these.
type InputController interface {
	MoveMouse(ctx context.Context, x, y int) error
	Click(ctx context.Context, x, y int, b action.Button, clicks int) error
	MouseDown(ctx context.Context, x, y int, b action.Button) error
	MouseUp(ctx context.Context, x, y int, b action.Button) error
	Drag(ctx context.Context, path []action.Point, b action.Button) error
	Scroll(ctx context.Context, x, y, dx, dy int) error
	TypeText(ctx context.Context, text string) error
	KeyPress(ctx context.Context, keys ...string) error
	KeyDown(ctx context.Context, key string) error
	KeyUp(ctx context.Context, key string) error
	CursorPosition(ctx context.Context) (x, y int, err error)
}

// Computer is the full driver seam: observe, act, and release resources.
type Computer interface {
	Screenshotter
	InputController
	io.Closer
}
