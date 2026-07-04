package chain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gnanam1990/argus/internal/grounder/chain"
	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/grounder"
)

// countingGrounder records how many times Detect was called.
type countingGrounder struct {
	els   []grounder.Element
	err   error
	calls int
}

func (g *countingGrounder) Detect(context.Context, action.Image) ([]grounder.Element, error) {
	g.calls++
	return g.els, g.err
}

func el(id int) grounder.Element { return grounder.Element{ID: id, Interactable: true} }

func TestPrimaryWins(t *testing.T) {
	t.Parallel()
	primary := &countingGrounder{els: []grounder.Element{el(1)}}
	fallback := &countingGrounder{els: []grounder.Element{el(2)}}
	c := chain.New(primary, fallback)

	got, err := c.Detect(context.Background(), action.Image{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != 1 {
		t.Errorf("got %+v, want primary elements", got)
	}
	if fallback.calls != 0 {
		t.Error("fallback should not run when primary succeeds")
	}
}

func TestFallbackOnError(t *testing.T) {
	t.Parallel()
	primary := &countingGrounder{err: errors.New("ax unavailable")}
	fallback := &countingGrounder{els: []grounder.Element{el(2)}}
	c := chain.New(primary, fallback)

	got, err := c.Detect(context.Background(), action.Image{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != 2 {
		t.Errorf("got %+v, want fallback elements", got)
	}
	if fallback.calls != 1 {
		t.Error("fallback should run on primary error")
	}
}

func TestFallbackOnTooFew(t *testing.T) {
	t.Parallel()
	primary := &countingGrounder{els: nil} // empty
	fallback := &countingGrounder{els: []grounder.Element{el(2)}}
	c := chain.New(primary, fallback) // default minPrimary = 1

	got, err := c.Detect(context.Background(), action.Image{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != 2 {
		t.Errorf("got %+v, want fallback", got)
	}
}

func TestWithMinPrimary(t *testing.T) {
	t.Parallel()
	primary := &countingGrounder{els: []grounder.Element{el(1)}} // 1 element
	fallback := &countingGrounder{els: []grounder.Element{el(2), el(3)}}
	c := chain.New(primary, fallback, chain.WithMinPrimary(2)) // need >=2

	got, _ := c.Detect(context.Background(), action.Image{})
	if len(got) != 2 {
		t.Errorf("primary had too few (<2); should fall back, got %+v", got)
	}
}
