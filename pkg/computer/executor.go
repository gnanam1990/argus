package computer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gnanam1990/argus/pkg/action"
)

// Sentinel errors returned by the executor.
var (
	// ErrCapabilityDenied means a gated action was requested but is not in the
	// allowlist. The allowlist is off by default, so every gated action is
	// denied unless explicitly enabled.
	ErrCapabilityDenied = errors.New("computer: action denied by capability allowlist")
	// ErrUnsupported means the action is valid and permitted but the underlying
	// Computer cannot perform it (e.g. run_command needs a Sandbox, not a bare
	// Computer).
	ErrUnsupported = errors.New("computer: action not supported by this executor")
	// ErrUnknownMark means an action referenced a set-of-marks index that is
	// not in the current index.
	ErrUnknownMark = errors.New("computer: unknown set-of-marks index")
)

// ActionExecutor turns a canonical action into concrete Computer calls. It is
// the single place that applies coordinate scaling, resolves set-of-marks
// indices, and enforces the capability allowlist.
type ActionExecutor interface {
	Execute(ctx context.Context, a action.Action) (action.Result, error)
}

// Executor is the default ActionExecutor over a Computer.
//
// It is single-owner (one session drives it); Scale and Marks are updated by
// the loop after each screenshot/grounding pass and read during Execute.
type Executor struct {
	c Computer

	sx, sy float64                    // model-space → screen-space multipliers
	marks  map[int]action.Rect        // set-of-marks index (screenshot space)
	allow  map[action.ActionType]bool // gated-capability allowlist
}

// ExecutorOption configures an Executor.
type ExecutorOption func(*Executor)

// WithCapabilities adds gated action types to the allowlist. Only gated types
// are meaningful here; non-gated actions are always permitted.
func WithCapabilities(types ...action.ActionType) ExecutorOption {
	return func(e *Executor) {
		for _, t := range types {
			e.allow[t] = true
		}
	}
}

// NewExecutor builds an executor over c with identity scale and an empty
// allowlist (all gated actions denied).
func NewExecutor(c Computer, opts ...ExecutorOption) *Executor {
	e := &Executor{
		c:     c,
		sx:    1,
		sy:    1,
		allow: make(map[action.ActionType]bool),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// SetScale sets the per-axis model→screen multipliers. The loop computes these
// from the screenshot it sent the model versus the driver's ScreenSize.
func (e *Executor) SetScale(sx, sy float64) { e.sx, e.sy = sx, sy }

// SetMarks installs the current set-of-marks index (screenshot-space boxes).
func (e *Executor) SetMarks(marks map[int]action.Rect) { e.marks = marks }

// Allowed reports whether the executor may run action type t.
func (e *Executor) Allowed(t action.ActionType) bool {
	if t.Gated() {
		return e.allow[t]
	}
	return true
}

// Execute validates a, enforces the capability gate, resolves its target
// coordinates, and dispatches to the Computer.
func (e *Executor) Execute(ctx context.Context, a action.Action) (action.Result, error) {
	if err := a.Validate(); err != nil {
		return action.Result{}, err
	}
	if !e.Allowed(a.Type) {
		return action.Result{}, fmt.Errorf("%w: %s", ErrCapabilityDenied, a.Type)
	}

	switch a.Type {
	case action.Click, action.DoubleClick, action.TripleClick:
		p, err := e.target(a)
		if err != nil {
			return action.Result{}, err
		}
		return action.Result{}, e.c.Click(ctx, p.X, p.Y, a.Button, clicksFor(a))

	case action.Move:
		p, err := e.target(a)
		if err != nil {
			return action.Result{}, err
		}
		return action.Result{}, e.c.MoveMouse(ctx, p.X, p.Y)

	case action.MouseDown:
		p, err := e.target(a)
		if err != nil {
			return action.Result{}, err
		}
		return action.Result{}, e.c.MouseDown(ctx, p.X, p.Y, a.Button)

	case action.MouseUp:
		p, err := e.target(a)
		if err != nil {
			return action.Result{}, err
		}
		return action.Result{}, e.c.MouseUp(ctx, p.X, p.Y, a.Button)

	case action.Drag:
		path := make([]action.Point, len(a.Path))
		for i, p := range a.Path {
			path[i] = e.scale(p)
		}
		return action.Result{}, e.c.Drag(ctx, path, a.Button)

	case action.Scroll:
		p, err := e.target(a)
		if err != nil {
			return action.Result{}, err
		}
		// Scroll deltas are in wheel units, not pixels, so they are not scaled.
		return action.Result{}, e.c.Scroll(ctx, p.X, p.Y, a.DX, a.DY)

	case action.Type:
		return action.Result{}, e.c.TypeText(ctx, a.Text)

	case action.Key:
		return action.Result{}, e.c.KeyPress(ctx, a.Keys...)

	case action.KeyDown:
		return action.Result{}, e.keyEach(ctx, a.Keys, e.c.KeyDown)

	case action.KeyUp:
		return action.Result{}, e.keyEach(ctx, a.Keys, e.c.KeyUp)

	case action.Wait:
		return action.Result{}, sleep(ctx, a.Dur)

	case action.Screenshot:
		img, err := e.c.Screenshot(ctx)
		if err != nil {
			return action.Result{}, err
		}
		return action.Result{Screenshot: img}, nil

	case action.CursorPosition:
		x, y, err := e.c.CursorPosition(ctx)
		if err != nil {
			return action.Result{}, err
		}
		// Report the position back in model space so the model reasons in the
		// same coordinates it clicks in.
		return action.Result{Cursor: e.unscale(action.Point{X: x, Y: y})}, nil

	case action.Terminate:
		return action.Result{Terminated: true}, nil

	default:
		// Gated system/window actions (run_command, read_file, write_file,
		// window_focus, window_move) need a Sandbox or window manager the bare
		// Computer does not provide.
		return action.Result{}, fmt.Errorf("%w: %s", ErrUnsupported, a.Type)
	}
}

// target resolves an action's click point in screen space: a set-of-marks
// center when Mark is set, otherwise the raw Point, then scaled.
func (e *Executor) target(a action.Action) (action.Point, error) {
	if a.HasMark() {
		r, ok := e.marks[a.Mark]
		if !ok {
			return action.Point{}, fmt.Errorf("%w: %d", ErrUnknownMark, a.Mark)
		}
		return e.scale(r.Center()), nil
	}
	return e.scale(a.Point), nil
}

// scale converts a model-space point to screen space.
func (e *Executor) scale(p action.Point) action.Point { return p.Scale(e.sx, e.sy) }

// unscale converts a screen-space point back to model space.
func (e *Executor) unscale(p action.Point) action.Point {
	return p.Scale(1/e.sx, 1/e.sy)
}

func (e *Executor) keyEach(ctx context.Context, keys []string, fn func(context.Context, string) error) error {
	for _, k := range keys {
		if err := fn(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

// clicksFor returns the click count for a click-family action.
func clicksFor(a action.Action) int {
	switch a.Type {
	case action.DoubleClick:
		return 2
	case action.TripleClick:
		return 3
	default:
		if a.Clicks <= 0 {
			return 1
		}
		return a.Clicks
	}
}

// sleep waits for d, returning early if the context is cancelled.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
