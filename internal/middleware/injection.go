package middleware

import (
	"context"
	"sync"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/agent"
)

// InjectionGuard defends against prompt injection from on-screen content. When
// an action's values derive from untrusted content (Action.Untrusted) AND the
// action is sensitive (a gated system/window capability), the guard flags it —
// and denies it outright in strict mode.
//
// This is the last line before execution: untrusted content that induces the
// model to run a command or touch a file is exactly the category-defining
// attack, so a sensitive+untrusted action never executes silently.
type InjectionGuard struct {
	agent.Base
	mu      sync.Mutex
	strict  bool
	flagged int
}

// NewInjectionGuard builds a guard. In strict mode, flagged actions are denied;
// otherwise they are counted and allowed (report-only).
func NewInjectionGuard(strict bool) *InjectionGuard {
	return &InjectionGuard{strict: strict}
}

func sensitive(t action.ActionType) bool { return t.Gated() }

// OnAction flags (and, in strict mode, denies) sensitive untrusted actions.
func (g *InjectionGuard) OnAction(_ context.Context, a *action.Action) (bool, error) {
	if a.Untrusted && sensitive(a.Type) {
		g.mu.Lock()
		g.flagged++
		g.mu.Unlock()
		if g.strict {
			return false, nil
		}
	}
	return true, nil
}

// Flagged returns how many actions the guard flagged.
func (g *InjectionGuard) Flagged() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.flagged
}
