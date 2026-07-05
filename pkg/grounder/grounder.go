// Package grounder defines the swappable set-of-marks seam. A Grounder detects
// interactable UI elements in a screenshot; a Marker overlays numbered marks
// and returns the number→box index the executor resolves clicks against.
//
// Detector backends (accessibility tree, an out-of-process vision service) plug
// in behind Grounder and return boxes only. The numbered-overlay rendering and
// the number→click mapping always stay in Go (see internal/mark), so swapping
// the detector never changes the marking or click-resolution code.
package grounder

import (
	"context"

	"github.com/gnanam1990/argus/pkg/action"
)

// Element is a detected UI element in screenshot-pixel space. ID is the mark
// number assigned to it; the executor resolves a set-of-marks click by looking
// up ID in the Marker's index.
type Element struct {
	ID    int         `json:"id"`
	Box   action.Rect `json:"box"`
	Label string      `json:"label,omitempty"`
	Text  string      `json:"text,omitempty"`
	// Role is the source's own element role when it has one (e.g. an
	// accessibility role like "AXButton"/"AXLink"); empty for detectors that
	// don't expose one (a vision backend). Carried through so downstream
	// consumers can filter/label by it.
	Role         string  `json:"role,omitempty"`
	Interactable bool    `json:"interactable"`
	Confidence   float64 `json:"confidence"`
}

// Grounder detects UI elements in a screenshot.
type Grounder interface {
	Detect(ctx context.Context, img action.Image) ([]Element, error)
}

// Marker overlays numbered marks on a screenshot and returns the marked image
// plus the mark-number → box index. The overlay stays pure-Go regardless of
// which Grounder produced the elements.
type Marker interface {
	Overlay(img action.Image, els []Element) (marked action.Image, index map[int]action.Rect, err error)
}

// Index maps each element's ID to its box. It is the canonical way to build the
// set-of-marks index the executor consumes; a Marker returns exactly this.
// Duplicate IDs are last-writer-wins — run Renumber first so a detector that
// emits colliding IDs cannot make a drawn label resolve to the wrong box.
func Index(els []Element) map[int]action.Rect {
	if len(els) == 0 {
		return map[int]action.Rect{}
	}
	m := make(map[int]action.Rect, len(els))
	for _, e := range els {
		m[e.ID] = e.Box
	}
	return m
}

// Renumber returns a copy of els in which every ID is unique: the first
// occurrence of a non-negative ID keeps it, collisions and negative IDs are
// reassigned to the next unused non-negative integer. Markers renumber before
// drawing so the labels shown to the model always match the index clicks
// resolve against.
func Renumber(els []Element) []Element {
	if len(els) == 0 {
		return els
	}
	out := make([]Element, len(els))
	copy(out, els)
	used := make(map[int]bool, len(out))
	next := 0
	for i := range out {
		if out[i].ID >= 0 && !used[out[i].ID] {
			used[out[i].ID] = true
			continue
		}
		for used[next] {
			next++
		}
		out[i].ID = next
		used[next] = true
	}
	return out
}

// Filter returns the interactable elements whose confidence is >= min,
// preserving input order. It is how detector backends discard weak or
// non-actionable detections before marking.
func Filter(els []Element, minConfidence float64) []Element {
	out := make([]Element, 0, len(els))
	for _, e := range els {
		if e.Interactable && e.Confidence >= minConfidence {
			out = append(out, e)
		}
	}
	return out
}
