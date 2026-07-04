// Package agent implements the observe→think→act loop at the center of Argus.
// The Runner drives a task to completion against the model, driver, and
// (optional) grounder seams, while every cross-cutting concern — budget,
// human-in-the-loop approval, prompt-injection defense, secret redaction,
// image retention, telemetry, trajectory recording — is expressed as pluggable
// Middleware rather than loop code.
package agent

import (
	"context"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/model"
)

// Session runs a single task and exposes the resulting conversation.
type Session interface {
	Run(ctx context.Context, task string) (*Outcome, error)
	History() *model.Conversation
}

// State is the mutable run state middleware inspects at continuation checkpoints.
type State struct {
	Task  string
	Step  int         // completed steps
	Usage model.Usage // cumulative token usage
}

// Outcome summarizes a completed run.
type Outcome struct {
	Task      string
	Steps     int
	Usage     model.Usage
	Reason    string // completed | terminated | max_steps | halted | error
	FinalText string // last assistant text
}

// Reason constants for Outcome.Reason. Whenever Run returns a non-nil error the
// outcome's Reason is ReasonError, so callers can trust Reason on every path.
const (
	ReasonCompleted  = "completed"
	ReasonTerminated = "terminated"
	ReasonMaxSteps   = "max_steps"
	ReasonHalted     = "halted"
	ReasonError      = "error"
)

// Middleware hooks into the loop. Implementations embed Base and override only
// the hooks they need. Hooks fire in registration order. Mutating hooks
// (OnLLMStart, OnLLMEnd, OnAction) operate in place; gate hooks (OnRunContinue,
// OnAction) return false to stop or skip.
type Middleware interface {
	// OnRunStart fires once before the loop begins.
	OnRunStart(ctx context.Context, task string) error
	// OnRunContinue is the continuation gate, checked before each step. Return
	// false to stop the run (e.g. budget or step-limit exhausted).
	OnRunContinue(ctx context.Context, st *State) (bool, error)
	// OnLLMStart may transform the conversation in place before it is sent to
	// the provider (trim, compact, redact).
	OnLLMStart(ctx context.Context, conv *model.Conversation) error
	// OnLLMEnd may inspect or adjust the provider turn in place.
	OnLLMEnd(ctx context.Context, turn *model.Turn) error
	// OnAction is the approval gate, fired before an action executes. Return
	// false to skip the action (deny). The action may be mutated in place.
	OnAction(ctx context.Context, a *action.Action) (bool, error)
	// OnActionResult fires after an action executes.
	OnActionResult(ctx context.Context, a action.Action, r action.Result) error
	// OnScreenshot fires for every captured observation.
	OnScreenshot(ctx context.Context, img action.Image) error
	// OnUsage fires with the usage of each provider turn.
	OnUsage(ctx context.Context, u model.Usage) error
}

// Base is a no-op Middleware. Embed it and override the hooks you need.
type Base struct{}

// OnRunStart implements Middleware.
func (Base) OnRunStart(context.Context, string) error { return nil }

// OnRunContinue implements Middleware.
func (Base) OnRunContinue(context.Context, *State) (bool, error) { return true, nil }

// OnLLMStart implements Middleware.
func (Base) OnLLMStart(context.Context, *model.Conversation) error { return nil }

// OnLLMEnd implements Middleware.
func (Base) OnLLMEnd(context.Context, *model.Turn) error { return nil }

// OnAction implements Middleware.
func (Base) OnAction(context.Context, *action.Action) (bool, error) { return true, nil }

// OnActionResult implements Middleware.
func (Base) OnActionResult(context.Context, action.Action, action.Result) error { return nil }

// OnScreenshot implements Middleware.
func (Base) OnScreenshot(context.Context, action.Image) error { return nil }

// OnUsage implements Middleware.
func (Base) OnUsage(context.Context, model.Usage) error { return nil }
