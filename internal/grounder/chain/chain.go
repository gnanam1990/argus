// Package chain composes grounders: it tries a primary detector (typically the
// accessibility tree — exact, free, no GPU) and falls back to a secondary
// detector (typically a vision service) when the primary errors or returns too
// few elements. This is the recommended default: cheap-and-exact first, vision
// only for canvas/WebGL/Electron surfaces the tree can't see.
package chain

import (
	"context"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/grounder"
)

// Grounder tries primary then fallback.
type Grounder struct {
	primary  grounder.Grounder
	fallback grounder.Grounder
	minFirst int
}

// Option configures a Grounder.
type Option func(*Grounder)

// WithMinPrimary sets the minimum element count the primary must return to be
// accepted; below it (or on error) the fallback runs. Default 1.
func WithMinPrimary(n int) Option { return func(g *Grounder) { g.minFirst = n } }

// New composes primary with a fallback.
func New(primary, fallback grounder.Grounder, opts ...Option) *Grounder {
	g := &Grounder{primary: primary, fallback: fallback, minFirst: 1}
	for _, o := range opts {
		o(g)
	}
	return g
}

var _ grounder.Grounder = (*Grounder)(nil)

// Detect runs the primary; on error or too-few elements, runs the fallback.
func (g *Grounder) Detect(ctx context.Context, img action.Image) ([]grounder.Element, error) {
	if els, err := g.primary.Detect(ctx, img); err == nil && len(els) >= g.minFirst {
		return els, nil
	}
	return g.fallback.Detect(ctx, img)
}
