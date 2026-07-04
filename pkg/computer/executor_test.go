package computer_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/computer"
	"github.com/gnanam1990/argus/pkg/computer/fake"
)

func TestExecuteScalesClickCoordinates(t *testing.T) {
	t.Parallel()
	f := fake.New()
	e := computer.NewExecutor(f)
	e.SetScale(2, 2)

	_, err := e.Execute(context.Background(), action.Action{
		Type: action.Click, Button: action.Left, Point: action.Point{X: 10, Y: 20}, Mark: action.NoMark,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	last, _ := f.Last()
	if last.Method != "Click" || last.X != 20 || last.Y != 40 {
		t.Errorf("click at %v (%d,%d), want Click (20,40)", last.Method, last.X, last.Y)
	}
	if last.Clicks != 1 {
		t.Errorf("clicks = %d, want 1", last.Clicks)
	}
}

func TestExecuteResolvesMarkToScaledCenter(t *testing.T) {
	t.Parallel()
	f := fake.New()
	e := computer.NewExecutor(f)
	e.SetScale(2, 2)
	e.SetMarks(map[int]action.Rect{5: {Min: action.Point{X: 0, Y: 0}, Max: action.Point{X: 10, Y: 10}}})

	_, err := e.Execute(context.Background(), action.Action{Type: action.Click, Button: action.Left, Mark: 5})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// center (5,5) scaled by 2 -> (10,10)
	last, _ := f.Last()
	if last.X != 10 || last.Y != 10 {
		t.Errorf("mark click at (%d,%d), want (10,10)", last.X, last.Y)
	}
}

func TestExecuteUnknownMark(t *testing.T) {
	t.Parallel()
	e := computer.NewExecutor(fake.New())
	_, err := e.Execute(context.Background(), action.Action{Type: action.Click, Button: action.Left, Mark: 9})
	if !errors.Is(err, computer.ErrUnknownMark) {
		t.Errorf("err = %v, want ErrUnknownMark", err)
	}
}

func TestExecuteClickCounts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		a      action.Action
		clicks int
	}{
		{"single default", action.Action{Type: action.Click, Button: action.Left, Mark: action.NoMark}, 1},
		{"explicit triple via clicks", action.Action{Type: action.Click, Button: action.Left, Clicks: 3, Mark: action.NoMark}, 3},
		{"double_click", action.Action{Type: action.DoubleClick, Button: action.Left, Mark: action.NoMark}, 2},
		{"triple_click", action.Action{Type: action.TripleClick, Button: action.Left, Mark: action.NoMark}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := fake.New()
			e := computer.NewExecutor(f)
			if _, err := e.Execute(context.Background(), tt.a); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			last, _ := f.Last()
			if last.Clicks != tt.clicks {
				t.Errorf("clicks = %d, want %d", last.Clicks, tt.clicks)
			}
		})
	}
}

