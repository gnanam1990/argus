package middleware

import (
	"context"
	"log/slog"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/agent"
	"github.com/gnanam1990/argus/pkg/model"
)

// Telemetry logs loop events via slog, tagged with a run ID for correlation.
type Telemetry struct {
	agent.Base
	log   *slog.Logger
	runID string
}

// NewTelemetry builds a telemetry middleware. A nil logger uses slog.Default.
func NewTelemetry(log *slog.Logger, runID string) *Telemetry {
	if log == nil {
		log = slog.Default()
	}
	return &Telemetry{log: log, runID: runID}
}

func (t *Telemetry) with() *slog.Logger { return t.log.With("run_id", t.runID) }

// OnRunStart logs the task.
func (t *Telemetry) OnRunStart(_ context.Context, task string) error {
	t.with().Info("run.start", "task", task)
	return nil
}

// OnUsage logs token usage per turn.
func (t *Telemetry) OnUsage(_ context.Context, u model.Usage) error {
	t.with().Info("run.usage", "input", u.InputTokens, "output", u.OutputTokens)
	return nil
}

// OnAction logs the action about to run and always allows it.
func (t *Telemetry) OnAction(_ context.Context, a *action.Action) (bool, error) {
	t.with().Info("run.action", "type", a.Type.String(), "untrusted", a.Untrusted)
	return true, nil
}
