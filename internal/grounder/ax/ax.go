// Package ax is a grounder.Grounder backed by the platform accessibility tree
// (AT-SPI on Linux, AXUIElement on macOS, UIAutomation on Windows). The tree
// gives exact element boxes and free semantic labels with no GPU and no tokens
// — the preferred detector when it is available.
//
// The concrete tree readers are platform- and CGo-specific and plug in via a
// TreeSource. The default source reports the tree as unavailable (ErrUnavailable)
// so a chain grounder falls back to a vision detector.
package ax

import (
	"context"
	"errors"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/grounder"
)

// ErrUnavailable means no accessibility tree could be read (unsupported
// platform, headless, or a canvas/WebGL surface with no semantic tree).
var ErrUnavailable = errors.New("ax: accessibility tree unavailable")

// TreeSource reads interactable elements from the platform accessibility tree.
type TreeSource func(ctx context.Context) ([]grounder.Element, error)

// Detector grounds via the accessibility tree.
type Detector struct {
	source TreeSource
}

// Option configures a Detector.
type Option func(*Detector)

// WithSource installs a platform tree reader (e.g. a build-tagged AT-SPI impl).
func WithSource(s TreeSource) Option { return func(d *Detector) { d.source = s } }

// New builds a detector. Without a source it reports ErrUnavailable.
func New(opts ...Option) *Detector {
	d := &Detector{}
	for _, o := range opts {
		o(d)
	}
	return d
}

var _ grounder.Grounder = (*Detector)(nil)

// Detect returns the accessibility-tree elements, or ErrUnavailable.
func (d *Detector) Detect(ctx context.Context, _ action.Image) ([]grounder.Element, error) {
	if d.source == nil {
		return nil, ErrUnavailable
	}
	return d.source(ctx)
}
