package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/gnanam1990/argus/internal/app"
	"github.com/gnanam1990/argus/internal/config"
	"github.com/gnanam1990/argus/internal/pricing"
	"github.com/gnanam1990/argus/internal/tui"
	"github.com/gnanam1990/argus/pkg/agent"
	"github.com/gnanam1990/argus/pkg/computer"
	"github.com/gnanam1990/argus/pkg/grounder"
	"github.com/gnanam1990/argus/pkg/model"
	"github.com/gnanam1990/argus/pkg/trajectory"
)

// runTUI drives a task through the interactive Bubble Tea view. The agent loop
// runs in a goroutine while the program owns the main goroutine; loop events
// reach the display through the TUI middleware, and risky-action approvals are
// answered inline. Quitting the view (q / ctrl-c) cancels the run.
func runTUI(
	parent context.Context,
	cfg config.Config,
	prov model.Provider,
	comp computer.Computer,
	gr grounder.Grounder,
	marker grounder.Marker,
	rec trajectory.Recorder,
	secrets []string,
	runID, task string,
	out io.Writer,
) error {
	runCtx, cancel := context.WithCancel(parent)
	defer cancel()

	m := tui.NewModel(task, cfg.Provider.Kind, cfg.Provider.Model, cancel)
	prog := tui.NewProgram(m)
	mask := maskFunc(secrets)

	// Approvals prompt inside the TUI; the display middleware renders the loop.
	// Everything displayed passes through the secret mask.
	mw := app.BuildMiddleware(cfg, secrets, discardLogger(), runID, tui.MaskedApprover(prog, mask))
	display := tui.NewMiddleware(prog, cfg.Provider.Kind, cfg.Provider.Model)
	display.SetMask(mask)
	mw = append(mw, display)
	r := app.NewRunner(cfg, prov, comp, gr, marker, rec, mw)

	var (
		outcome *agent.Outcome
		runErr  error
		done    = make(chan struct{})
	)
	go func() {
		defer close(done)
		outcome, runErr = r.Run(runCtx, task)
		dm := tui.DoneMsg{}
		if outcome != nil {
			dm.Reason, dm.Steps, dm.FinalText = outcome.Reason, outcome.Steps, mask(outcome.FinalText)
		}
		if runErr != nil {
			if dm.Reason == "" {
				dm.Reason = agent.ReasonError
			}
			dm.Err = mask(runErr.Error())
		}
		prog.Send(dm)
	}()

	_, perr := prog.Run()
	cancel() // stop the loop if the user quit early
	<-done   // wait for the goroutine so outcome/runErr are safe to read
	if perr != nil {
		return perr
	}
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		return runErr
	}

	// The alternate screen is restored; print a final summary to the terminal.
	if outcome != nil {
		fmt.Fprintf(out, "outcome: %s in %d steps\n", outcome.Reason, outcome.Steps)
		if outcome.FinalText != "" {
			fmt.Fprintln(out, mask(outcome.FinalText))
		}
		if cost, ok := pricing.Cost(cfg.Provider.Model, outcome.Usage); ok {
			fmt.Fprintf(out, "cost: $%.4f (in %d / out %d tokens)\n",
				cost, outcome.Usage.InputTokens, outcome.Usage.OutputTokens)
		}
	}
	return nil
}
