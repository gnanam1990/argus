package middleware

import (
	"context"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/agent"
)

// RiskPolicy decides whether an action must be approved before it runs.
type RiskPolicy func(action.Action) bool

// DefaultRiskPolicy requires approval for gated (system/window) actions and for
// any action whose values derive from untrusted on-screen content.
func DefaultRiskPolicy(a action.Action) bool {
	return a.Type.Gated() || a.Untrusted
}

// Approver decides whether a risky action may proceed. Return false to deny.
type Approver interface {
	Approve(ctx context.Context, a action.Action) (bool, error)
}

// ApproverFunc adapts a function to Approver.
type ApproverFunc func(ctx context.Context, a action.Action) (bool, error)

// Approve implements Approver.
func (f ApproverFunc) Approve(ctx context.Context, a action.Action) (bool, error) { return f(ctx, a) }

// Approval gates risky actions behind an Approver. Actions the policy considers
// safe pass through automatically; risky ones route to the Approver.
type Approval struct {
	agent.Base
	policy   RiskPolicy
	approver Approver
}

// NewApproval builds an approval gate. A nil policy uses DefaultRiskPolicy. A
// nil approver fails closed: every risky action is denied (an approval gate
// with nobody to ask must never wave actions through or panic).
func NewApproval(policy RiskPolicy, approver Approver) *Approval {
	if policy == nil {
		policy = DefaultRiskPolicy
	}
	if approver == nil {
		approver = ApproverFunc(func(context.Context, action.Action) (bool, error) { return false, nil })
	}
	return &Approval{policy: policy, approver: approver}
}

// OnAction routes risky actions to the Approver.
func (a *Approval) OnAction(ctx context.Context, act *action.Action) (bool, error) {
	if !a.policy(*act) {
		return true, nil
	}
	return a.approver.Approve(ctx, *act)
}

// AllowList is an Approver that permits only the listed action types.
type AllowList map[action.ActionType]bool

// Approve implements Approver.
func (l AllowList) Approve(_ context.Context, a action.Action) (bool, error) {
	return l[a.Type], nil
}
