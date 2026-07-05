package wayland

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
)

// fakeRunner records every command it's asked to run and replays canned
// stdout/err keyed by the command name.
type fakeRunner struct {
	calls [][]string
	out   map[string][]byte
	err   map[string]error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if f.err != nil {
		if e, ok := f.err[name]; ok {
			return nil, e
		}
	}
	if f.out != nil {
		if o, ok := f.out[name]; ok {
			return o, nil
		}
	}
	return nil, nil
}

func (f *fakeRunner) last() []string {
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
}

func joined(argv []string) string { return strings.Join(argv, " ") }

func newTest(fr *fakeRunner, opts ...Option) *Driver {
	return New(append([]Option{WithRunner(fr)}, opts...)...)
}

func TestMoveMouseAbsolute(t *testing.T) {
	fr := &fakeRunner{}
	if err := newTest(fr).MoveMouse(context.Background(), 300, 450); err != nil {
		t.Fatal(err)
	}
	if got := joined(fr.last()); got != "ydotool mousemove --absolute -x 300 -y 450" {
		t.Errorf("argv = %q", got)
	}
}

func TestClickButtonsAndRepeat(t *testing.T) {
	cases := []struct {
		b        action.Button
		wantCode string
	}{
		{action.Left, "0xC0"},
		{action.Right, "0xC1"},
		{action.Middle, "0xC2"},
	}
	for _, c := range cases {
		fr := &fakeRunner{}
		if err := newTest(fr).Click(context.Background(), 10, 20, c.b, 1); err != nil {
			t.Fatal(err)
		}
		// First call moves, second clicks.
		if got := joined(fr.calls[0]); got != "ydotool mousemove --absolute -x 10 -y 20" {
			t.Errorf("move argv = %q", got)
		}
		if got := joined(fr.last()); got != "ydotool click "+c.wantCode {
			t.Errorf("click argv = %q, want code %s", got, c.wantCode)
		}
	}

	// A double click issues two click events.
	fr := &fakeRunner{}
	if err := newTest(fr).Click(context.Background(), 0, 0, action.Left, 2); err != nil {
		t.Fatal(err)
	}
	clicks := 0
	for _, c := range fr.calls {
		if len(c) >= 2 && c[1] == "click" {
			clicks++
		}
	}
	if clicks != 2 {
		t.Errorf("got %d click events, want 2", clicks)
	}
}

func TestMouseDownUpCodes(t *testing.T) {
	fr := &fakeRunner{}
	d := newTest(fr)
	if err := d.MouseDown(context.Background(), 5, 6, action.Left); err != nil {
		t.Fatal(err)
	}
	if got := joined(fr.last()); got != "ydotool click 0x40" {
		t.Errorf("down argv = %q, want press code 0x40", got)
	}
	if err := d.MouseUp(context.Background(), 5, 6, action.Right); err != nil {
		t.Fatal(err)
	}
	if got := joined(fr.last()); got != "ydotool click 0x81" {
		t.Errorf("up argv = %q, want right-release code 0x81", got)
	}
}

func TestKeyChordPressReleaseOrder(t *testing.T) {
	fr := &fakeRunner{}
	// ctrl+shift+s → ctrl(29), shift(42), s(31): down in order, up reversed.
	if err := newTest(fr).KeyPress(context.Background(), "ctrl", "shift", "s"); err != nil {
		t.Fatal(err)
	}
	want := "ydotool key 29:1 42:1 31:1 31:0 42:0 29:0"
	if got := joined(fr.last()); got != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
}

func TestKeyUnknownErrors(t *testing.T) {
	fr := &fakeRunner{}
	if err := newTest(fr).KeyPress(context.Background(), "ctrl", "nope"); err == nil {
		t.Error("unknown key should error, not silently drop")
	}
}

func TestTypeText(t *testing.T) {
	fr := &fakeRunner{}
	if err := newTest(fr).TypeText(context.Background(), "hi there"); err != nil {
		t.Fatal(err)
	}
	want := []string{"ydotool", "type", "hi there"}
	if joined(fr.last()) != joined(want) {
		t.Errorf("argv = %v, want %v", fr.last(), want)
	}
}

func TestScrollNegatesBothAxes(t *testing.T) {
	fr := &fakeRunner{}
	// canonical down+right (dy=3, dx=2) → ydotool wheel gets negated.
	if err := newTest(fr).Scroll(context.Background(), 100, 100, 2, 3); err != nil {
		t.Fatal(err)
	}
	want := "ydotool mousemove --wheel -x -2 -y -3"
	if got := joined(fr.last()); got != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
}

func TestScreenshotUsesStdoutTool(t *testing.T) {
	png := makePNG(t, 320, 240)
	fr := &fakeRunner{out: map[string][]byte{"grim": png}}
	img, err := newTest(fr).Screenshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(img.Data, png) || img.MIME != action.MIMEPNG {
		t.Errorf("screenshot did not return grim's PNG")
	}
	if got := joined(fr.calls[0]); got != "grim -" {
		t.Errorf("first capture cmd = %q, want grim -", got)
	}
}

func TestScreenshotFallsBackThroughChain(t *testing.T) {
	png := makePNG(t, 100, 100)
	// grim fails; gnome-screenshot writes a file we serve via readFile.
	fr := &fakeRunner{err: map[string]error{"grim": errors.New("not found")}}
	d := New(
		WithRunner(fr),
		func(d *Driver) {
			d.tempFile = func() (string, func(), error) { return "/tmp/x.png", func() {}, nil }
			d.readFile = func(string) ([]byte, error) { return png, nil }
		},
	)
	img, err := d.Screenshot(context.Background())
	if err != nil {
		t.Fatalf("expected fallback to succeed: %v", err)
	}
	if !bytes.Equal(img.Data, png) {
		t.Error("fallback did not return the file-based tool's PNG")
	}
	// gnome-screenshot's {} placeholder must be substituted with the temp path.
	var gnome []string
	for _, c := range fr.calls {
		if c[0] == "gnome-screenshot" {
			gnome = c
		}
	}
	if joined(gnome) != "gnome-screenshot -f /tmp/x.png" {
		t.Errorf("gnome-screenshot argv = %v, want the temp path substituted", gnome)
	}
}

func TestScreenSizeDecodesScreenshot(t *testing.T) {
	fr := &fakeRunner{out: map[string][]byte{"grim": makePNG(t, 2560, 1440)}}
	w, h, err := newTest(fr).ScreenSize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if w != 2560 || h != 1440 {
		t.Errorf("size = %dx%d, want 2560x1440", w, h)
	}
}

func TestCursorPositionUnsupported(t *testing.T) {
	if _, _, err := newTest(&fakeRunner{}).CursorPosition(context.Background()); err == nil {
		t.Error("CursorPosition should report unsupported on Wayland")
	}
}

func TestWithYdotoolOverride(t *testing.T) {
	fr := &fakeRunner{}
	d := newTest(fr, WithYdotool("/opt/ydotool"))
	if err := d.MoveMouse(context.Background(), 1, 2); err != nil {
		t.Fatal(err)
	}
	if fr.last()[0] != "/opt/ydotool" {
		t.Errorf("binary = %q, want the override", fr.last()[0])
	}
}

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