func TestExecuteDragScalesPath(t *testing.T) {
	t.Parallel()
	f := fake.New()
	e := computer.NewExecutor(f)
	e.SetScale(2, 2)
	_, err := e.Execute(context.Background(), action.Action{
		Type: action.Drag, Button: action.Left,
		Path: []action.Point{{X: 1, Y: 1}, {X: 2, Y: 2}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	last, _ := f.Last()
	want := []action.Point{{X: 2, Y: 2}, {X: 4, Y: 4}}
	if len(last.Path) != 2 || last.Path[0] != want[0] || last.Path[1] != want[1] {
		t.Errorf("drag path = %v, want %v", last.Path, want)
	}
}

func TestExecuteScrollDeltaNotScaled(t *testing.T) {
	t.Parallel()
	f := fake.New()
	e := computer.NewExecutor(f)
	e.SetScale(2, 2)
	_, err := e.Execute(context.Background(), action.Action{
		Type: action.Scroll, Point: action.Point{X: 5, Y: 5}, DY: -3, Mark: action.NoMark,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	last, _ := f.Last()
	if last.X != 10 || last.Y != 10 {
		t.Errorf("scroll point = (%d,%d), want (10,10)", last.X, last.Y)
	}
	if last.DX != 0 || last.DY != -3 {
		t.Errorf("scroll delta = (%d,%d), want (0,-3) unscaled", last.DX, last.DY)
	}
}

func TestExecuteKeyboardActions(t *testing.T) {
	t.Parallel()

	t.Run("type", func(t *testing.T) {
		t.Parallel()
		f := fake.New()
		e := computer.NewExecutor(f)
		if _, err := e.Execute(context.Background(), action.Action{Type: action.Type, Text: "hi"}); err != nil {
			t.Fatal(err)
		}
		last, _ := f.Last()
		if last.Method != "TypeText" || last.Text != "hi" {
			t.Errorf("got %+v, want TypeText 'hi'", last)
		}
	})

	t.Run("key chord", func(t *testing.T) {
		t.Parallel()
		f := fake.New()
		e := computer.NewExecutor(f)
		if _, err := e.Execute(context.Background(), action.Action{Type: action.Key, Keys: []string{"ctrl", "c"}}); err != nil {
			t.Fatal(err)
		}
		last, _ := f.Last()
		if last.Method != "KeyPress" || len(last.Keys) != 2 {
			t.Errorf("got %+v, want KeyPress ctrl+c", last)
		}
	})

	t.Run("key_down issues one call per key", func(t *testing.T) {
		t.Parallel()
		f := fake.New()
		e := computer.NewExecutor(f)
		if _, err := e.Execute(context.Background(), action.Action{Type: action.KeyDown, Keys: []string{"a", "b"}}); err != nil {
			t.Fatal(err)
		}
		calls := f.Calls()
		if len(calls) != 2 || calls[0].Method != "KeyDown" || calls[1].Method != "KeyDown" {
			t.Errorf("expected two KeyDown calls, got %+v", calls)
		}
	})
}

func TestExecuteMouseVariants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		a      action.Action
		method string
	}{
		{"move", action.Action{Type: action.Move, Button: action.Left, Point: action.Point{X: 3, Y: 4}, Mark: action.NoMark}, "MoveMouse"},
		{"mouse_down", action.Action{Type: action.MouseDown, Button: action.Left, Point: action.Point{X: 3, Y: 4}, Mark: action.NoMark}, "MouseDown"},
		{"mouse_up", action.Action{Type: action.MouseUp, Button: action.Left, Point: action.Point{X: 3, Y: 4}, Mark: action.NoMark}, "MouseUp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := fake.New()
			e := computer.NewExecutor(f)
			e.SetScale(2, 2)
			if _, err := e.Execute(context.Background(), tt.a); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			last, _ := f.Last()
			if last.Method != tt.method || last.X != 6 || last.Y != 8 {
				t.Errorf("got %s (%d,%d), want %s (6,8)", last.Method, last.X, last.Y, tt.method)
			}
		})
	}
}

func TestExecuteKeyUp(t *testing.T) {
	t.Parallel()
	f := fake.New()
	e := computer.NewExecutor(f)
	if _, err := e.Execute(context.Background(), action.Action{Type: action.KeyUp, Keys: []string{"shift", "a"}}); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if len(calls) != 2 || calls[0].Method != "KeyUp" || calls[1].Method != "KeyUp" {
		t.Errorf("expected two KeyUp calls, got %+v", calls)
	}
	if calls[0].Keys[0] != "shift" || calls[1].Keys[0] != "a" {
		t.Errorf("key order = %q, %q", calls[0].Keys, calls[1].Keys)
	}
}

func TestExecuteObservations(t *testing.T) {
	t.Parallel()

	t.Run("screenshot returns image", func(t *testing.T) {
		t.Parallel()
		img := action.Image{MIME: action.MIMEPNG, Data: []byte{1, 2, 3}}
		f := fake.New().WithScreenshot(img, 200, 100)
		e := computer.NewExecutor(f)
		res, err := e.Execute(context.Background(), action.Action{Type: action.Screenshot})
		if err != nil {
			t.Fatal(err)
		}
		if res.Screenshot.MIME != action.MIMEPNG || len(res.Screenshot.Data) != 3 {
			t.Errorf("screenshot result = %+v", res.Screenshot)
		}
	})

	t.Run("cursor position reported in model space", func(t *testing.T) {
		t.Parallel()
		f := fake.New().WithCursor(20, 40)
		e := computer.NewExecutor(f)
		e.SetScale(2, 2)
		res, err := e.Execute(context.Background(), action.Action{Type: action.CursorPosition})
		if err != nil {
			t.Fatal(err)
		}
		// screen (20,40) unscaled by 2 -> model (10,20)
		if res.Cursor != (action.Point{X: 10, Y: 20}) {
			t.Errorf("cursor = %v, want (10,20)", res.Cursor)
		}
	})

	t.Run("terminate", func(t *testing.T) {
		t.Parallel()
		f := fake.New()
		e := computer.NewExecutor(f)
		res, err := e.Execute(context.Background(), action.Action{Type: action.Terminate})
		if err != nil {
			t.Fatal(err)
		}
		if !res.Terminated {
			t.Error("Terminated = false, want true")
		}
		if len(f.Calls()) != 0 {
			t.Error("terminate must not call the computer")
		}
	})
}

func TestExecuteWait(t *testing.T) {
	t.Parallel()

	t.Run("returns after duration", func(t *testing.T) {
		t.Parallel()
		f := fake.New()
		e := computer.NewExecutor(f)
		start := time.Now()
		if _, err := e.Execute(context.Background(), action.Action{Type: action.Wait, Dur: 5 * time.Millisecond}); err != nil {
			t.Fatal(err)
		}
		if time.Since(start) < 3*time.Millisecond {
			t.Error("wait returned too early")
		}
		if len(f.Calls()) != 0 {
			t.Error("wait must not call the computer")
		}
	})

	t.Run("honors context cancellation", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		e := computer.NewExecutor(fake.New())
		_, err := e.Execute(ctx, action.Action{Type: action.Wait, Dur: time.Hour})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	})
}

func TestExecuteCapabilityGate(t *testing.T) {
	t.Parallel()

	t.Run("gated action denied by default", func(t *testing.T) {
		t.Parallel()
		e := computer.NewExecutor(fake.New())
		_, err := e.Execute(context.Background(), action.Action{Type: action.RunCommand, Text: "ls"})
		if !errors.Is(err, computer.ErrCapabilityDenied) {
			t.Errorf("err = %v, want ErrCapabilityDenied", err)
		}
	})

	t.Run("allowlisted gated action is unsupported by a bare computer", func(t *testing.T) {
		t.Parallel()
		e := computer.NewExecutor(fake.New(), computer.WithCapabilities(action.RunCommand))
		_, err := e.Execute(context.Background(), action.Action{Type: action.RunCommand, Text: "ls"})
		if !errors.Is(err, computer.ErrUnsupported) {
			t.Errorf("err = %v, want ErrUnsupported", err)
		}
	})

	t.Run("non-gated action always allowed", func(t *testing.T) {
		t.Parallel()
		e := computer.NewExecutor(fake.New())
		if !e.Allowed(action.Click) {
			t.Error("Click should always be allowed")
		}
		if e.Allowed(action.RunCommand) {
			t.Error("RunCommand should be denied by default")
		}
	})
}

func TestExecuteValidatesFirst(t *testing.T) {
	t.Parallel()
	f := fake.New()
	e := computer.NewExecutor(f)
	// Type with empty text fails Validate before any driver call.
	_, err := e.Execute(context.Background(), action.Action{Type: action.Type})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if len(f.Calls()) != 0 {
		t.Error("invalid action must not reach the computer")
	}
}

func TestExecutePropagatesDriverError(t *testing.T) {
	t.Parallel()
	boom := errors.New("driver boom")
	f := fake.New().WithError(boom)
	e := computer.NewExecutor(f)
	_, err := e.Execute(context.Background(), action.Action{Type: action.Click, Button: action.Left, Mark: action.NoMark})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want driver boom", err)
	}
}
