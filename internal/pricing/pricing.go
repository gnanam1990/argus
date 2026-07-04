// Package pricing maps model IDs to token→USD rates so the budget middleware
// and the end-of-run cost summary can price a run.
//
// Rates are pinned constants (USD per million tokens) verified against provider
// pricing as of 2026-07. They MUST be re-confirmed before a release (a stale
// rate silently mis-prices a run); the release checklist owns that gate.
package pricing

import "github.com/gnanam1990/argus/pkg/model"

// Cache-token pricing is expressed as a multiple of the base input rate:
// reads are ~0.1x, and 5-minute-TTL writes are ~1.25x.
const (
	cacheReadMultiplier  = 0.10
	cacheWriteMultiplier = 1.25
)

// Rate is per-million-token USD pricing for a model.
type Rate struct {
	InputPerMTok  float64
	OutputPerMTok float64
}

// rates is the pinned rate table. Update alongside a model launch.
var rates = map[string]Rate{
	"claude-fable-5":    {InputPerMTok: 10, OutputPerMTok: 50},
	"claude-opus-4-8":   {InputPerMTok: 5, OutputPerMTok: 25},
	"claude-opus-4-7":   {InputPerMTok: 5, OutputPerMTok: 25},
	"claude-opus-4-6":   {InputPerMTok: 5, OutputPerMTok: 25},
	"claude-sonnet-5":   {InputPerMTok: 3, OutputPerMTok: 15},
	"claude-sonnet-4-6": {InputPerMTok: 3, OutputPerMTok: 15},
	"claude-haiku-4-5":  {InputPerMTok: 1, OutputPerMTok: 5},
}

// Lookup returns the rate for a model ID, and false if it is unknown.
func Lookup(modelID string) (Rate, bool) {
	r, ok := rates[modelID]
	return r, ok
}

// Cost returns the USD cost of u under modelID's rate, and false if the model
// has no known rate (callers should fall back to a token budget).
func Cost(modelID string, u model.Usage) (float64, bool) {
	r, ok := rates[modelID]
	if !ok {
		return 0, false
	}
	const perTok = 1.0 / 1_000_000.0
	cost := float64(u.InputTokens)*r.InputPerMTok*perTok +
		float64(u.OutputTokens)*r.OutputPerMTok*perTok +
		float64(u.CacheReadTokens)*r.InputPerMTok*cacheReadMultiplier*perTok +
		float64(u.CacheWriteTokens)*r.InputPerMTok*cacheWriteMultiplier*perTok
	return cost, true
}
