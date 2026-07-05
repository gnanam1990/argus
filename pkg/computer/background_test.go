package computer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/computer"
	"github.com/gnanam1990/argus/pkg/computer/fake"
)

// bgFake is a fake driver that additionally implements BackgroundClicker.
type bgFake struct {
	*fake.Computer
	result  error // what BackgroundClick returns
	bgCalls int
}

func (b *bgFake) BackgroundClick(_ context.Context, _, _ int) error {
	b.bgCalls++
	return b.result
}

func cursorClicks(f *fake.Computer) int {
	n := 0
	for _, c := range f.Calls() {
		if c.Method == "Click" {
			n++
		}
	}
	return n
}

func leftClick(x, y int) action.Action {
	return action.Action{Type: action.Click, Point: action.Point{X: x, Y: y}, Button: action.Left, Mark: action.NoMark}
}

func TestBackgroundClickSucceedsNoCursor(t *testing.T) {
	t.Parallel()
	f := &bgFake{Computer: fake.New()}
	e := computer.NewExecutor(f, computer.WithBackgroundDispatch())

	if _, err := e.Execute(context.Background(), leftClick(5, 5)); err != nil {
		t.Fatal(err)
	}
	if f.bgCalls != 1 {
		t.Errorf("background click calls = %d, want 1", f.bgCalls)
	}
	if cursorClicks(f.Computer) != 0 {
		t.Error("a successful background click must not move the cursor")
	}
}

func TestBackgroundClickNoTargetFallsBackToCursor(t *testing.T) {
	t.Parallel()
	f := &bgFake{Computer: fake.New(), result: computer.ErrNoBackgroundTarget}
	e := computer.NewExecutor(f, computer.WithBackgroundDispatch())

	if _, err := e.Execute(context.Background(), leftClick(5, 5)); err != nil {
		t.Fatal(err)
	}
	if f.bgCalls != 1 {
		t.Errorf("background attempts = %d, want 1", f.bgCalls)
	}
	if cursorClicks(f.Computer) != 1 {
		t.Error("no background target must fall back to a cursor click")
	}
}

func TestBackgroundClickRealErrorSurfaced(t *testing.T) {
	t.Parallel()
	boom := errors.New("ax: permission denied")
	f := &bgFake{Computer: fake.New(), result: boom}
	e := computer.NewExecutor(f, computer.WithBackgroundDispatch())

	if _, err := e.Execute(context.Background(), leftClick(5, 5)); err == nil {
		t.Fatal("a real background-click error must surface, not fall back silently")
	}
	if cursorClicks(f.Computer) != 0 {
		t.Error("a real error must not also fire a cursor click")
	}
}

func TestBackgroundDispatchOffAlwaysCursor(t *testing.T) {
	t.Parallel()
	f := &bgFake{Computer: fake.New()}
	e := computer.NewExecutor(f) // no WithBackgroundDispatch

	if _, err := e.Execute(context.Background(), leftClick(5, 5)); err != nil {
		t.Fatal(err)
	}
	if f.bgCalls != 0 {
		t.Error("background dispatch off: must not attempt a background click")
	}
	if cursorClicks(f.Computer) != 1 {
		t.Error("background dispatch off: must use the cursor")
	}
}

func TestBackgroundDispatchOnlySingleLeftClick(t *testing.T) {
	t.Parallel()
	f := &bgFake{Computer: fake.New()}
	e := computer.NewExecutor(f, computer.WithBackgroundDispatch())

	// Double-click and right-click must not use background dispatch.
	_, _ = e.Execute(context.Background(), action.Action{Type: action.DoubleClick, Point: action.Point{X: 1, Y: 1}, Button: action.Left, Mark: action.NoMark})
	_, _ = e.Execute(context.Background(), action.Action{Type: action.Click, Point: action.Point{X: 1, Y: 1}, Button: action.Right, Mark: action.NoMark})
	if f.bgCalls != 0 {
		t.Errorf("background attempts = %d; only single left clicks qualify", f.bgCalls)
	}
	if cursorClicks(f.Computer) != 2 {
		t.Errorf("cursor clicks = %d, want 2 (double + right)", cursorClicks(f.Computer))
	}
}
