// Package capture implements the computer-use "observe" step: an async
// worker that assembles a state.AppState for one application by driving, in
// order, the permission/lock precondition gate, the per-app approval store,
// bringing the app to the foreground, taking a screenshot, walking its
// accessibility tree, and loading any per-app instructions. Every dependency
// is an injected interface so the pipeline can be exercised with fakes and no
// real OS call, subprocess, network, or filesystem access.
//
// Start returns a channel of Update values rather than a single result
// because the pipeline can legitimately take a while (e.g. waiting for the
// user to unlock the screen or grant a permission) — UpdatePending lets a
// caller show progress instead of blocking silently. Provider adapts that
// channel-based Worker into the synchronous state.StateProvider the rest of
// the computer-use subsystem (and the MCP server) consumes.
package capture

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/gnanam1990/argus/internal/computeruse/approval"
	"github.com/gnanam1990/argus/internal/computeruse/grounding"
	"github.com/gnanam1990/argus/internal/computeruse/instructions"
	"github.com/gnanam1990/argus/internal/computeruse/permissions"
	"github.com/gnanam1990/argus/internal/computeruse/state"
	"github.com/gnanam1990/argus/pkg/action"
)

// AnimationTarget describes the on-screen chrome a client is animating
// toward while a capture is in flight (e.g. a highlight overlay drawn around
// the app's window). The capture worker does not interpret it — it is
// carried on Request purely so a caller can correlate a Request with the
// visual state it kicked off; nothing in this package reads its fields.
type AnimationTarget struct {
	// BackgroundColor is the overlay's fill, as a CSS-style color string.
	BackgroundColor string
	// CornerRadius is the overlay's corner radius, in points.
	CornerRadius float64
	// PrimaryTextColor is the overlay's label color, as a CSS-style color
	// string.
	PrimaryTextColor string
	// ViewportFrame is the screen region the overlay occupies.
	ViewportFrame state.Rect
}

// Request describes one capture: which app to observe, and (optionally) the
// animation the caller is presenting while it waits.
type Request struct {
	// RequestID identifies this capture; it is echoed back on every Update
	// so a caller driving multiple concurrent captures can tell them apart.
	RequestID string
	// BundleIdentifier is the app to observe. It must be non-empty.
	BundleIdentifier string
	// AnimationTarget is opaque to this package; see its doc comment.
	AnimationTarget AnimationTarget
}

// UpdateType classifies an Update.
type UpdateType string

const (
	// UpdateCompleted means State holds the assembled observation.
	UpdateCompleted UpdateType = "completed"
	// UpdateFailed means the capture stopped with Error and will not
	// produce a State; no further Updates follow on the channel.
	UpdateFailed UpdateType = "failed"
	// UpdatePending means a precondition (permission grant, screen unlock)
	// is not yet satisfied but the worker is still retrying; more Updates
	// follow.
	UpdatePending UpdateType = "pending"
)

// Update is one message on a Worker's channel. Exactly one Update of type
// UpdateCompleted or UpdateFailed is ever sent per Request, and it is always
// the last thing sent before the channel closes; any number (including
// zero) of UpdatePending updates may precede it.
type Update struct {
	// RequestID echoes the originating Request.RequestID.
	RequestID string
	// Type classifies this Update.
	Type UpdateType
	// Error is set only when Type is UpdateFailed.
	Error string
	// State is set only when Type is UpdateCompleted.
	State state.AppState
}

// Worker runs a capture asynchronously.
type Worker interface {
	// Start begins assembling the AppState described by req and returns a
	// channel of progress/result Updates. The channel is closed after the
	// single terminal Update (UpdateCompleted or UpdateFailed) is sent.
	// Start itself returns an error only for a request that is invalid
	// before any work begins (e.g. a missing BundleIdentifier); once
	// Start returns a channel successfully, every failure surfaces as an
	// UpdateFailed on that channel instead.
	Start(ctx context.Context, req Request) (<-chan Update, error)
}

// Focuser brings an application to the foreground so the accessibility walk
// and screenshot observe the app the caller asked about.
type Focuser interface {
	// Focus activates the app identified by bundleID.
	Focus(ctx context.Context, bundleID string) error
}

// Screenshotter captures the current screen as an encoded image.
// pkg/computer.Computer satisfies this interface, so a real computer.Computer
// can be passed directly wherever a Screenshotter is required.
type Screenshotter interface {
	// Screenshot captures the current screen.
	Screenshot(ctx context.Context) (action.Image, error)
}

// updateBuffer is the Update channel's buffer size: large enough that a
// handful of UpdatePending retries can queue up without the worker
// goroutine blocking on a caller that reads updates in a loop (the normal
// case), while still small enough that an abandoned channel (a caller that
// starts a capture and never reads it) doesn't grow unbounded.
const updateBuffer = 8

const (
	// defaultTimeout bounds how long Start will keep retrying a pending
	// permission/lock precondition before giving up.
	defaultTimeout = 120 * time.Second
	// defaultRetryInterval is how long Start waits between successive
	// permissions.Orchestrator.Ensure retries while the precondition is
	// pending.
	defaultRetryInterval = 2 * time.Second
)

