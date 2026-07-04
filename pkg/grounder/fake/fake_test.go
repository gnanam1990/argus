package fake

import (
	"context"
	"errors"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/grounder"
)

func rect(x0, y0, x1, y1 int) action.Rect {
	return action.Rect{Min: action.Point{X: x0, Y: y0}, Max: action.Point{X: x1, Y: y1}}
}

func TestGrounderDetect(t *testing.T) {
	t.Parallel()
	want := grounder.Element{ID: 1, Box: rect(1, 2, 3, 4), Label: "OK button", Interactable: true, Confidence: 0.8}
	g := NewGrounder(want)

	got, err := g.Detect(context.Background(), action.Image{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != want {
		t.Errorf("Detect = %+v, want [%+v]", got, want)
	}

	// Returned slice is a copy — mutating it must not affect the fake.
	got[0].Label = "MUTATED"
	again, _ := g.Detect(context.Background(), action.Image{})
	if again[0].Label != "OK button" {
		t.Error("Detect must return an independent copy")
	}
}

func TestGrounderError(t *testing.T) {
	t.Parallel()
	boom := errors.New("vision down")
	g := NewGrounder().WithError(boom)
	if _, err := g.Detect(context.Background(), action.Image{}); !errors.Is(err, boom) {
		t.Errorf("err = %v, want boom", err)
	}
}

func TestMarkerPassthrough(t *testing.T) {
	t.Parallel()
	img := action.Image{MIME: action.MIMEPNG, Data: []byte{1, 2, 3}}
	els := []grounder.Element{{ID: 7, Box: rect(0, 0, 4, 4)}}

	marked, idx, err := Marker{}.Overlay(img, els)
	if err != nil {
		t.Fatal(err)
	}
	if marked.MIME != img.MIME || len(marked.Data) != 3 {
		t.Errorf("marked image changed: %+v", marked)
	}
	if idx[7] != rect(0, 0, 4, 4) {
		t.Errorf("index[7] = %v", idx[7])
	}
}
