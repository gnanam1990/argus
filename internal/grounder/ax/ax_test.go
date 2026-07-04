package ax_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gnanam1990/argus/internal/grounder/ax"
	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/grounder"
)

func TestDefaultUnavailable(t *testing.T) {
	t.Parallel()
	_, err := ax.New().Detect(context.Background(), action.Image{})
	if !errors.Is(err, ax.ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

func TestWithSource(t *testing.T) {
	t.Parallel()
	want := []grounder.Element{{ID: 1, Label: "Save", Interactable: true}}
	d := ax.New(ax.WithSource(func(context.Context) ([]grounder.Element, error) {
		return want, nil
	}))
	got, err := d.Detect(context.Background(), action.Image{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Label != "Save" {
		t.Errorf("got %+v", got)
	}
}

func TestSourceError(t *testing.T) {
	t.Parallel()
	boom := errors.New("dbus down")
	d := ax.New(ax.WithSource(func(context.Context) ([]grounder.Element, error) {
		return nil, boom
	}))
	if _, err := d.Detect(context.Background(), action.Image{}); !errors.Is(err, boom) {
		t.Errorf("err = %v, want boom", err)
	}
}
