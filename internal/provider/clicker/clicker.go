// Package clicker implements model.Clicker by grounding a natural-language
// target against detected UI elements — no separate planner LLM required. It
// lets a composed configuration (a planner model that emits instructions +
// this grounder-backed clicker) resolve "click the Submit button" to a point.
package clicker

import (
	"context"
	"strings"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/grounder"
	"github.com/gnanam1990/argus/pkg/model"
)

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

// PredictClick detects elements, scores each against the instruction by shared
// words in its label/text, and returns the best element's center. ok is false
// when nothing matches.
func (c *Grounded) PredictClick(ctx context.Context, img action.Image, instruction string) (action.Point, bool, error) {
	els, err := c.g.Detect(ctx, img)
	if err != nil {
		return action.Point{}, false, err
	}
	els = grounder.Filter(els, c.minConf)

	want := tokens(instruction)
	bestScore := 0
	var best *grounder.Element
	for i := range els {
		s := score(want, els[i])
		if s > bestScore {
			bestScore = s
			best = &els[i]
		}
	}
	if best == nil {
		return action.Point{}, false, nil
	}
	return best.Box.Center(), true, nil
}

func score(want []string, el grounder.Element) int {
	hay := tokens(el.Label + " " + el.Text)
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
	return n
}

func tokens(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	return fields
}
