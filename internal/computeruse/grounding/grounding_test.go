package grounding_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gnanam1990/argus/internal/computeruse/grounding"
	"github.com/gnanam1990/argus/internal/computeruse/state"
	"github.com/gnanam1990/argus/internal/grounder/ax"
	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/grounder"
)

// rect builds an action.Rect from raw corner ints, matching the pkg/action
// convention used throughout the repo's tests.
func rect(x0, y0, x1, y1 int) action.Rect {
	return action.Rect{Min: action.Point{X: x0, Y: y0}, Max: action.Point{X: x1, Y: y1}}
}

// fixedShot returns a Shooter that always yields img, nil.
func fixedShot(img action.Image) grounding.Shooter {
	return func(context.Context) (action.Image, error) { return img, nil }
}

// fixedSource returns an ax.TreeSource that always yields els, nil,
// regardless of the image it's given.
func fixedSource(els []grounder.Element) ax.TreeSource {
	return func(context.Context, action.Image) ([]grounder.Element, error) { return els, nil }
}

func TestFrontmostTree_MapsFlatListToIndexedChildren(t *testing.T) {
	t.Parallel()

	els := []grounder.Element{
		{ID: 0, Box: rect(0, 0, 100, 20), Label: "Save", Interactable: true},
		{ID: 1, Box: rect(0, 20, 100, 40), Text: "hello world"}, // no Label, not interactable
		{ID: 2, Box: rect(10, 40, 60, 80), Label: "Cancel", Interactable: true},
	}
	p := grounding.New(fixedSource(els), fixedShot(action.Image{}))

	got, err := p.FrontmostTree(context.Background(), "com.example.app")
	if err != nil {
		t.Fatalf("FrontmostTree: %v", err)
	}

	// Root synthesizes AXWindow at index 0.
	if got.Index != 0 {
		t.Errorf("root index = %d, want 0", got.Index)
	}
	if got.Role != "AXWindow" {
		t.Errorf("root role = %q, want AXWindow", got.Role)
	}

	if len(got.Children) != 3 {
		t.Fatalf("children = %d, want 3", len(got.Children))
	}

	// Indices are sequential depth-first starting at 1, in source order.
	for i, c := range got.Children {
		wantIdx := i + 1
		if c.Index != wantIdx {
			t.Errorf("child %d index = %d, want %d", i, c.Index, wantIdx)
		}
	}

	// Frame conversion: int min/max Rect -> float origin/size Rect.
	wantFrame0 := state.Rect{X: 0, Y: 0, Width: 100, Height: 20}
	if got.Children[0].Frame != wantFrame0 {
		t.Errorf("child 0 frame = %+v, want %+v", got.Children[0].Frame, wantFrame0)
	}
	wantFrame2 := state.Rect{X: 10, Y: 40, Width: 50, Height: 40}
	if got.Children[2].Frame != wantFrame2 {
		t.Errorf("child 2 frame = %+v, want %+v", got.Children[2].Frame, wantFrame2)
	}

	// Label fallback to Text when Label is empty.
	if got.Children[1].Label != "hello world" {
		t.Errorf("child 1 label = %q, want fallback to Text", got.Children[1].Label)
	}
	if got.Children[0].Label != "Save" {
		t.Errorf("child 0 label = %q, want Save", got.Children[0].Label)
	}

	// Interactable elements get AXPress; non-interactable get none.
	if len(got.Children[0].Actions) != 1 || got.Children[0].Actions[0] != "AXPress" {
		t.Errorf("child 0 actions = %v, want [AXPress]", got.Children[0].Actions)
	}
	if len(got.Children[2].Actions) != 1 || got.Children[2].Actions[0] != "AXPress" {
		t.Errorf("child 2 actions = %v, want [AXPress]", got.Children[2].Actions)
	}
	if len(got.Children[1].Actions) != 0 {
		t.Errorf("child 1 actions = %v, want none (not interactable)", got.Children[1].Actions)
	}

	// Root frame is the bounding box of all (surviving) children.
	wantRoot := state.Rect{X: 0, Y: 0, Width: 100, Height: 80}
	if got.Frame != wantRoot {
		t.Errorf("root frame = %+v, want %+v", got.Frame, wantRoot)
	}
}

