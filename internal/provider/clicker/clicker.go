// Package clicker implements model.Clicker by grounding a natural-language
// target against detected UI elements — no separate planner LLM required. It
// lets a composed configuration (a planner model that emits instructions +
// this grounder-backed clicker) resolve "click the Submit button" to a point.
package clicker

import (
	"context"
	"math"
	"strings"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/grounder"
	"github.com/gnanam1990/argus/pkg/model"
)

// stopWords are filtered out of both the instruction and each candidate's
// label/text before scoring. Without this, instruction filler like "click ...
// button" inflates the overlap count of a long, filler-heavy candidate label
// (e.g. "Click here to submit your order now") past a short, exact target
// label (e.g. "Submit") purely because it repeats more of the filler back.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "to": true, "on": true, "in": true,
	"of": true, "and": true, "or": true, "click": true, "press": true,
	"tap": true, "button": true,
}

// Grounded resolves an instruction to a click point by matching it against a
// Grounder's detected elements.
type Grounded struct {
	g       grounder.Grounder
	minConf float64
}

// New builds a grounded clicker over g, discarding detections below minConf.
func New(g grounder.Grounder, minConf float64) *Grounded {
	return &Grounded{g: g, minConf: minConf}
}

var _ model.Clicker = (*Grounded)(nil)

// PredictClick detects elements and returns the best match's center. An
// element whose full label matches the instruction outright (case-
// insensitive, trimmed) always wins regardless of score. Otherwise each
// candidate is scored by its stop-word-filtered token overlap with the
// instruction, normalized by the square root of the candidate's own token
// count (see score) — this keeps a verbose, filler-heavy decoy label from
// outranking a short exact one purely by containing more overlapping words.
// ok is false when nothing scores above zero.
func (c *Grounded) PredictClick(ctx context.Context, img action.Image, instruction string) (action.Point, bool, error) {
	els, err := c.g.Detect(ctx, img)
	if err != nil {
		return action.Point{}, false, err
	}
	els = grounder.Filter(els, c.minConf)

	want := filteredTokens(instruction)
	normInstruction := strings.ToLower(strings.TrimSpace(instruction))

	bestScore := -1.0
	var best *grounder.Element
	for i := range els {
		if label := strings.ToLower(strings.TrimSpace(els[i].Label)); label != "" && label == normInstruction {
			return els[i].Box.Center(), true, nil
		}
		if s := score(want, els[i]); s > bestScore {
			bestScore = s
			best = &els[i]
		}
	}
	if best == nil || bestScore <= 0 {
		return action.Point{}, false, nil
	}
	return best.Box.Center(), true, nil
}

// score returns the stop-word-filtered token overlap between want and the
// element's label+text, divided by the square root of the candidate's own
// (filtered) token count. Normalizing by sqrt(len) rather than len still lets
// a candidate that repeats more of the instruction win, while stopping a
// long, filler-heavy label from beating a short exact match purely by bulk:
// e.g. for instruction "submit", "Submit" scores 1/sqrt(1)=1 and "Click here
// to submit your order now" scores 1/sqrt(5)≈0.45 (after stop-word removal).
func score(want []string, el grounder.Element) float64 {
	hay := filteredTokens(el.Label + " " + el.Text)
	if len(hay) == 0 {
		return 0
	}
	set := make(map[string]bool, len(hay))
	for _, h := range hay {
		set[h] = true
	}
	n := 0
	for _, w := range want {
		if set[w] {
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return float64(n) / math.Sqrt(float64(len(hay)))
}

// filteredTokens tokenizes s and drops stop words.
func filteredTokens(s string) []string {
	fields := tokens(s)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if !stopWords[f] {
			out = append(out, f)
		}
	}
	return out
}

func tokens(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	return fields
}
