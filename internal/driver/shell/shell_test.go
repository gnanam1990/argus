package shell

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
)

type fakeRunner struct {
	calls   [][]string
	outputs map[string][]byte
	err     error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if f.err != nil {
		return nil, f.err
	}
	return f.outputs[name], nil
}

func (f *fakeRunner) last() []string {
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
}

func argv(name string, rest ...string) []string { return append([]string{name}, rest...) }

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func newDriver(f *fakeRunner) *Driver { return New(WithRunner(f)) }

func ctx() context.Context { return context.Background() }

func TestClickArgv(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{}
	d := newDriver(f)

	if err := d.Click(ctx(), 10, 20, action.Left, 1); err != nil {
		t.Fatal(err)
	}
	want := argv("xdotool", "mousemove", "10", "20", "click", "1")
	if !eq(f.last(), want) {
		t.Errorf("click argv = %v, want %v", f.last(), want)
	}

	if err := d.Click(ctx(), 5, 6, action.Right, 3); err != nil {
		t.Fatal(err)
	}
	want = argv("xdotool", "mousemove", "5", "6", "click", "--repeat", "3", "3")
	if !eq(f.last(), want) {
		t.Errorf("triple right-click argv = %v, want %v", f.last(), want)
	}
}

func TestMouseAndDrag(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{}
	d := newDriver(f)

	_ = d.MoveMouse(ctx(), 1, 2)
	if !eq(f.last(), argv("xdotool", "mousemove", "1", "2")) {
		t.Errorf("move argv = %v", f.last())
	}
	_ = d.MouseDown(ctx(), 3, 4, action.Left)
	if !eq(f.last(), argv("xdotool", "mousemove", "3", "4", "mousedown", "1")) {
		t.Errorf("mousedown argv = %v", f.last())
	}
	_ = d.Drag(ctx(), []action.Point{{X: 0, Y: 0}, {X: 10, Y: 10}}, action.Left)
	want := argv("xdotool", "mousemove", "0", "0", "mousedown", "1", "mousemove", "10", "10", "mouseup", "1")
	if !eq(f.last(), want) {
		t.Errorf("drag argv = %v, want %v", f.last(), want)
	}
}

func TestScrollArgv(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{}
	d := newDriver(f)

	// down 3 → move then wheel button 5 repeat 3
	_ = d.Scroll(ctx(), 5, 5, 0, 3)
	if !eq(f.calls[0], argv("xdotool", "mousemove", "5", "5")) {
		t.Errorf("scroll move argv = %v", f.calls[0])
	}
	if !eq(f.calls[1], argv("xdotool", "click", "--repeat", "3", "5")) {
		t.Errorf("scroll wheel argv = %v", f.calls[1])
	}

	// up 1 → button 4, no repeat
	f2 := &fakeRunner{}
	d2 := newDriver(f2)
	_ = d2.Scroll(ctx(), 0, 0, 0, -1)
	if !eq(f2.calls[1], argv("xdotool", "click", "4")) {
		t.Errorf("scroll up argv = %v", f2.calls[1])
	}
}

func TestKeyboardArgv(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{}
	d := newDriver(f)

	_ = d.TypeText(ctx(), "hello world")
	if !eq(f.last(), argv("xdotool", "type", "--", "hello world")) {
		t.Errorf("type argv = %v", f.last())
	}
	// key mapping: enter → Return
	_ = d.KeyPress(ctx(), "ctrl", "enter")
	if !eq(f.last(), argv("xdotool", "key", "ctrl+Return")) {
		t.Errorf("key argv = %v", f.last())
	}
	_ = d.KeyDown(ctx(), "shift")
	if !eq(f.last(), argv("xdotool", "keydown", "shift")) {
		t.Errorf("keydown argv = %v", f.last())
	}
}

func TestMouseUpKeyUpAndHorizontalScroll(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{}
	d := newDriver(f)

	_ = d.MouseUp(ctx(), 3, 4, action.Middle)
	if !eq(f.last(), argv("xdotool", "mousemove", "3", "4", "mouseup", "2")) {
		t.Errorf("mouseup argv = %v", f.last())
	}
	_ = d.KeyUp(ctx(), "esc")
	if !eq(f.last(), argv("xdotool", "keyup", "Escape")) {
		t.Errorf("keyup argv = %v", f.last())
	}

	// horizontal scroll right 2 → button 7 repeat 2 (after the move)
	f2 := &fakeRunner{}
	d2 := newDriver(f2)
	_ = d2.Scroll(ctx(), 1, 1, 2, 0)
	if !eq(f2.calls[1], argv("xdotool", "click", "--repeat", "2", "7")) {
		t.Errorf("scroll right argv = %v", f2.calls[1])
	}
	// left 1 → button 6
	f3 := &fakeRunner{}
	d3 := newDriver(f3)
	_ = d3.Scroll(ctx(), 0, 0, -1, 0)
	if !eq(f3.calls[1], argv("xdotool", "click", "6")) {
		t.Errorf("scroll left argv = %v", f3.calls[1])
	}
}

func TestScreenshot(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{outputs: map[string][]byte{"maim": {0x89, 'P', 'N', 'G'}}}
	d := newDriver(f)
	img, err := d.Screenshot(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if img.MIME != action.MIMEPNG || len(img.Data) != 4 {
		t.Errorf("screenshot = %+v", img)
	}
	if !eq(f.last(), argv("maim")) {
		t.Errorf("screenshot argv = %v", f.last())
	}
}

func TestScreenSize(t *testing.T) {
	t.Parallel()
	fixture := "Screen 0: minimum 320 x 200, current 1920 x 1080, maximum 16384 x 16384\n"
	f := &fakeRunner{outputs: map[string][]byte{"xrandr": []byte(fixture)}}
	d := newDriver(f)
	w, h, err := d.ScreenSize(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if w != 1920 || h != 1080 {
		t.Errorf("size = %dx%d, want 1920x1080", w, h)
	}
}

func TestCursorPosition(t *testing.T) {
	t.Parallel()
	fixture := "X=42\nY=99\nSCREEN=0\nWINDOW=123\n"
	f := &fakeRunner{outputs: map[string][]byte{"xdotool": []byte(fixture)}}
	d := newDriver(f)
	x, y, err := d.CursorPosition(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if x != 42 || y != 99 {
		t.Errorf("cursor = (%d,%d), want (42,99)", x, y)
	}
}

func TestErrorPropagation(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{err: errors.New("boom")}
	d := newDriver(f)
	if err := d.Click(ctx(), 1, 1, action.Left, 1); err == nil {
		t.Error("expected error from runner")
	} else if !strings.Contains(err.Error(), "xdotool") {
		t.Errorf("error not wrapped: %v", err)
	}
}

func TestCloseNoop(t *testing.T) {
	t.Parallel()
	if err := New().Close(); err != nil {
		t.Errorf("Close = %v", err)
	}
}

func TestNoRealBinariesConfigurable(t *testing.T) {
	t.Parallel()
	// A custom screenshot command is honored.
	f := &fakeRunner{outputs: map[string][]byte{"scrot": {1}}}
	d := New(WithRunner(f), WithScreenshotCommand("scrot", "-o", "-"))
	if _, err := d.Screenshot(ctx()); err != nil {
		t.Fatal(err)
	}
	if !eq(f.last(), argv("scrot", "-o", "-")) {
		t.Errorf("custom screenshot argv = %v", f.last())
	}
}