// Option configures a DefaultWorker.
type Option func(*DefaultWorker)

// WithTimeout overrides how long Start retries a pending precondition
// before failing (default 120s).
func WithTimeout(d time.Duration) Option {
	return func(w *DefaultWorker) { w.timeout = d }
}

// WithRetryInterval overrides the delay between precondition retries
// (default 2s).
func WithRetryInterval(d time.Duration) Option {
	return func(w *DefaultWorker) { w.retryInterval = d }
}

// WithClock overrides how the worker reads the current time, for
// deterministic timeout tests. The default is time.Now.
func WithClock(now func() time.Time) Option {
	return func(w *DefaultWorker) { w.now = now }
}

// WithSleep overrides how the worker waits between retries, for
// deterministic tests that don't want to block on a real timer. The
// injected function must honor ctx cancellation (returning ctx.Err() if
// canceled before d elapses) the way the default implementation does.
func WithSleep(sleep func(ctx context.Context, d time.Duration) error) Option {
	return func(w *DefaultWorker) { w.sleep = sleep }
}

// DefaultWorker is the Worker implementation: it drives the seven-step
// pipeline described in the package doc against injected dependencies.
type DefaultWorker struct {
	orch     permissions.Orchestrator
	store    approval.Store
	focuser  Focuser
	provider grounding.Provider
	loader   instructions.Loader
	shot     Screenshotter

	timeout       time.Duration
	retryInterval time.Duration
	now           func() time.Time
	sleep         func(ctx context.Context, d time.Duration) error
}

var _ Worker = (*DefaultWorker)(nil)

// NewDefaultWorker builds a DefaultWorker from its dependencies. All six are
// required (non-nil); opts may override the retry timeout/interval and, for
// tests, the clock/sleep used to evaluate them.
func NewDefaultWorker(
	orch permissions.Orchestrator,
	store approval.Store,
	focuser Focuser,
	provider grounding.Provider,
	loader instructions.Loader,
	shot Screenshotter,
	opts ...Option,
) *DefaultWorker {
	w := &DefaultWorker{
		orch:          orch,
		store:         store,
		focuser:       focuser,
		provider:      provider,
		loader:        loader,
		shot:          shot,
		timeout:       defaultTimeout,
		retryInterval: defaultRetryInterval,
		now:           time.Now,
		sleep:         defaultSleep,
	}
	for _, o := range opts {
		o(w)
	}
	return w
}

// defaultSleep waits for d, or returns ctx.Err() early if ctx is canceled
// first.
func defaultSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
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

// Start validates req and, if valid, launches the capture pipeline in a
// goroutine, returning immediately with a channel of Updates.
func (w *DefaultWorker) Start(ctx context.Context, req Request) (<-chan Update, error) {
	if req.BundleIdentifier == "" {
		return nil, errors.New("capture: request has no BundleIdentifier")
	}

	ch := make(chan Update, updateBuffer)
	go w.run(ctx, req, ch)
	return ch, nil
}

// run executes the pipeline and always closes ch after sending exactly one
// terminal Update.
func (w *DefaultWorker) run(ctx context.Context, req Request, ch chan<- Update) {
	defer close(ch)

	if !w.awaitPreconditions(ctx, req, ch) {
		return
	}

	decision, err := w.store.Get(ctx, req.BundleIdentifier)
	if err != nil {
		w.fail(ctx, req, ch, fmt.Errorf("capture: check approval for %s: %w", req.BundleIdentifier, err))
		return
	}
	if decision != approval.Approved {
		w.failMsg(ctx, req, ch, fmt.Sprintf(
			"Computer Use is not allowed to use the app '%s'. Ask the user for approval.",
			req.BundleIdentifier))
		return
	}

	if ctx.Err() != nil {
		w.fail(ctx, req, ch, ctx.Err())
		return
	}
	if err := w.focuser.Focus(ctx, req.BundleIdentifier); err != nil {
		w.fail(ctx, req, ch, fmt.Errorf("capture: focus %s: %w", req.BundleIdentifier, err))
		return
	}

	if ctx.Err() != nil {
		w.fail(ctx, req, ch, ctx.Err())
		return
	}
	shot, err := w.shot.Screenshot(ctx)
	if err != nil {
		w.fail(ctx, req, ch, fmt.Errorf("capture: screenshot: %w", err))
		return
	}

	if ctx.Err() != nil {
		w.fail(ctx, req, ch, ctx.Err())
		return
	}
	root, err := w.provider.FrontmostTree(ctx, req.BundleIdentifier)
	if err != nil {
		w.fail(ctx, req, ch, fmt.Errorf("capture: read accessibility tree for %s: %w", req.BundleIdentifier, err))
		return
	}

	if ctx.Err() != nil {
		w.fail(ctx, req, ch, ctx.Err())
		return
	}
	instr, err := w.loader.Load(ctx, req.BundleIdentifier)
	if err != nil {
		w.fail(ctx, req, ch, fmt.Errorf("capture: load instructions for %s: %w", req.BundleIdentifier, err))
		return
	}

	elements := root.Children
	if len(elements) == 0 {
		elements = []state.Element{root}
	}

	st := state.AppState{
		BundleIdentifier: req.BundleIdentifier,
		WindowTitle:      root.Label,
		WindowFrame:      root.Frame,
		Elements:         elements,
		Screenshot:       shot,
		Instruction:      instr.Markdown,
	}
	w.send(ctx, ch, Update{RequestID: req.RequestID, Type: UpdateCompleted, State: st})
}

