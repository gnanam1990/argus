package clicker_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gnanam1990/argus/internal/provider/clicker"
	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/grounder"
	grounderfake "github.com/gnanam1990/argus/pkg/grounder/fake"
)

func rect(x0, y0, x1, y1 int) action.Rect {
	return action.Rect{Min: action.Point{X: x0, Y: y0}, Max: action.Point{X: x1, Y: y1}}
}

func TestPredictClickMatchesLabel(t *testing.T) {
	t.Parallel()
	g := grounderfake.NewGrounder(
		grounder.Element{ID: 0, Box: rect(0, 0, 10, 10), Label: "Cancel", Interactable: true, Confidence: 0.9},
		grounder.Element{ID: 1, Box: rect(20, 20, 40, 40), Label: "Submit button", Interactable: true, Confidence: 0.9},
	)
	c := clicker.New(g, 0.5)

	pt, ok, err := c.PredictClick(context.Background(), action.Image{}, "click the submit button")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected a match")
	}
	// Submit's box centers at (30,30).
	if pt != (action.Point{X: 30, Y: 30}) {
		t.Errorf("point = %v, want (30,30)", pt)
	}
}

func TestPredictClickNoMatch(t *testing.T) {
	t.Parallel()
	g := grounderfake.NewGrounder(
		grounder.Element{ID: 0, Box: rect(0, 0, 10, 10), Label: "Cancel", Interactable: true, Confidence: 0.9},
	)
	c := clicker.New(g, 0.5)
	_, ok, err := c.PredictClick(context.Background(), action.Image{}, "open settings menu")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected no match for an unrelated instruction")
	}
}

func TestPredictClickFiltersConfidence(t *testing.T) {
	t.Parallel()
	g := grounderfake.NewGrounder(
		grounder.Element{ID: 0, Box: rect(0, 0, 10, 10), Label: "Submit", Interactable: true, Confidence: 0.2},
	)
	c := clicker.New(g, 0.5) // 0.2 < 0.5 → filtered out
	_, ok, _ := c.PredictClick(context.Background(), action.Image{}, "submit")
	if ok {
		t.Error("low-confidence element should be filtered")
	}
}

func TestPredictClickGrounderError(t *testing.T) {
	t.Parallel()
	g := grounderfake.NewGrounder().WithError(errors.New("vision down"))
	c := clicker.New(g, 0.5)
	if _, _, err := c.PredictClick(context.Background(), action.Image{}, "x"); err == nil {
		t.Error("expected grounder error to propagate")
	}
}
