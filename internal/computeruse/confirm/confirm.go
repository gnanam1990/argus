// Package confirm implements a computer-use confirmation middleware. It
// classifies every proposed action's risk with a policy.ActionClassifier and
// routes it accordingly: routine actions are allowed silently, risky actions
// are put to a middleware.Approver, and actions the classifier judges too
// risky to attempt at all are refused outright so the agent hands the task
// back to the user. It is named "confirm" (not "approval") to avoid colliding
// with the general-purpose internal/middleware.Approval gate.
package confirm

import (
	"context"
	"sync"

	"github.com/gnanam1990/argus/internal/computeruse/policy"
	"github.com/gnanam1990/argus/internal/middleware"
	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/agent"
)

// Confirmation is an agent.Middleware that gates actions behind a risk
// classification. It embeds agent.Base so every other Middleware hook is a
// no-op; only OnRunStart (to capture the task) and OnAction (the gate itself)
// are overridden.
type Confirmation struct {
	agent.Base

	classifier policy.ActionClassifier
	approver   middleware.Approver

	mu         sync.Mutex
	task       string
	lastReason string
	lastLevel  policy.RiskLevel
}

// denyApprover fails closed: it denies every action it is asked about. It is
// used when no real Approver is supplied, so a confirmation gate with nobody
// to ask never waves a risky action through.
type denyApprover struct{}

func (denyApprover) Approve(context.Context, action.Action) (bool, error) { return false, nil }

// NewConfirmation builds a confirmation middleware. A nil classifier uses
// policy.DefaultClassifier{}. A nil approver fails closed: any action that
// would otherwise require confirmation is denied.
func NewConfirmation(classifier policy.ActionClassifier, approver middleware.Approver) *Confirmation {
	if classifier == nil {
		classifier = policy.DefaultClassifier{}
	}
	if approver == nil {
		approver = denyApprover{}
	}
	return &Confirmation{classifier: classifier, approver: approver}
}

// OnRunStart captures the task string, which the classifier needs to judge
// intent (e.g. whether the task pre-approved a sensitive action).
func (c *Confirmation) OnRunStart(_ context.Context, task string) error {
	c.mu.Lock()
	c.task = task
	c.mu.Unlock()
	return nil
}

// OnAction classifies a and routes it:
//
//   - HandOff: denied outright, no approver consulted — the agent should not
//     attempt this action at all.
//   - AlwaysConfirm, PreApproval: routed to the Approver; its answer decides.
//   - NoConfirm: allowed without asking.
func (c *Confirmation) OnAction(ctx context.Context, a *action.Action) (bool, error) {
	c.mu.Lock()
	task := c.task
	c.mu.Unlock()

	level, reason := c.classifier.Classify(ctx, *a, task)

	c.mu.Lock()
	c.lastLevel = level
	c.lastReason = reason
	c.mu.Unlock()

	switch level {
	case policy.HandOff:
		return false, nil
	case policy.AlwaysConfirm, policy.PreApproval:
		return c.approver.Approve(ctx, *a)
	default: // policy.NoConfirm
		return true, nil
	}
}

// LastReason returns the reason string from the most recent classification,
// for observability and tests.
func (c *Confirmation) LastReason() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastReason
}

// LastLevel returns the RiskLevel from the most recent classification, for
// observability and tests.
func (c *Confirmation) LastLevel() policy.RiskLevel {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastLevel
}
