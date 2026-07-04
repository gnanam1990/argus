package agent

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for screenshot sizing
	_ "image/png"  // register PNG decoder for screenshot sizing

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

	conv *model.Conversation
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
func WithCapabilities(types ...action.ActionType) Option {
	return func(r *Runner) {
		r.exec = computer.NewExecutor(r.computer, computer.WithCapabilities(types...))
	}
}

// WithModelOptions sets per-step provider options (temperature/seed/max-tokens).
func WithModelOptions(opts ...model.StepOption) Option {
	return func(r *Runner) { r.stepOpts = append(r.stepOpts, opts...) }
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
	return img, grounder.Index(els), nil
}

// History returns the conversation from the most recent Run.
func (r *Runner) History() *model.Conversation { return r.conv }

// Run drives the task to completion.
func (r *Runner) Run(ctx context.Context, task string) (*Outcome, error) {
	r.conv = &model.Conversation{System: r.system}
	r.conv.AddUser(model.Text(task))

	st := &State{Task: task}
	out := &Outcome{Task: task}

	for _, mw := range r.mw {
		if err := mw.OnRunStart(ctx, task); err != nil {
			return out, err
		}
	}

	// Initial observation.
	obs, err := r.observe(ctx)
	if err != nil {
		return out, fmt.Errorf("initial observe: %w", err)
	}

	for {
		if err := ctx.Err(); err != nil {
			return out, err
		}

		cont, err := r.gate(ctx, st)
		if err != nil {
			return out, err
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
				return out, err
			}
		}

		turn, err := r.provider.Step(ctx, r.conv, r.stepOpts...)
		if err != nil {
			out.Reason = "error"
			return out, fmt.Errorf("provider step: %w", err)
		}

		for _, mw := range r.mw {
			if err := mw.OnLLMEnd(ctx, turn); err != nil {
				return out, err
			}
		}

		st.Usage = st.Usage.Add(turn.Usage)
		for _, mw := range r.mw {
			if err := mw.OnUsage(ctx, turn.Usage); err != nil {
				return out, err
			}
		}

		r.conv.Add(turn.Message)
		st.Step++
		out.Steps = st.Step
		out.Usage = st.Usage
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
			if err := r.recorder.Append(step); err != nil {
				return out, err
			}
			break
		}

		terminated, toolMsg, err := r.act(ctx, turn, &step)
		if err != nil {
			return out, err
		}
		if err := r.recorder.Append(step); err != nil {
			return out, err
		}
		if terminated {
			out.Reason = ReasonTerminated
			break
		}

		r.conv.Add(toolMsg)

		// Re-observe for the next step.
		obs, err = r.observe(ctx)
		if err != nil {
			return out, fmt.Errorf("observe: %w", err)
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
		}

		step.Actions = append(step.Actions, a)
		step.Results = append(step.Results, res)
		contents = append(contents, model.ActionResult(use.CallID, res))
		if res.Terminated {
			terminated = true
		}
	}

	return terminated, model.ToolMessage(contents...), nil
}

// observe captures a screenshot, updates the executor's scale factors and (when
// grounding is engaged) its set-of-marks index, fires OnScreenshot, and appends
// the observation to the conversation. It returns the raw screenshot for the
// trajectory record.
func (r *Runner) observe(ctx context.Context) (action.Image, error) {
	img, err := r.computer.Screenshot(ctx)
	if err != nil {
		return action.Image{}, err
	}

	// Scale ownership lives here: map model/screenshot space to screen space.
	if sw, sh, serr := r.computer.ScreenSize(ctx); serr == nil {
		if iw, ih, ok := decodeSize(img); ok && iw > 0 && ih > 0 {
			r.exec.SetScale(float64(sw)/float64(iw), float64(sh)/float64(ih))
		}
	}

	marked := img
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
		marked = m
		r.exec.SetMarks(idx)
	}

	for _, mw := range r.mw {
		if err := mw.OnScreenshot(ctx, img); err != nil {
			return action.Image{}, err
		}
	}

	r.conv.AddUser(model.ImageContent(marked))
	return img, nil
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
