// Package normalize maps each provider's raw computer-tool action vocabulary
// onto the single canonical action.Action union. Every model adapter feeds its
// raw tool-call input through here, so the agent loop and drivers only ever see
// canonical actions — the one place provider divergence is resolved.
//
// Repair is deliberately forgiving: a single malformed or unrecognized tool
// call is turned into a safe no-op (a screenshot re-observation) rather than
// aborting the whole run.
package normalize

import (
	"math"

	"github.com/gnanam1990/argus/pkg/action"
)

// Repair returns a safe stand-in action for a tool call that could not be
// normalized or validated. Re-observing (a screenshot) lets the loop feed a
// fresh frame back to the model and continue instead of crashing.
func Repair() action.Action {
	return action.Action{Type: action.Screenshot, Mark: action.NoMark}
}

// roundCoord rounds a fractional pixel coordinate to the nearest integer.
// Models routinely emit fractional coordinates (e.g. "x":820.5); decoding
// straight into an int would either be a hard unmarshal error (silently
// repaired away into a no-op screenshot) or, with a lenient decoder, truncate
// toward zero and bias every such click up/left by up to a pixel. Both
// openai.go and anthropic.go funnel every coordinate-shaped field through
// this single rounding rule so the behavior is one decision, not a per-field
// judgment call.
func roundCoord(f float64) int {
	return int(math.Round(f))
}
