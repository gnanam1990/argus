package pricing

import (
	"math"
	"testing"

	"github.com/gnanam1990/argus/pkg/model"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestLookup(t *testing.T) {
	t.Parallel()
	if r, ok := Lookup("claude-opus-4-8"); !ok || r.InputPerMTok != 5 || r.OutputPerMTok != 25 {
		t.Errorf("opus-4-8 = %+v, %v", r, ok)
	}
	if _, ok := Lookup("no-such-model"); ok {
		t.Error("unknown model should not resolve")
	}
}

func TestCost(t *testing.T) {
	t.Parallel()
	// 1M input + 1M output on opus-4-8 = $5 + $25 = $30.
	got, ok := Cost("claude-opus-4-8", model.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000})
	if !ok || !approx(got, 30) {
		t.Errorf("Cost = %v, %v; want 30", got, ok)
	}

	// Cache read is 0.1x input; write is 1.25x input.
	got, _ = Cost("claude-opus-4-8", model.Usage{CacheReadTokens: 1_000_000, CacheWriteTokens: 1_000_000})
	want := 5*0.10 + 5*1.25 // 0.5 + 6.25
	if !approx(got, want) {
		t.Errorf("cache Cost = %v, want %v", got, want)
	}

	// Sonnet: 1M in + 1M out = 3 + 15 = 18.
	got, _ = Cost("claude-sonnet-5", model.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000})
	if !approx(got, 18) {
		t.Errorf("sonnet Cost = %v, want 18", got)
	}
}

func TestCostUnknownModel(t *testing.T) {
	t.Parallel()
	if _, ok := Cost("mystery", model.Usage{InputTokens: 100}); ok {
		t.Error("unknown model must return ok=false")
	}
}

func TestCostZeroUsage(t *testing.T) {
	t.Parallel()
	if got, ok := Cost("claude-haiku-4-5", model.Usage{}); !ok || got != 0 {
		t.Errorf("zero usage = %v, %v; want 0, true", got, ok)
	}
}
