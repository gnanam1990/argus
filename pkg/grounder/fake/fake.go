// Package fake provides scriptable grounder.Grounder and passthrough
// grounder.Marker implementations for tests — no vision service required.
package fake

import (
	"context"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/grounder"
)

// Grounder returns a fixed set of elements (or an error) on Detect.
type Grounder struct {
	els []grounder.Element
	err error
}

var _ grounder.Grounder = (*Grounder)(nil)

// NewGrounder builds a fake that detects the given elements.
func NewGrounder(els ...grounder.Element) *Grounder { return &Grounder{els: els} }

// WithError makes Detect return err.
func (g *Grounder) WithError(err error) *Grounder {
	g.err = err
	return g
}

// Detect returns a copy of the scripted elements, or the configured error.
func (g *Grounder) Detect(context.Context, action.Image) ([]grounder.Element, error) {
	if g.err != nil {
		return nil, g.err
	}
	out := make([]grounder.Element, len(g.els))
	copy(out, g.els)
	return out, nil
}

// Marker is a passthrough Marker: it returns the image unchanged and derives
// the index from element IDs. Real pixel overlay lives in internal/mark.
type Marker struct{}

var _ grounder.Marker = Marker{}

// Overlay returns the image unchanged and the ID→box index.
func (Marker) Overlay(img action.Image, els []grounder.Element) (action.Image, map[int]action.Rect, error) {
	return img, grounder.Index(els), nil
}
