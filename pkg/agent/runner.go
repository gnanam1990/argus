package agent

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for screenshot sizing
	_ "image/png"  // register PNG decoder for screenshot sizing
	"time"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/computer"
	"github.com/gnanam1990/argus/pkg/grounder"
	"github.com/gnanam1990/argus/pkg/model"
	"github.com/gnanam1990/argus/pkg/trajectory"
)

// Runner is the default Session implementation.
type Runner struct {
	provider model.Provider
	computer computer.Computer
	exec     *computer.Executor

	grounder      grounder.Grounder
	marker        grounder.Marker
	minConfidence float64

	recorder trajectory.Recorder
	mw       []Middleware
	stepOpts []model.StepOption

	system   string
	maxSteps int

	settleDelay time.Duration
	preprocess  func(action.Image) (action.Image, error)

	conv     *model.Conversation
	observed bool // at least one screenshot has entered the conversation
}

// Option configures a Runner.
type Option func(*Runner)

// WithSystemPrompt sets the system prompt.
func WithSystemPrompt(s string) Option { return func(r *Runner) { r.system = s } }

// WithMaxSteps caps the number of steps (0 = unlimited; rely on middleware).
func WithMaxSteps(n int) Option { return func(r *Runner) { r.maxSteps = n } }

// WithGrounder installs a set-of-marks grounder and marker. Grounding is
// engaged only when the provider lacks native computer use.
func WithGrounder(g grounder.Grounder, m grounder.Marker, minConfidence float64) Option {
	return func(r *Runner) { r.grounder, r.marker, r.minConfidence = g, m, minConfidence }
}

// WithTrajectory sets the trajectory recorder (default: no-op).
func WithTrajectory(rec trajectory.Recorder) Option { return func(r *Runner) { r.recorder = rec } }

// WithMiddleware appends middleware to the ordered chain.
func WithMiddleware(mw ...Middleware) Option {
	return func(r *Runner) { r.mw = append(r.mw, mw...) }
}

// WithCapabilities enables gated action types on the executor allowlist.
// Repeated options accumulate.
func WithCapabilities(types ...action.ActionType) Option {
	return func(r *Runner) { r.exec.Allow(types...) }
}

// WithModelOptions sets per-step provider options (temperature/seed/max-tokens).
func WithModelOptions(opts ...model.StepOption) Option {
	return func(r *Runner) { r.stepOpts = append(r.stepOpts, opts...) }
}

// WithSettleDelay pauses after each action batch before re-observing, so the
// UI has time to react (a menu opening, a window appearing) and the next
// screenshot reflects the action's result rather than the pre-action screen.
func WithSettleDelay(d time.Duration) Option {
	return func(r *Runner) { r.settleDelay = d }
}

// WithBackgroundDispatch makes single left clicks prefer the driver's
// background (no-cursor) click when available, falling back to a cursor click.
func WithBackgroundDispatch() Option {
	return func(r *Runner) { r.exec.SetBackgroundDispatch(true) }
}

// WithScreenshotProcessor transforms each screenshot before it is sent to the
// model (e.g. downscaling to cut tokens/latency). The executor's coordinate
// scale is derived from the processed frame, so clicks still land correctly.
// The trajectory records the original, full-resolution capture.
func WithScreenshotProcessor(fn func(action.Image) (action.Image, error)) Option {
	return func(r *Runner) { r.preprocess = fn }
}

