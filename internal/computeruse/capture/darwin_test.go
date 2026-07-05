//go:build darwin

package capture_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gnanam1990/argus/internal/computeruse/capture"
)

// fakeRunner is a hermetic capture.Runner: it never spawns a real process,
// only records the argv it was asked to run and returns fixture
// output/error.
type fakeRunner struct {
	out  []byte
	err  error
	argv []string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.argv = append([]string{name}, args...)
	return f.out, f.err
}

func lastArg(fr *fakeRunner) string {
	if len(fr.argv) == 0 {
		return ""
	}
	return fr.argv[len(fr.argv)-1]
}

func TestHostFocuser_ActivatesByBundleID(t *testing.T) {
	t.Parallel()
	fr := &fakeRunner{}
	f := capture.NewHostFocuser(capture.WithFocuserRunner(fr))

	if err := f.Focus(context.Background(), "com.example.app"); err != nil {
		t.Fatalf("Focus() error = %v", err)
	}
	script := lastArg(fr)
	if !strings.Contains(script, "com.example.app") || !strings.Contains(script, "activate") {
		t.Errorf("script = %q, want it to reference the bundle id and activate", script)
	}
}

func TestHostFocuser_RunErrorPropagates(t *testing.T) {
	t.Parallel()
	fr := &fakeRunner{err: errors.New("osascript: not authorized")}
	f := capture.NewHostFocuser(capture.WithFocuserRunner(fr))

	err := f.Focus(context.Background(), "com.example.app")
	if err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Errorf("Focus() error = %v, want it to wrap the runner error", err)
	}
}

func TestHostAppLister_ParsesRunningApps(t *testing.T) {
	t.Parallel()
	fr := &fakeRunner{out: []byte(`[{"bundleIdentifier":"com.apple.finder","name":"Finder"},` +
		`{"bundleIdentifier":"com.apple.Terminal","name":"Terminal"}]`)}
	l := capture.NewHostAppLister(capture.WithAppListerRunner(fr))

	apps, err := l.ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps() error = %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("ListApps() = %+v, want 2 apps", apps)
	}
	if apps[0].BundleIdentifier != "com.apple.finder" || apps[0].Name != "Finder" || !apps[0].IsRunning {
		t.Errorf("apps[0] = %+v", apps[0])
	}
	if apps[1].BundleIdentifier != "com.apple.Terminal" {
		t.Errorf("apps[1] = %+v", apps[1])
	}
	if fr.argv[0] != "osascript" {
		t.Errorf("argv[0] = %q, want osascript", fr.argv[0])
	}
}

func TestHostAppLister_EmptyList(t *testing.T) {
	t.Parallel()
	fr := &fakeRunner{out: []byte(`[]`)}
	l := capture.NewHostAppLister(capture.WithAppListerRunner(fr))

	apps, err := l.ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps() error = %v", err)
	}
	if len(apps) != 0 {
		t.Errorf("ListApps() = %+v, want empty", apps)
	}
}

func TestHostAppLister_MalformedOutput(t *testing.T) {
	t.Parallel()
	fr := &fakeRunner{out: []byte("not json")}
	l := capture.NewHostAppLister(capture.WithAppListerRunner(fr))

	if _, err := l.ListApps(context.Background()); err == nil {
		t.Error("ListApps() should error on malformed output")
	}
}

func TestHostAppLister_RunErrorPropagates(t *testing.T) {
	t.Parallel()
	fr := &fakeRunner{err: errors.New("osascript: timeout")}
	l := capture.NewHostAppLister(capture.WithAppListerRunner(fr))

	if _, err := l.ListApps(context.Background()); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Errorf("ListApps() error = %v, want it to wrap the runner error", err)
	}
}
