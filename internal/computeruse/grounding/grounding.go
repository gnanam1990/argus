// Package grounding builds the accessibility-tree observation the
// computer-use subsystem hands to a model. It wraps a screenshot and an
// internal/grounder/ax.TreeSource into a single synthetic root
// internal/computeruse/state.Element whose children are the flat elements
// the tree source reported, each carrying a stable depth-first index the
// model can act against later ("click element 3").
package grounding

import (
	"context"
	"fmt"
	"math"

	"github.com/gnanam1990/argus/internal/computeruse/state"
	"github.com/gnanam1990/argus/internal/grounder/ax"
	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/grounder"
)

// rootRole is the synthetic role assigned to the tree's root element. An
// ax.TreeSource returns a flat element list with no window node of its own,
// so DefaultProvider synthesizes one to hold them.
const rootRole = "AXWindow"

// pressAction is the action recorded on an interactable element. ax's tree
// source only ever reports press-style controls (buttons, links, fields,
// etc.), so this is the only action DefaultProvider ever assigns.
const pressAction = "AXPress"

// Provider produces the accessibility-tree observation of the frontmost
// window of a given app, as a single rooted state.Element tree the caller
// can flatten and index against (see state.AppState.Flatten).
type Provider interface {
	// FrontmostTree returns the frontmost window's element tree for bundleID.
	FrontmostTree(ctx context.Context, bundleID string) (state.Element, error)
}

// Shooter captures the current screen so a TreeSource can relate its own
// coordinate space to screenshot-pixel space (see package ax's doc comment
// on coordinate spaces). It is narrowed to just the method this package
// needs so it depends on no other package's Computer interface.
type Shooter func(ctx context.Context) (action.Image, error)

// DefaultProvider implements Provider by taking a screenshot and running it
// through an ax.TreeSource, then reshaping the flat element list the source
// returns into a single rooted tree.
type DefaultProvider struct {
	source ax.TreeSource
	shot   Shooter
}

var _ Provider = (*DefaultProvider)(nil)

// New builds a DefaultProvider. src reads the platform accessibility tree —
// ax.HostSource() in production, a fake ax.TreeSource in tests — and shot
// captures the screenshot passed to src so it can scale its own coordinate
// space into screenshot-pixel space. Neither argument may be nil; a nil src
// or shot causes FrontmostTree to panic when called, the same as any other
// required collaborator omitted at construction time.
func New(src ax.TreeSource, shot Shooter) *DefaultProvider {
	return &DefaultProvider{source: src, shot: shot}
}

// FrontmostTree takes a screenshot, runs the accessibility tree source
// against it, and maps the flat []grounder.Element result into a single
// synthetic root state.Element (Index 0, Role "AXWindow", Frame the bounding
// box of its children) whose Children are the reported elements in the order
// the source returned them. Each child is assigned a stable, sequential
// depth-first index starting at 1; elements with a zero-area or inverted box
// are dropped before indices are assigned, so dropped elements never create a
// gap relative to the elements that remain.
//
// bundleID identifies the app whose frontmost window is being observed. The
// tree source always reads whichever window is actually frontmost — it is
// not otherwise interpreted here — but is threaded through so any error
// returned can be attributed to the app the caller asked about.
//
// If either the screenshot or the tree source fails, FrontmostTree returns
// the zero state.Element and a wrapped error.
func (p *DefaultProvider) FrontmostTree(ctx context.Context, bundleID string) (state.Element, error) {
	img, err := p.shot(ctx)
	if err != nil {
		return state.Element{}, fmt.Errorf("grounding: screenshot for %q: %w", bundleID, err)
	}

	els, err := p.source(ctx, img)
	if err != nil {
		return state.Element{}, fmt.Errorf("grounding: accessibility tree for %q: %w", bundleID, err)
	}

	children := make([]state.Element, 0, len(els))
	idx := 1
	for _, el := range els {
		if el.Box.Empty() {
			continue
		}
		children = append(children, toStateElement(el, idx))
		idx++
	}

	return state.Element{
		Index:    0,
		Role:     rootRole,
		Frame:    boundingBox(children),
		Children: children,
	}, nil
}

// toStateElement converts one flat grounder.Element into a leaf
// state.Element at the given stable index. Label falls back to the
// element's Text when it has no Label of its own; Actions carries
// pressAction only when the source marked the element Interactable.
func toStateElement(el grounder.Element, index int) state.Element {
	label := el.Label
	if label == "" {
		label = el.Text
	}

	var actions []string
	if el.Interactable {
		actions = []string{pressAction}
	}

	return state.Element{
		Index:   index,
		Role:    el.Role,
		Label:   label,
		Frame:   toRect(el.Box),
		Actions: actions,
	}
}

// isChrome reports whether a role is window-manager chrome (the menu bar and
// its items) that spans the whole screen and should not define the app's window
// box. Excluding it keeps the window frame — and the scroll/observe target
// derived from it — over the actual application window.
func isChrome(role string) bool {
	switch role {
	case "AXMenuBar", "AXMenuBarItem":
		return true
	default:
		return false
	}
}

// toRect converts an int, min/max action.Rect into a float, origin/size
// state.Rect.
func toRect(r action.Rect) state.Rect {
	return state.Rect{
		X:      float64(r.Min.X),
		Y:      float64(r.Min.Y),
		Width:  float64(r.Width()),
		Height: float64(r.Height()),
	}
}

// boundingBox returns the smallest rect enclosing the app's window content: the
// union of every child's frame except menu-bar chrome, which spans the full
// screen width and would otherwise stretch the box across the whole display.
// Falls back to all children if every child is chrome (or there are none).
func boundingBox(children []state.Element) state.Rect {
	var frames []state.Rect
	for _, c := range children {
		if !isChrome(c.Role) {
			frames = append(frames, c.Frame)
		}
	}
	if len(frames) == 0 {
		// All chrome or empty: fall back to every child so we still report a box.
		for _, c := range children {
			frames = append(frames, c.Frame)
		}
	}
	if len(frames) == 0 {
		return state.Rect{}
	}

	minX, minY := frames[0].X, frames[0].Y
	maxX, maxY := frames[0].X+frames[0].Width, frames[0].Y+frames[0].Height
	for _, f := range frames[1:] {
		minX = math.Min(minX, f.X)
		minY = math.Min(minY, f.Y)
		maxX = math.Max(maxX, f.X+f.Width)
		maxY = math.Max(maxY, f.Y+f.Height)
	}
	return state.Rect{X: minX, Y: minY, Width: maxX - minX, Height: maxY - minY}
}
