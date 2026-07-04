// Package normalize maps each provider's raw computer-tool action vocabulary
// onto the single canonical action.Action union. Every model adapter feeds its
// raw tool-call input through here, so the agent loop and drivers only ever see
// canonical actions — the one place provider divergence is resolved.
//
// Repair is deliberately forgiving: a single malformed or unrecognized tool
// call is turned into a safe no-op (a screenshot re-observation) rather than
// aborting the whole run.
package normalize

import "github.com/gnanam1990/argus/pkg/action"

// Repair returns a safe stand-in action for a tool call that could not be
// normalized or validated. Re-observing (a screenshot) lets the loop feed a
// fresh frame back to the model and continue instead of crashing.
func Repair() action.Action {
	return action.Action{Type: action.Screenshot, Mark: action.NoMark}
}
