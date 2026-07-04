package agent_test

import (
	"context"
	"fmt"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/agent"
	compfake "github.com/gnanam1990/argus/pkg/computer/fake"
	"github.com/gnanam1990/argus/pkg/model"
	providerfake "github.com/gnanam1990/argus/pkg/model/fake"
)

// Example drives the agent loop with fake seams: the provider first asks for a
// screenshot, then finishes. No display, network, or model is involved.
func Example() {
	provider := providerfake.New(
		model.ActionTurn(model.Usage{}, action.Action{Type: action.Screenshot, Mark: action.NoMark}),
		model.EndTurn("task complete", model.Usage{}),
	)
	comp := compfake.New()

	runner := agent.NewRunner(provider, comp, agent.WithMaxSteps(10))
	outcome, err := runner.Run(context.Background(), "take a screenshot")
	if err != nil {
		panic(err)
	}

	fmt.Println(outcome.Reason, outcome.Steps, outcome.FinalText)
	// Output: completed 2 task complete
}
