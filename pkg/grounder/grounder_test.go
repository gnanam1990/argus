package grounder_test

import (
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/grounder"
)

func rect(x0, y0, x1, y1 int) action.Rect {
	return action.Rect{Min: action.Point{X: x0, Y: y0}, Max: action.Point{X: x1, Y: y1}}
}

func TestIndex(t *testing.T) {
	t.Parallel()
	els := []grounder.Element{
		{ID: 0, Box: rect(0, 0, 10, 10)},
		{ID: 5, Box: rect(20, 20, 30, 30)},
	}
	idx := grounder.Index(els)
	if len(idx) != 2 {
		t.Fatalf("index len = %d, want 2", len(idx))
	}
	if idx[5] != rect(20, 20, 30, 30) {
		t.Errorf("idx[5] = %v", idx[5])
	}
	if got := idx[0].Center(); got != (action.Point{X: 5, Y: 5}) {
		t.Errorf("center of mark 0 = %v, want (5,5)", got)
	}
}

func TestIndexEmpty(t *testing.T) {
	t.Parallel()
	idx := grounder.Index(nil)
	if idx == nil || len(idx) != 0 {
		t.Errorf("Index(nil) = %v, want empty non-nil map", idx)
	}
}

func TestFilter(t *testing.T) {
	t.Parallel()
	els := []grounder.Element{
		{ID: 0, Interactable: true, Confidence: 0.9},
		{ID: 1, Interactable: false, Confidence: 0.99}, // not interactable
		{ID: 2, Interactable: true, Confidence: 0.3},   // below threshold
		{ID: 3, Interactable: true, Confidence: 0.5},   // exactly at threshold
	}
	got := grounder.Filter(els, 0.5)
	if len(got) != 2 {
		t.Fatalf("filtered len = %d, want 2", len(got))
	}
	// order preserved: IDs 0 then 3
	if got[0].ID != 0 || got[1].ID != 3 {
		t.Errorf("filtered IDs = %d, %d; want 0, 3", got[0].ID, got[1].ID)
	}
}
