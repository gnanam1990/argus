package remote_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gnanam1990/argus/internal/driver/remote"
	"github.com/gnanam1990/argus/internal/guest"
	"github.com/gnanam1990/argus/internal/transport"
	"github.com/gnanam1990/argus/pkg/action"
	compfake "github.com/gnanam1990/argus/pkg/computer/fake"
)

// stack wires remote -> transport -> guest -> fake computer, the real path.
func stack(t *testing.T, f *compfake.Computer) *remote.Computer {
	t.Helper()
	srv := httptest.NewServer(transport.NewServer(guest.New(f)))
	t.Cleanup(srv.Close)
	return remote.New(srv.URL)
}

func TestRemoteObservations(t *testing.T) {
	t.Parallel()
	f := compfake.New().
		WithScreenshot(action.Image{MIME: action.MIMEPNG, Data: []byte{1, 2, 3}}, 800, 600).
		WithCursor(11, 22)
	c := stack(t, f)
	ctx := context.Background()

	img, err := c.Screenshot(ctx)
	if err != nil || img.MIME != action.MIMEPNG || len(img.Data) != 3 {
		t.Fatalf("screenshot = %+v, %v", img, err)
	}
	w, h, err := c.ScreenSize(ctx)
	if err != nil || w != 800 || h != 600 {
		t.Errorf("size = %d,%d,%v", w, h, err)
	}
	x, y, err := c.CursorPosition(ctx)
	if err != nil || x != 11 || y != 22 {
		t.Errorf("cursor = %d,%d,%v", x, y, err)
	}
}

func TestRemoteInput(t *testing.T) {
	t.Parallel()
	f := compfake.New()
	c := stack(t, f)
	ctx := context.Background()

	if err := c.Click(ctx, 10, 20, action.Right, 2); err != nil {
		t.Fatal(err)
	}
	last, _ := f.Last()
	if last.Method != "Click" || last.X != 10 || last.Y != 20 || last.Button != action.Right || last.Clicks != 2 {
		t.Errorf("click forwarded as %+v", last)
	}

	if err := c.TypeText(ctx, "hi"); err != nil {
		t.Fatal(err)
	}
	if last, _ = f.Last(); last.Method != "TypeText" || last.Text != "hi" {
		t.Errorf("type forwarded as %+v", last)
	}

	if err := c.Drag(ctx, []action.Point{{X: 0, Y: 0}, {X: 5, Y: 6}}, action.Left); err != nil {
		t.Fatal(err)
	}
	if last, _ = f.Last(); last.Method != "Drag" || len(last.Path) != 2 || last.Path[1] != (action.Point{X: 5, Y: 6}) {
		t.Errorf("drag forwarded as %+v", last)
	}

	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if !f.Closed() {
		t.Error("close not forwarded")
	}
}

func TestRemoteAllInputMethods(t *testing.T) {
	t.Parallel()
	f := compfake.New()
	c := stack(t, f)
	ctx := context.Background()

	steps := []struct {
		call   func() error
		method string
	}{
		{func() error { return c.MoveMouse(ctx, 1, 2) }, "MoveMouse"},
		{func() error { return c.MouseDown(ctx, 3, 4, action.Left) }, "MouseDown"},
		{func() error { return c.MouseUp(ctx, 3, 4, action.Left) }, "MouseUp"},
		{func() error { return c.Scroll(ctx, 5, 5, 0, 3) }, "Scroll"},
		{func() error { return c.KeyPress(ctx, "ctrl", "c") }, "KeyPress"},
		{func() error { return c.KeyDown(ctx, "shift") }, "KeyDown"},
		{func() error { return c.KeyUp(ctx, "shift") }, "KeyUp"},
	}
	for _, s := range steps {
		if err := s.call(); err != nil {
			t.Fatalf("%s: %v", s.method, err)
		}
		if last, _ := f.Last(); last.Method != s.method {
			t.Errorf("last method = %s, want %s", last.Method, s.method)
		}
	}
}

func TestRemoteTransportError(t *testing.T) {
	t.Parallel()
	// Point at a closed server → network error.
	srv := httptest.NewServer(transport.NewServer(guest.New(compfake.New())))
	url := srv.URL
	srv.Close()
	c := remote.New(url)
	if _, err := c.Screenshot(context.Background()); err == nil {
		t.Error("expected a transport error against a closed server")
	}
}
