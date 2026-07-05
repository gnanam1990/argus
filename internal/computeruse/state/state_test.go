package state_test

import (
	"testing"

	"github.com/gnanam1990/argus/internal/computeruse/state"
)

func tree() state.AppState {
	return state.AppState{
		Elements: []state.Element{
			{Index: 0, Role: "AXWindow", Children: []state.Element{
				{Index: 1, Role: "AXButton", Label: "OK"},
				{Index: 2, Role: "AXGroup", Children: []state.Element{
					{Index: 3, Role: "AXTextField", Label: "Name"},
				}},
			}},
		},
	}
}

func TestFlattenDepthFirst(t *testing.T) {
	t.Parallel()
	flat := tree().Flatten()
	if len(flat) != 4 {
		t.Fatalf("flatten = %d elements, want 4", len(flat))
	}
	want := []int{0, 1, 2, 3}
	for i, e := range flat {
		if e.Index != want[i] {
			t.Errorf("position %d has index %d, want %d (depth-first order)", i, e.Index, want[i])
		}
		if e.Children != nil {
			t.Error("flattened elements must not carry children")
		}
	}
}

func TestFindByIndex(t *testing.T) {
	t.Parallel()
	s := tree()
	e, ok := s.FindByIndex(3)
	if !ok || e.Label != "Name" {
		t.Errorf("FindByIndex(3) = %+v, %v", e, ok)
	}
	if _, ok := s.FindByIndex(99); ok {
		t.Error("unknown index must not resolve")
	}
}