// awaitPreconditions calls permissions.Orchestrator.Ensure, retrying while it
// reports ErrPending (emitting an UpdatePending on every attempt) until
// either it succeeds (true), the overall timeout elapses, ctx is canceled,
// or a non-pending error is returned — each of the latter three sends a
// terminal UpdateFailed and returns false.
func (w *DefaultWorker) awaitPreconditions(ctx context.Context, req Request, ch chan<- Update) bool {
	deadline := w.now().Add(w.timeout)

	for {
		err := w.orch.Ensure(ctx)
		if err == nil {
			return true
		}

		if !errors.Is(err, permissions.ErrPending) {
			w.fail(ctx, req, ch, err)
			return false
		}

		if !w.send(ctx, ch, Update{RequestID: req.RequestID, Type: UpdatePending}) {
			return false
		}

		remaining := deadline.Sub(w.now())
		if remaining <= 0 {
			w.failMsg(ctx, req, ch, fmt.Sprintf(
				"capture: timed out after %s waiting for permissions/screen unlock for %s",
				w.timeout, req.BundleIdentifier))
			return false
		}

		interval := w.retryInterval
		if remaining < interval {
			interval = remaining
		}
		if err := w.sleep(ctx, interval); err != nil {
			w.fail(ctx, req, ch, err)
			return false
		}
	}
}

// fail sends a terminal UpdateFailed built from err.
func (w *DefaultWorker) fail(ctx context.Context, req Request, ch chan<- Update, err error) {
	w.failMsg(ctx, req, ch, err.Error())
}

// failMsg sends a terminal UpdateFailed carrying msg verbatim.
func (w *DefaultWorker) failMsg(ctx context.Context, req Request, ch chan<- Update, msg string) {
	w.send(ctx, ch, Update{RequestID: req.RequestID, Type: UpdateFailed, Error: msg})
}

// send delivers upd on ch, preferring the send whenever ch has buffer room
// (so a terminal Update is never dropped just because ctx happened to be
// canceled in the same instant) and only falling back to racing against ctx
// cancellation once the buffer is actually full, so the caller can still
// stop the pipeline without blocking forever on a channel no one is
// reading.
func (w *DefaultWorker) send(ctx context.Context, ch chan<- Update, upd Update) bool {
	select {
	case ch <- upd:
		return true
	default:
	}
	select {
	case ch <- upd:
		return true
	case <-ctx.Done():
		return false
	}
}

// AppLister enumerates the apps a capture could target.
type AppLister interface {
	// ListApps returns the currently known/running apps.
	ListApps(ctx context.Context) ([]state.AppInfo, error)
}

// Provider adapts a Worker (and an AppLister) into state.StateProvider, the
// synchronous shape the rest of the computer-use subsystem consumes: it
// starts a capture and blocks until the Worker reports a terminal Update.
type Provider struct {
	worker Worker
	lister AppLister

	nextID atomic.Uint64
}

var _ state.StateProvider = (*Provider)(nil)

// NewProvider builds a Provider backed by w and lister.
func NewProvider(w Worker, lister AppLister) *Provider {
	return &Provider{worker: w, lister: lister}
}

// GetAppState starts a capture for bundleID and blocks until the worker
// reports a terminal Update: on UpdateCompleted it returns the assembled
// AppState; on UpdateFailed it returns an error carrying the Update's Error
// text. UpdatePending updates are consumed silently (GetAppState has no way
// to report interim progress to its state.StateProvider caller); ctx
// cancellation aborts the wait immediately.
func (p *Provider) GetAppState(ctx context.Context, bundleID string) (state.AppState, error) {
	req := Request{
		RequestID:        fmt.Sprintf("capture-%d", p.nextID.Add(1)),
		BundleIdentifier: bundleID,
	}
	ch, err := p.worker.Start(ctx, req)
	if err != nil {
		return state.AppState{}, err
	}

	for {
		select {
		case <-ctx.Done():
			return state.AppState{}, ctx.Err()
		case upd, ok := <-ch:
			if !ok {
				return state.AppState{}, fmt.Errorf("capture: worker closed without a terminal update for %s", bundleID)
			}
			switch upd.Type {
			case UpdateCompleted:
				return upd.State, nil
			case UpdateFailed:
				return state.AppState{}, errors.New(upd.Error)
			case UpdatePending:
				// Keep waiting; nothing to report at this layer.
			}
		}
	}
}

// ListApps delegates to the injected AppLister.
func (p *Provider) ListApps(ctx context.Context) ([]state.AppInfo, error) {
	return p.lister.ListApps(ctx)
}
