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

// TestPredictClickPrefersExactShortLabelOverVerboseDecoy is the audit's decoy
// scenario: a verbose candidate that happens to repeat the instruction's
// filler words ("click", "to", ...) must not outrank a short candidate whose
// label is the exact target, purely because it has more raw word overlap.
func TestPredictClickPrefersExactShortLabelOverVerboseDecoy(t *testing.T) {
	t.Parallel()
	g := grounderfake.NewGrounder(
		grounder.Element{ID: 0, Box: rect(0, 0, 300, 20), Label: "Click here to submit your order now", Interactable: true, Confidence: 0.9},
		grounder.Element{ID: 1, Box: rect(50, 50, 70, 70), Label: "Submit", Interactable: true, Confidence: 0.9},
	)
	c := clicker.New(g, 0.5)

	pt, ok, err := c.PredictClick(context.Background(), action.Image{}, "click submit")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected a match")
	}
	// "Submit"'s box centers at (60,60); the verbose decoy centers at (150,10).
	if pt != (action.Point{X: 60, Y: 60}) {
		t.Errorf("point = %v, want (60,60) (the short exact label, not the verbose decoy)", pt)
	}
}

// TestPredictClickExactLabelMatchWinsOutright confirms the outright-win rule:
// an element whose full label matches the instruction (case-insensitively)
// is chosen even when a longer decoy would otherwise score at least as high.
func TestPredictClickExactLabelMatchWinsOutright(t *testing.T) {
	t.Parallel()
	g := grounderfake.NewGrounder(
		grounder.Element{ID: 0, Box: rect(0, 0, 300, 20), Label: "submit your order and submit again", Interactable: true, Confidence: 0.9},
		grounder.Element{ID: 1, Box: rect(50, 50, 70, 70), Label: "Submit", Interactable: true, Confidence: 0.9},
	)
	c := clicker.New(g, 0.5)

	pt, ok, err := c.PredictClick(context.Background(), action.Image{}, "submit")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected a match")
	}
	if pt != (action.Point{X: 60, Y: 60}) {
		t.Errorf("point = %v, want (60,60) (the outright exact-label match)", pt)
	}
}

// TestPredictClickStopWordsDoNotMatchAlone confirms an instruction that is
// pure filler (stop words only) cannot spuriously match a candidate via
// leftover stop-word tokens, since both sides are filtered before scoring.
func TestPredictClickStopWordsDoNotMatchAlone(t *testing.T) {
	t.Parallel()
	g := grounderfake.NewGrounder(
		grounder.Element{ID: 0, Box: rect(0, 0, 10, 10), Label: "the button", Interactable: true, Confidence: 0.9},
	)
	c := clicker.New(g, 0.5)
	_, ok, err := c.PredictClick(context.Background(), action.Image{}, "click on the button")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected no match: label is only stop words once filtered")
	}
}