// NewRunner builds a Runner over the given provider and computer.
func NewRunner(provider model.Provider, comp computer.Computer, opts ...Option) *Runner {
	r := &Runner{
		provider: provider,
		computer: comp,
		exec:     computer.NewExecutor(comp),
		marker:   defaultMarker{},
		recorder: trajectory.NoOp{},
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// defaultMarker resolves marks to their box centers without drawing, so a
// grounder can be used before internal/mark ships the real overlay.
type defaultMarker struct{}

func (defaultMarker) Overlay(img action.Image, els []grounder.Element) (action.Image, map[int]action.Rect, error) {
	return img, grounder.Index(grounder.Renumber(els)), nil
}

// History returns the conversation from the most recent Run.
func (r *Runner) History() *model.Conversation { return r.conv }

// Run drives the task to completion.
func (r *Runner) Run(ctx context.Context, task string) (*Outcome, error) {
	r.conv = &model.Conversation{System: r.system}
	r.conv.AddUser(model.Text(task))
	r.observed = false

	st := &State{Task: task}
	out := &Outcome{Task: task}
	// fail stamps the outcome so callers can trust Reason on every error path.
	fail := func(err error) (*Outcome, error) {
		out.Reason = ReasonError
		return out, err
	}

	for _, mw := range r.mw {
		if err := mw.OnRunStart(ctx, task); err != nil {
			return fail(err)
		}
	}

	// Initial observation.
	obs, err := r.observe(ctx)
	if err != nil {
		return fail(fmt.Errorf("initial observe: %w", err))
	}

	for {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}

		cont, err := r.gate(ctx, st)
		if err != nil {
			return fail(err)
		}
		if !cont {
			out.Reason = ReasonHalted
			break
		}
		if r.maxSteps > 0 && st.Step >= r.maxSteps {
			out.Reason = ReasonMaxSteps
			break
		}

		// Pre-LLM middleware transforms the request conversation.
		for _, mw := range r.mw {
			if err := mw.OnLLMStart(ctx, r.conv); err != nil {
				return fail(err)
			}
		}

		turn, err := r.provider.Step(ctx, r.conv, r.stepOpts...)
		if err != nil {
			return fail(fmt.Errorf("provider step: %w", err))
		}

		// The provider call happened: account for it before any hook can abort,
		// so error paths still report truthful step and usage totals.
		st.Usage = st.Usage.Add(turn.Usage)
		st.Step++
		out.Steps = st.Step
		out.Usage = st.Usage

		for _, mw := range r.mw {
			if err := mw.OnLLMEnd(ctx, turn); err != nil {
				return fail(err)
			}
		}

		for _, mw := range r.mw {
			if err := mw.OnUsage(ctx, turn.Usage); err != nil {
				return fail(err)
			}
		}

		r.conv.Add(turn.Message)
		if t := turn.Text(); t != "" {
			out.FinalText = t
		}

		step := trajectory.Step{
			Index:      st.Step - 1,
			Screenshot: obs,
			Text:       turn.Text(),
			Usage:      turn.Usage,
		}

		if !turn.HasActions() {
			out.Reason = ReasonCompleted
			// The completing turn IS the final word, even when empty — never
			// leave a stale earlier step's text as FinalText.
			out.FinalText = turn.Text()
			if err := r.recorder.Append(step); err != nil {
				return fail(err)
			}
			break
		}

		terminated, toolMsg, err := r.act(ctx, turn, &step)
		if err != nil {
			// Keep the evidence: actions before the failure already ran.
			_ = r.recorder.Append(step)
			return fail(err)
		}
		if err := r.recorder.Append(step); err != nil {
			return fail(err)
		}

		// Always pair the turn's tool uses with their results — including for a
		// terminating batch, so history never ends on a dangling tool call.
		r.conv.Add(toolMsg)

		if terminated {
			out.Reason = ReasonTerminated
			break
		}

		// Let the action's effect render, then re-observe for the next step.
		if err := r.settle(ctx); err != nil {
			return fail(err)
		}
		obs, err = r.observe(ctx)
		if err != nil {
			return fail(fmt.Errorf("observe: %w", err))
		}
	}

	return out, nil
}

// gate runs the continuation middleware; any false or error stops the run.
func (r *Runner) gate(ctx context.Context, st *State) (bool, error) {
	for _, mw := range r.mw {
		cont, err := mw.OnRunContinue(ctx, st)
		if err != nil {
			return false, err
		}
		if !cont {
			return false, nil
		}
	}
	return true, nil
}

// act executes the turn's requested actions, returning whether the run
// terminated and the tool message to append with the results.
func (r *Runner) act(ctx context.Context, turn *model.Turn, step *trajectory.Step) (bool, model.Message, error) {
	var contents []model.Content
	terminated := false

	for _, use := range turn.ActionUses() {
		a := use.Action

		// Post-observation actions are conservatively untrusted: their values
		// may derive from attacker-controlled on-screen content, and provenance
		// cannot be proven. Sensitive-action policy (the injection guard and the
		// default approval risk policy) keys on this flag.
		if r.observed {
			a.Untrusted = true
		}

		proceed := true
		for _, mw := range r.mw {
			ok, err := mw.OnAction(ctx, &a)
			if err != nil {
				return false, model.Message{}, err
			}
			if !ok {
				proceed = false
				break
			}
		}

		var res action.Result
		if proceed {
			var err error
			res, err = r.exec.Execute(ctx, a)
			if err != nil {
				return false, model.Message{}, fmt.Errorf("execute %s: %w", a.Type, err)
			}
			for _, mw := range r.mw {
				if err := mw.OnActionResult(ctx, a, res); err != nil {
					return false, model.Message{}, err
				}
			}
		} else {
			// Make denials visible to the model instead of an empty result.
			res = action.Result{Output: "action denied by policy"}
		}

		step.Actions = append(step.Actions, a)
		step.Results = append(step.Results, res)
		contents = append(contents, model.ActionResult(use.CallID, res))
		if res.Terminated {
			// Nothing after a terminate in the same batch may execute.
			terminated = true
			break
		}
	}

	return terminated, model.ToolMessage(contents...), nil
}

// observe captures a screenshot, decides the frame to send the model (marked
// for grounding, or a processed/downscaled copy otherwise), updates the
// executor's scale from THAT frame, fires OnScreenshot, and appends the
// observation to the conversation. It returns the raw, full-resolution capture
// for the trajectory record.
func (r *Runner) observe(ctx context.Context) (action.Image, error) {
	img, err := r.computer.Screenshot(ctx)
	if err != nil {
		return action.Image{}, err
	}

	sw, sh, err := r.computer.ScreenSize(ctx)
	if err != nil {
		return action.Image{}, fmt.Errorf("screen size: %w", err)
	}

	// Decide the frame the model actually sees. Grounding overlays marks on the
	// full-resolution capture (the mark index is in that space). Otherwise an
	// optional processor may downscale it — vision models internally downsample
	// large screenshots anyway, so capping resolution trims tokens and latency.
	sent := img
	if r.grounder != nil && !r.provider.Capabilities().NativeComputerUse {
		els, derr := r.grounder.Detect(ctx, img)
		if derr != nil {
			return action.Image{}, fmt.Errorf("ground: %w", derr)
		}
		els = grounder.Filter(els, r.minConfidence)
		m, idx, merr := r.marker.Overlay(img, els)
		if merr != nil {
			return action.Image{}, fmt.Errorf("mark: %w", merr)
		}
		sent = m
		r.exec.SetMarks(idx)
	} else if r.preprocess != nil {
		p, perr := r.preprocess(img)
		if perr != nil {
			return action.Image{}, fmt.Errorf("screenshot preprocess: %w", perr)
		}
		sent = p
	}

	// Scale ownership: map the model's screenshot-space coordinates to screen
	// space using the dimensions of the frame the model actually saw, so
	// downscaling doesn't shift where clicks land. Both failure modes stay
	// loud — a silently stale or identity scale is exactly how an unexpected
	// resolution becomes a systematic misclick while the run reports success
	// (H6).
	if iw, ih, ok := decodeSize(sent); ok {
		if iw > 0 && ih > 0 {
			r.exec.SetScale(float64(sw)/float64(iw), float64(sh)/float64(ih))
		}
	} else if len(sent.Data) > 0 {
		return action.Image{}, fmt.Errorf("screenshot undecodable: cannot compute scale")
	}

	for _, mw := range r.mw {
		if err := mw.OnScreenshot(ctx, img); err != nil {
			return action.Image{}, err
		}
	}

	r.conv.AddUser(model.ImageContent(sent))
	r.observed = true
	return img, nil
}

// settle waits settleDelay (if set), honoring cancellation, so an action's
// on-screen effect can render before the next observation.
func (r *Runner) settle(ctx context.Context) error {
	if r.settleDelay <= 0 {
		return nil
	}
	t := time.NewTimer(r.settleDelay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// decodeSize reads an encoded image's pixel dimensions without a full decode.
func decodeSize(img action.Image) (w, h int, ok bool) {
	if len(img.Data) == 0 {
		return 0, 0, false
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(img.Data))
	if err != nil {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}

// Compile-time check that Runner is a Session.
var _ Session = (*Runner)(nil)
