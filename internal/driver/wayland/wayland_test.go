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
	// Force the ydotool pointer + a no-op settle so tests are deterministic and
	// don't sleep, regardless of the host's compositor env. Tests that exercise
	// the compositor path pass withPointer(...) after these.
	base := []Option{WithRunner(fr), withPointer(pointerYdotool), withSettle(func() {})}
	return New(append(base, opts...)...)
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
	// "--" ends option parsing so leading-dash text is typed, not read as flags.
	if err := newTest(fr).TypeText(context.Background(), "-hi there"); err != nil {
		t.Fatal(err)
	}
	want := []string{"ydotool", "type", "--", "-hi there"}
	if joined(fr.last()) != joined(want) {
		t.Errorf("argv = %v, want %v", fr.last(), want)
	}
}

// errRunner fails commands whose joined argv matches a predicate, with a given
// error; everything else succeeds.
type errRunner struct {
	fakeRunner
	failWhen func(argv string) error
}

func (e *errRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	e.calls = append(e.calls, append([]string{name}, args...))
	if e.failWhen != nil {
		if err := e.failWhen(strings.Join(append([]string{name}, args...), " ")); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// TestDaemonErrorIsActionable locks in the fix for the opaque "exit status 2":
// a socket/daemon failure must name ydotoold and the socket-ownership fix
// instead of a bare exit status.
func TestDaemonErrorIsActionable(t *testing.T) {
	fr := &errRunner{failWhen: func(argv string) error {
		if strings.HasPrefix(argv, "ydotool") {
			return errors.New("failed to connect socket '/tmp/.ydotool_socket': Permission denied")
		}
		return nil
	}}
	err := newTest(&fr.fakeRunner, WithRunner(fr)).MoveMouse(context.Background(), 10, 10)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"ydotoold", "--socket-own", "Permission denied"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
	// A daemon failure must abort, not be retried across syntax variants.
	if len(fr.calls) != 1 {
		t.Errorf("got %d attempts, want 1 (no syntax retries on a daemon error)", len(fr.calls))
	}
}

// TestMoveSyntaxFallbackAndCache verifies the version-adaptive mousemove: when
// the modern long-flag syntax is rejected (usage error), the driver falls back
// to the next shape and caches it, so later moves go straight to the working
// syntax.
func TestMoveSyntaxFallbackAndCache(t *testing.T) {
	fr := &errRunner{failWhen: func(argv string) error {
		if strings.Contains(argv, "--absolute -x") {
			return errors.New("Usage: mousemove [OPTION]...") // old/short-flag build
		}
		return nil
	}}
	d := newTest(&fr.fakeRunner, WithRunner(fr))

	if err := d.MoveMouse(context.Background(), 7, 9); err != nil {
		t.Fatalf("fallback should succeed: %v", err)
	}
	if got := joined(fr.last()); got != "ydotool mousemove -a -x 7 -y 9" {
		t.Errorf("fallback argv = %q, want the short-flag shape", got)
	}

	// Second move: cached — exactly one call, straight to the working shape.
	n := len(fr.calls)
	if err := d.MoveMouse(context.Background(), 1, 2); err != nil {
		t.Fatal(err)
	}
	if len(fr.calls) != n+1 {
		t.Errorf("cached move made %d calls, want 1", len(fr.calls)-n)
	}
	if got := joined(fr.last()); got != "ydotool mousemove -a -x 1 -y 2" {
		t.Errorf("cached argv = %q, want the remembered short-flag shape", got)
	}
}

// TestAllSyntaxesFailingNamesTheRequirement: when every known shape is
// rejected, the error should say what's needed rather than dumping one opaque
// exit status.
func TestAllSyntaxesFailingNamesTheRequirement(t *testing.T) {
	fr := &errRunner{failWhen: func(argv string) error {
		if strings.HasPrefix(argv, "ydotool mousemove") {
			return errors.New("unrecognized option")
		}
		return nil
	}}
	err := newTest(&fr.fakeRunner, WithRunner(fr)).MoveMouse(context.Background(), 1, 1)
	if err == nil || !strings.Contains(err.Error(), "ydotool >= 1.0") {
		t.Errorf("error = %v, want it to name the ydotool >= 1.0 requirement", err)
	}
}

// TestWheelSyntaxFallback mirrors the move fallback for wheel scrolling.
func TestWheelSyntaxFallback(t *testing.T) {
	fr := &errRunner{failWhen: func(argv string) error {
		if strings.Contains(argv, "--wheel") {
			return errors.New("Usage: mousemove [OPTION]...")
		}
		return nil
	}}
	d := newTest(&fr.fakeRunner, WithRunner(fr))
	if err := d.Scroll(context.Background(), 50, 50, 0, 3); err != nil {
		t.Fatalf("wheel fallback should succeed: %v", err)
	}
	if got := joined(fr.last()); got != "ydotool mousemove -w -x 0 -y -3" {
		t.Errorf("wheel argv = %q, want the short-flag shape with negated dy", got)
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

func TestDetectPointer(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	cases := []struct {
		name string
		m    map[string]string
		want pointerKind
	}{
		{"hyprland", map[string]string{"HYPRLAND_INSTANCE_SIGNATURE": "abc"}, pointerHyprland},
		{"sway", map[string]string{"SWAYSOCK": "/run/sway.sock"}, pointerSway},
		{"none", map[string]string{}, pointerYdotool},
		{"override", map[string]string{"HYPRLAND_INSTANCE_SIGNATURE": "abc", "ARGUS_WL_POINTER": "ydotool"}, pointerYdotool},
	}
	for _, c := range cases {
		if got := detectPointer(env(c.m)); got != c.want {
			t.Errorf("%s: detectPointer = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestHyprlandExactPositioning(t *testing.T) {
	// scale 1.0: click coords pass straight through to hyprctl movecursor.
	fr := &fakeRunner{out: map[string][]byte{
		"hyprctl": []byte(`[{"focused":true,"scale":1.0}]`),
	}}
	d := newTest(fr, withPointer(pointerHyprland))
	if err := d.Click(context.Background(), 840, 300, action.Left, 1); err != nil {
		t.Fatal(err)
	}
	var moved, clicked bool
	for _, c := range fr.calls {
		if c[0] == "hyprctl" && len(c) >= 5 && c[1] == "dispatch" && c[2] == "movecursor" && c[3] == "840" && c[4] == "300" {
			moved = true
		}
		if c[0] == "ydotool" && len(c) >= 2 && c[1] == "click" {
			clicked = true
		}
	}
	if !moved {
		t.Errorf("expected `hyprctl dispatch movecursor 840 300`, calls=%v", fr.calls)
	}
	if !clicked {
		t.Error("expected the button press to still go through ydotool")
	}
}

func TestHyprlandScaleMapsCoords(t *testing.T) {
	// scale 2.0 (HiDPI): a screenshot-pixel (1000,500) must map to logical
	// (500,250) before hyprctl, or the click lands at 2x the intended offset.
	fr := &fakeRunner{out: map[string][]byte{
		"hyprctl": []byte(`[{"focused":true,"scale":2.0}]`),
	}}
	d := newTest(fr, withPointer(pointerHyprland))
	if err := d.MoveMouse(context.Background(), 1000, 500); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range fr.calls {
		if c[0] == "hyprctl" && len(c) >= 3 && c[2] == "movecursor" {
			got = c
		}
	}
	if len(got) < 5 || got[3] != "500" || got[4] != "250" {
		t.Errorf("movecursor args = %v, want logical 500 250 (scale 2.0)", got)
	}
}

func TestPointerFallsBackToYdotoolOnCompositorError(t *testing.T) {
	// hyprctl fails at runtime → the move must fall back to ydotool, not error.
	fr := &errRunner{failWhen: func(argv string) error {
		if strings.HasPrefix(argv, "hyprctl") {
			return errors.New("hyprctl: connection failed")
		}
		return nil
	}}
	d := newTest(&fr.fakeRunner, WithRunner(fr), withPointer(pointerHyprland))
	if err := d.MoveMouse(context.Background(), 10, 20); err != nil {
		t.Fatalf("should fall back, not fail: %v", err)
	}
	sawYdotool := false
	for _, c := range fr.calls {
		if c[0] == "ydotool" && len(c) >= 2 && c[1] == "mousemove" {
			sawYdotool = true
		}
	}
	if !sawYdotool {
		t.Errorf("expected ydotool fallback after hyprctl failure, calls=%v", fr.calls)
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
