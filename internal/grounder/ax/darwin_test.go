package ax_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/gnanam1990/argus/internal/grounder/ax"
	"github.com/gnanam1990/argus/pkg/action"
)

// fakeRunner is this package's equivalent of internal/driver/shell's
// fakeRunner: it records argv and the context it was called with, and
// returns canned output/error, so no test here ever spawns osascript.
type fakeRunner struct {
	out     []byte
	err     error
	lastCtx context.Context
	argv    []string
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	f.lastCtx = ctx
	f.argv = append([]string{name}, args...)
	return f.out, f.err
}

// encodedImage returns a real, decodable w×h PNG (see pkg/computer/fake's
// blankPNG precedent) so tests exercise the real image.DecodeConfig path
// detect uses to size the screenshot for scaling. Gray (1 byte/pixel) rather
// than RGBA keeps a full-resolution (e.g. 2880x1800) fixture's encode cost
// low; only the dimensions matter here, not the pixel content.
func encodedImage(t *testing.T, w, h int) action.Image {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatalf("encode fixture image: %v", err)
	}
	return action.Image{MIME: action.MIMEPNG, Data: buf.Bytes()}
}

func TestHostSourceArgv(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{out: []byte(`{"screen":{"w":100,"h":100},"elements":[]}`)}
	d := ax.New(ax.WithSource(ax.HostSource(ax.WithRunner(f))))
	if _, err := d.Detect(context.Background(), action.Image{}); err != nil {
		t.Fatal(err)
	}
	if len(f.argv) != 5 { // osascript, -l, JavaScript, -e, <script>
		t.Fatalf("argv len = %d, want 5: %v", len(f.argv), f.argv)
	}
	want := []string{"osascript", "-l", "JavaScript", "-e"}
	for i, w := range want {
		if f.argv[i] != w {
			t.Errorf("argv[%d] = %q, want %q", i, f.argv[i], w)
		}
	}
	if !strings.Contains(f.argv[4], "System Events") {
		t.Errorf("script argv should reference System Events, got %q", f.argv[4])
	}
}

func TestHostSourceParsesAndScales(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{out: []byte(`{"screen":{"w":1440,"h":900},"elements":[
		{"role":"AXButton","title":"Save","value":"","x":10,"y":20,"w":80,"h":24,"enabled":true},
		{"role":"AXStaticText","title":"hint","value":"","x":0,"y":0,"w":10,"h":10,"enabled":true}
	]}`)}
	d := ax.New(ax.WithSource(ax.HostSource(ax.WithRunner(f))))
	els, err := d.Detect(context.Background(), encodedImage(t, 2880, 1800))
	if err != nil {
		t.Fatal(err)
	}
	if len(els) != 2 {
		t.Fatalf("got %d elements, want 2: %+v", len(els), els)
	}
	save := els[0]
	if save.Label != "Save" || !save.Interactable || save.Confidence != 1.0 {
		t.Errorf("save element = %+v", save)
	}
	if save.Box.Min != (action.Point{X: 20, Y: 40}) || save.Box.Max != (action.Point{X: 180, Y: 88}) {
		t.Errorf("save box = %+v, want the point box doubled into pixel space", save.Box)
	}
	if els[1].Interactable {
		t.Errorf("AXStaticText should not be interactable: %+v", els[1])
	}
}

func TestHostSourceEmptyOutput(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{out: nil}
	d := ax.New(ax.WithSource(ax.HostSource(ax.WithRunner(f))))
	if _, err := d.Detect(context.Background(), action.Image{}); !errors.Is(err, ax.ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

func TestHostSourceMalformedJSON(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{out: []byte("not json")}
	d := ax.New(ax.WithSource(ax.HostSource(ax.WithRunner(f))))
	if _, err := d.Detect(context.Background(), action.Image{}); !errors.Is(err, ax.ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

// TestHostSourceOsascriptMissing simulates the "osascript is not installed"
// case: os/exec would fail to even start the process. HostSource wraps any
// such Runner error as ErrUnavailable, matching the "osascript absent" half
// of the design's "returns ErrUnavailable when osascript is absent or the
// platform isn't darwin" requirement (the other half — a non-darwin GOOS —
// is enforced by ExecRunner itself, which a fake Runner bypasses by design so
// these tests run identically on every OS, including the Linux CI runner).
func TestHostSourceOsascriptMissing(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{err: errors.New(`exec: "osascript": executable file not found in $PATH`)}
	d := ax.New(ax.WithSource(ax.HostSource(ax.WithRunner(f))))
	if _, err := d.Detect(context.Background(), action.Image{}); !errors.Is(err, ax.ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

func TestHostSourceAssistiveDenied(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{err: errors.New(
		"execution error: System Events got an error: osascript is not allowed assistive access. (-25211)")}
	d := ax.New(ax.WithSource(ax.HostSource(ax.WithRunner(f))))
	_, err := d.Detect(context.Background(), action.Image{})
	if !errors.Is(err, ax.ErrUnavailable) {
		t.Errorf("err should wrap ErrUnavailable, got %v", err)
	}
	if !strings.Contains(err.Error(), "System Settings") {
		t.Errorf("err should name the fix (System Settings > ... > Accessibility), got %v", err)
	}
}

func TestHostSourceTimeoutSetsContextDeadline(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{out: []byte(`{"screen":{"w":1,"h":1},"elements":[]}`)}
	d := ax.New(ax.WithSource(ax.HostSource(ax.WithRunner(f), ax.WithTimeout(3*time.Second))))
	before := time.Now()
	if _, err := d.Detect(context.Background(), action.Image{}); err != nil {
		t.Fatal(err)
	}
	dl, ok := f.lastCtx.Deadline()
	if !ok {
		t.Fatal("expected the context passed to Run to carry a deadline")
	}
	if elapsed := dl.Sub(before); elapsed < 2*time.Second || elapsed > 4*time.Second {
		t.Errorf("deadline %v from now, want ~3s", elapsed)
	}
}

func TestHostSourceDefaultTimeoutIsFiveSeconds(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{out: []byte(`{"screen":{"w":1,"h":1},"elements":[]}`)}
	d := ax.New(ax.WithSource(ax.HostSource(ax.WithRunner(f))))
	before := time.Now()
	if _, err := d.Detect(context.Background(), action.Image{}); err != nil {
		t.Fatal(err)
	}
	dl, ok := f.lastCtx.Deadline()
	if !ok {
		t.Fatal("expected the context passed to Run to carry a deadline")
	}
	if elapsed := dl.Sub(before); elapsed < 4*time.Second || elapsed > 6*time.Second {
		t.Errorf("default deadline %v from now, want ~5s", elapsed)
	}
}