func TestFrontmostTree_FiltersZeroAreaAndInvertedBoxes(t *testing.T) {
	t.Parallel()

	els := []grounder.Element{
		{ID: 0, Box: rect(0, 0, 50, 50), Label: "keep-1"},
		{ID: 1, Box: rect(10, 10, 10, 40), Label: "zero-width"},  // Max.X == Min.X
		{ID: 2, Box: rect(10, 10, 40, 10), Label: "zero-height"}, // Max.Y == Min.Y
		{ID: 3, Box: rect(40, 40, 10, 10), Label: "inverted"},    // Max < Min on both axes
		{ID: 4, Box: rect(60, 60, 100, 100), Label: "keep-2"},
	}
	p := grounding.New(fixedSource(els), fixedShot(action.Image{}))

	got, err := p.FrontmostTree(context.Background(), "com.example.app")
	if err != nil {
		t.Fatalf("FrontmostTree: %v", err)
	}

	if len(got.Children) != 2 {
		t.Fatalf("children = %d, want 2 (zero-area/inverted filtered)", len(got.Children))
	}
	// Indices are sequential over surviving elements only — no gaps.
	if got.Children[0].Index != 1 || got.Children[1].Index != 2 {
		t.Errorf("surviving indices = %d, %d; want 1, 2", got.Children[0].Index, got.Children[1].Index)
	}
	if got.Children[0].Label != "keep-1" || got.Children[1].Label != "keep-2" {
		t.Errorf("surviving labels = %q, %q; want keep-1, keep-2", got.Children[0].Label, got.Children[1].Label)
	}

	// Bounding box only accounts for surviving children.
	want := state.Rect{X: 0, Y: 0, Width: 100, Height: 100}
	if got.Frame != want {
		t.Errorf("root frame = %+v, want %+v", got.Frame, want)
	}
}

func TestFrontmostTree_IndexStability(t *testing.T) {
	t.Parallel()

	els := []grounder.Element{
		{ID: 9, Box: rect(0, 0, 10, 10), Label: "a"},
		{ID: 2, Box: rect(20, 20, 30, 30), Label: "b"},
		{ID: 7, Box: rect(40, 40, 50, 50), Label: "c"},
	}
	p := grounding.New(fixedSource(els), fixedShot(action.Image{}))

	first, err := p.FrontmostTree(context.Background(), "com.example.app")
	if err != nil {
		t.Fatalf("FrontmostTree (first): %v", err)
	}
	second, err := p.FrontmostTree(context.Background(), "com.example.app")
	if err != nil {
		t.Fatalf("FrontmostTree (second): %v", err)
	}

	if len(first.Children) != len(second.Children) {
		t.Fatalf("child count differs across calls: %d vs %d", len(first.Children), len(second.Children))
	}
	for i := range first.Children {
		if first.Children[i].Index != second.Children[i].Index {
			t.Errorf("index at position %d differs: %d vs %d", i, first.Children[i].Index, second.Children[i].Index)
		}
		if first.Children[i].Label != second.Children[i].Label {
			t.Errorf("label at position %d differs: %q vs %q", i, first.Children[i].Label, second.Children[i].Label)
		}
	}
}

func TestFrontmostTree_EmptySourceYieldsEmptyRoot(t *testing.T) {
	t.Parallel()

	p := grounding.New(fixedSource(nil), fixedShot(action.Image{}))

	got, err := p.FrontmostTree(context.Background(), "com.example.app")
	if err != nil {
		t.Fatalf("FrontmostTree: %v", err)
	}
	if len(got.Children) != 0 {
		t.Errorf("children = %d, want 0", len(got.Children))
	}
	if got.Frame != (state.Rect{}) {
		t.Errorf("root frame = %+v, want zero value", got.Frame)
	}
}

func TestFrontmostTree_ScreenshotError(t *testing.T) {
	t.Parallel()

	boom := errors.New("capture failed")
	shot := func(context.Context) (action.Image, error) { return action.Image{}, boom }
	sourceCalled := false
	src := func(context.Context, action.Image) ([]grounder.Element, error) {
		sourceCalled = true
		return nil, nil
	}
	p := grounding.New(src, shot)

	_, err := p.FrontmostTree(context.Background(), "com.example.app")
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want wrapped %v", err, boom)
	}
	if sourceCalled {
		t.Error("tree source must not be called when the screenshot fails")
	}
}

func TestFrontmostTree_TreeSourceError(t *testing.T) {
	t.Parallel()

	boom := errors.New("ax unavailable")
	src := func(context.Context, action.Image) ([]grounder.Element, error) { return nil, boom }
	p := grounding.New(src, fixedShot(action.Image{}))

	_, err := p.FrontmostTree(context.Background(), "com.example.app")
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want wrapped %v", err, boom)
	}
}

func TestFrontmostTree_PassesScreenshotToSource(t *testing.T) {
	t.Parallel()

	wantImg := action.Image{MIME: action.MIMEPNG, Data: []byte{1, 2, 3}}
	var gotImg action.Image
	src := func(_ context.Context, img action.Image) ([]grounder.Element, error) {
		gotImg = img
		return nil, nil
	}
	p := grounding.New(src, fixedShot(wantImg))

	if _, err := p.FrontmostTree(context.Background(), "com.example.app"); err != nil {
		t.Fatalf("FrontmostTree: %v", err)
	}
	if gotImg.MIME != wantImg.MIME || string(gotImg.Data) != string(wantImg.Data) {
		t.Errorf("source got image %+v, want %+v", gotImg, wantImg)
	}
}

func TestDefaultProviderSatisfiesProvider(t *testing.T) {
	t.Parallel()
	var _ grounding.Provider = grounding.New(fixedSource(nil), fixedShot(action.Image{}))
}
