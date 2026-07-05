// Package actor implements the computer-use "act" step: it turns a resolved
// UI action request into calls on a computer.Computer driver.
//
// Every request in this package carries plain screen coordinates (X, Y, or
// FromX/FromY/ToX/ToY). Callers are responsible for resolving an
// ElementIndex (from a state.AppState produced by state.StateProvider) to
// those coordinates before calling into this package — ElementIndex is
// carried on each request purely for audit/logging purposes and is never
// interpreted here. This keeps Actor free of any dependency on how elements
// are discovered or indexed, and keeps it fully driver-agnostic.
package actor

import (
	"context"
	"fmt"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/computer"
)

// linesPerPage is the number of scroll lines a single "page" of Scroll
// direction maps to. It is an arbitrary but fixed convention so that
// Direction/Pages requests translate to a deterministic dx/dy.
const linesPerPage = 3

// ClickRequest describes a primary-button click at a resolved screen
// location. ElementIndex is carried for audit only; the click is issued at
// X, Y regardless of whether ElementIndex was set.
type ClickRequest struct {
	BundleIdentifier string
	ElementIndex     int
	X, Y             int
	// Button is "left", "right", or "middle" (case-sensitive). An empty
	// string defaults to "left".
	Button string
}

// TypeRequest describes literal text to type into the focused element of an
// application.
type TypeRequest struct {
	BundleIdentifier string
	Text             string
}

// KeyRequest describes a key or key-chord press within an application.
type KeyRequest struct {
	BundleIdentifier string
	Keys             []string
}

// ScrollRequest describes a scroll gesture at a resolved screen location.
// Direction is one of "up", "down", "left", "right"; Pages scales the
// gesture (Pages <= 0 is treated as 1). X, Y is the resolved point the gesture
// is issued at (the caller resolves ElementIndex to it upstream), so the scroll
// lands over the intended pane rather than wherever the pointer happened to be.
type ScrollRequest struct {
	BundleIdentifier string
	ElementIndex     int
	X, Y             int
	Direction        string
	Pages            int
}

// DragRequest describes a press-move-release gesture between two resolved
// screen locations, always performed with the left (primary) button.
type DragRequest struct {
	BundleIdentifier string
	FromX, FromY     int
	ToX, ToY         int
}

// SecondaryActionRequest describes a secondary (right-button) click at a
// resolved screen location, e.g. to open a context menu.
type SecondaryActionRequest struct {
	BundleIdentifier string
	ElementIndex     int
	X, Y             int
}

// Actor performs resolved UI actions against a computer. Implementations do
// not resolve ElementIndex to coordinates; that happens upstream against a
// state.AppState.
type Actor interface {
	// Click issues a button click at the request's coordinates.
	Click(ctx context.Context, req ClickRequest) error
	// TypeText types literal text into the currently focused element.
	TypeText(ctx context.Context, req TypeRequest) error
	// PressKey issues a key or key-chord press.
	PressKey(ctx context.Context, req KeyRequest) error
	// Scroll issues a scroll gesture at the request's coordinates.
	Scroll(ctx context.Context, req ScrollRequest) error
	// Drag issues a press-move-release gesture between two points.
	Drag(ctx context.Context, req DragRequest) error
	// PerformSecondaryAction issues a right-button click at the request's
	// coordinates.
	PerformSecondaryAction(ctx context.Context, req SecondaryActionRequest) error
}

// DefaultActor is the Actor implementation backed by a computer.Computer
// driver. It performs no coordinate resolution: every request's coordinates
// are passed straight through to the underlying driver.
type DefaultActor struct {
	computer computer.Computer
}

var _ Actor = (*DefaultActor)(nil)

// New returns a DefaultActor that drives c.
func New(c computer.Computer) *DefaultActor {
	return &DefaultActor{computer: c}
}

// buttonFromString maps a request's Button string to an action.Button. An
// empty or unrecognized string defaults to action.Left.
func buttonFromString(s string) action.Button {
	switch s {
	case "right":
		return action.Right
	case "middle":
		return action.Middle
	default:
		return action.Left
	}
}

// Click issues req.Button (default left) as a single click at req.X, req.Y.
func (a *DefaultActor) Click(ctx context.Context, req ClickRequest) error {
	return a.computer.Click(ctx, req.X, req.Y, buttonFromString(req.Button), 1)
}

// TypeText types req.Text into the currently focused element.
func (a *DefaultActor) TypeText(ctx context.Context, req TypeRequest) error {
	return a.computer.TypeText(ctx, req.Text)
}

// PressKey issues req.Keys as a single chord.
func (a *DefaultActor) PressKey(ctx context.Context, req KeyRequest) error {
	return a.computer.KeyPress(ctx, req.Keys...)
}

// Scroll converts req.Direction and req.Pages into a dx/dy delta and issues
// it at req.X, req.Y (the resolved target point), so the gesture lands over the
// intended element/pane. Direction follows the canonical contract that positive
// DY scrolls down:
//
//	"down"  -> dy = +pages*linesPerPage
//	"up"    -> dy = -pages*linesPerPage
//	"right" -> dx = +pages*linesPerPage
//	"left"  -> dx = -pages*linesPerPage
//
// Any other Direction is an error.
func (a *DefaultActor) Scroll(ctx context.Context, req ScrollRequest) error {
	pages := req.Pages
	if pages <= 0 {
		pages = 1
	}
	delta := pages * linesPerPage

	var dx, dy int
	switch req.Direction {
	case "down":
		dy = delta
	case "up":
		dy = -delta
	case "right":
		dx = delta
	case "left":
		dx = -delta
	default:
		return fmt.Errorf("actor: unknown scroll direction %q", req.Direction)
	}

	return a.computer.Scroll(ctx, req.X, req.Y, dx, dy)
}

// Drag performs a two-point drag from req.FromX, req.FromY to req.ToX,
// req.ToY using the left (primary) button.
func (a *DefaultActor) Drag(ctx context.Context, req DragRequest) error {
	path := []action.Point{
		{X: req.FromX, Y: req.FromY},
		{X: req.ToX, Y: req.ToY},
	}
	return a.computer.Drag(ctx, path, action.Left)
}

// PerformSecondaryAction issues a single right-button click at req.X, req.Y.
func (a *DefaultActor) PerformSecondaryAction(ctx context.Context, req SecondaryActionRequest) error {
	return a.computer.Click(ctx, req.X, req.Y, action.Right, 1)
}
