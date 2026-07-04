package grounder_test

import (
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/grounder"
)

// Detector ID collisions must never make one drawn label resolve to another
// element's box: Renumber keeps first occurrences and reassigns the rest.
func TestRenumberResolvesCollisions(t *testing.T) {
	t.Parallel()
	els := []grounder.Element{
		{ID: 5, Box: action.Rect{Min: action.Point{X: 0, Y: 0}, Max: action.Point{X: 10, Y: 10}}},
		{ID: 5, Box: action.Rect{Min: action.Point{X: 100, Y: 100}, Max: action.Point{X: 110, Y: 110}}},
		{ID: -1, Box: action.Rect{Min: action.Point{X: 200, Y: 200}, Max: action.Point{X: 210, Y: 210}}},
	}
	out := grounder.Renumber(els)

	if out[0].ID != 5 {
		t.Errorf("first occurrence must keep its ID, got %d", out[0].ID)
	}
	seen := map[int]bool{}
	for _, e := range out {
		if e.ID < 0 {
			t.Errorf("negative ID survived: %d", e.ID)
		}
		if seen[e.ID] {
			t.Errorf("duplicate ID after renumber: %d", e.ID)
		}
		seen[e.ID] = true
	}
	// Every element keeps its own box: index over the renumbered set must hold
	// all three distinct boxes.
	idx := grounder.Index(out)
	if len(idx) != 3 {
		t.Errorf("index size = %d, want 3 (no box lost to a collision)", len(idx))
	}
	// Input must not be mutated.
	if els[1].ID != 5 || els[2].ID != -1 {
		t.Error("Renumber must not mutate its input")
	}
}
