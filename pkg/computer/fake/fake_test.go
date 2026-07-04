package fake

import (
	"context"
	"errors"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
)

func TestRecordsCallsAndDefaults(t *testing.T) {
	t.Parallel()
	f := New()
	ctx := context.Background()

	img, err := f.Screenshot(ctx)
	if err != nil || img.Empty() {
		t.Fatalf("Screenshot = %v, %v", img, err)
	}
	w, h, err := f.ScreenSize(ctx)
	if err != nil || w != 100 || h != 100 {
		t.Fatalf("ScreenSize = %d,%d,%v", w, h, err)
	}
	if err := f.Click(ctx, 5, 6, action.Right, 2); err != nil {
		t.Fatal(err)
	}

	last, ok := f.Last()
	if !ok || last.Method != "Click" || last.X != 5 || last.Y != 6 || last.Button != action.Right || last.Clicks != 2 {
		t.Errorf("last = %+v", last)
	}
	if len(f.Calls()) != 3 {
		t.Errorf("Calls len = %d, want 3", len(f.Calls()))
	}
}

func TestLastEmpty(t *testing.T) {
	t.Parallel()
	if _, ok := New().Last(); ok {
		t.Error("Last on fresh fake should be ok=false")
	}
}

func TestWithScreenshotAndCursor(t *testing.T) {
	t.Parallel()
	img := action.Image{MIME: action.MIMEJPEG, Data: []byte{1, 2}}
	f := New().WithScreenshot(img, 640, 480).WithCursor(11, 22)

	got, _ := f.Screenshot(context.Background())
	if got.MIME != action.MIMEJPEG {
		t.Errorf("mime = %s", got.MIME)
	}
	w, h, _ := f.ScreenSize(context.Background())
	if w != 640 || h != 480 {
		t.Errorf("size = %d,%d", w, h)
	}
	x, y, _ := f.CursorPosition(context.Background())
	if x != 11 || y != 22 {
		t.Errorf("cursor = %d,%d", x, y)
	}
}

func TestWithErrorPropagates(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	f := New().WithError(boom)
	ctx := context.Background()

	if _, err := f.Screenshot(ctx); !errors.Is(err, boom) {
		t.Errorf("Screenshot err = %v", err)
	}
	if err := f.MoveMouse(ctx, 1, 1); !errors.Is(err, boom) {
		t.Errorf("MoveMouse err = %v", err)
	}
	if _, _, err := f.ScreenSize(ctx); !errors.Is(err, boom) {
		t.Errorf("ScreenSize err = %v", err)
	}
}

func TestCloseMarksClosed(t *testing.T) {
	t.Parallel()
	f := New()
	if f.Closed() {
		t.Error("fresh fake reports closed")
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if !f.Closed() {
		t.Error("Close did not mark closed")
	}
	last, _ := f.Last()
	if last.Method != "Close" {
		t.Errorf("last method = %s, want Close", last.Method)
	}
}

func TestCallsReturnsCopy(t *testing.T) {
	t.Parallel()
	f := New()
	_ = f.MoveMouse(context.Background(), 1, 2)
	calls := f.Calls()
	calls[0].X = 999 // mutate the returned slice
	if again := f.Calls(); again[0].X != 1 {
		t.Error("Calls() must return an independent copy")
	}
}

func TestDragAndKeyPressCopyInputs(t *testing.T) {
	t.Parallel()
	f := New()
	ctx := context.Background()

	path := []action.Point{{X: 1, Y: 1}, {X: 2, Y: 2}}
	_ = f.Drag(ctx, path, action.Left)
	path[0].X = 999 // mutate caller's slice after the call
	keys := []string{"ctrl", "v"}
	_ = f.KeyPress(ctx, keys...)
	keys[0] = "MUT"

	calls := f.Calls()
	if calls[0].Path[0].X != 1 {
		t.Error("Drag must copy the path slice")
	}
	if calls[1].Keys[0] != "ctrl" {
		t.Error("KeyPress must copy the keys slice")
	}
}
