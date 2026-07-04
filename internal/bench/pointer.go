package bench

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/grounder"
)

// Pointer is the system under test: given a screenshot and a
// natural-language instruction, predict the point to click. Run scores
// whichever Pointer it is given, so the same harness benchmarks a raw model,
// a grounder-backed clicker, or anything else that can answer the question.
type Pointer interface {
	PredictPoint(ctx context.Context, img action.Image, instruction string) (action.Point, error)
}

// FuncPointer adapts a plain function to Pointer, for scoring anything that
// isn't a grounder.Grounder (a raw model call, a recorded replay, ...).
type FuncPointer func(ctx context.Context, img action.Image, instruction string) (action.Point, error)

var _ Pointer = FuncPointer(nil)

// PredictPoint calls f.
func (f FuncPointer) PredictPoint(ctx context.Context, img action.Image, instruction string) (action.Point, error) {
	return f(ctx, img, instruction)
}

// grounderPointer adapts a grounder.Grounder into a Pointer by detecting
// elements and picking the one whose Label/Text best matches the
// instruction. It mirrors internal/provider/clicker's matching approach but
// reports "no match" as an error instead of an ok bool, since a benchmark run
// scores a failed grounding as a miss rather than silently skipping it.
type grounderPointer struct {
	g       grounder.Grounder
	minConf float64
}

// GrounderPointer builds a Pointer over g: it runs Detect, discards
// detections below minConfidence (see grounder.Filter), and returns the
// center of whichever remaining element's Label/Text best matches
// instruction. It returns an error when nothing matches.
func GrounderPointer(g grounder.Grounder, minConfidence float64) Pointer {
	return &grounderPointer{g: g, minConf: minConfidence}
}

var _ Pointer = (*grounderPointer)(nil)

// PredictPoint detects elements, scores each against instruction, and
// returns the best match's box center.
func (p *grounderPointer) PredictPoint(ctx context.Context, img action.Image, instruction string) (action.Point, error) {
	els, err := p.g.Detect(ctx, img)
	if err != nil {
		return action.Point{}, fmt.Errorf("bench: detect: %w", err)
	}
	els = grounder.Filter(els, p.minConf)

	el, ok := bestMatch(instruction, els)
	if !ok {
		return action.Point{}, fmt.Errorf("bench: no element matches instruction %q", instruction)
	}
	return el.Box.Center(), nil
}

// stopWords are dropped before matching an instruction against a candidate
// element's Label/Text — they carry no discriminating signal, so "click the
// Submit button" and "press Submit" score identically against a "Submit"
// label.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true,
	"to": true, "on": true, "in": true, "of": true,
	"and": true, "or": true,
	"click": true, "press": true, "tap": true, "button": true,
}

// normalizeTokens lowercases s, splits it on runs of non-alphanumeric
// characters, and drops stop words.
func normalizeTokens(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if !stopWords[f] {
			out = append(out, f)
		}
	}
	return out
}

// sameTokens reports whether a and b are the same token sequence, in order.
func sameTokens(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// overlapScore is a normalized bag-of-words overlap: how many distinct
// instrTokens appear among candTokens, scaled down by sqrt(len(candTokens))
// so a short, precise label ("Submit") beats a long candidate that merely
// happens to contain the same word ("Click here to submit your order now").
func overlapScore(instrTokens, candTokens []string) float64 {
	if len(candTokens) == 0 {
		return 0
	}
	cand := make(map[string]bool, len(candTokens))
	for _, t := range candTokens {
		cand[t] = true
	}
	seen := make(map[string]bool, len(instrTokens))
	overlap := 0
	for _, t := range instrTokens {
		if seen[t] {
			continue // count each distinct instruction token once
		}
		seen[t] = true
		if cand[t] {
			overlap++
		}
	}
	return float64(overlap) / math.Sqrt(float64(len(candTokens)))
}

// elementScore scores el against the already-normalized instruction tokens.
// An element whose Label matches the instruction exactly (after
// normalization) wins outright — this keeps a precisely-labeled element from
// losing to a competitor merely because the precise element also carries a
// long, noisy Text field that drags down its combined Label+Text overlap
// score.
func elementScore(instrTokens []string, el grounder.Element) float64 {
	labelTokens := normalizeTokens(el.Label)
	if len(labelTokens) > 0 && sameTokens(labelTokens, instrTokens) {
		return math.Inf(1)
	}
	candTokens := normalizeTokens(el.Label + " " + el.Text)
	return overlapScore(instrTokens, candTokens)
}

// bestMatch returns the element that best matches instruction, or false if
// every element scores zero (no shared tokens with instruction at all).
func bestMatch(instruction string, els []grounder.Element) (*grounder.Element, bool) {
	instrTokens := normalizeTokens(instruction)
	var best *grounder.Element
	bestScore := 0.0
	for i := range els {
		if s := elementScore(instrTokens, els[i]); s > bestScore {
			bestScore = s
			best = &els[i]
		}
	}
	if best == nil {
		return nil, false
	}
	return best, true
}
