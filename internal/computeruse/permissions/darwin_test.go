//go:build darwin

package permissions_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gnanam1990/argus/internal/computeruse/permissions"
)

// fakeRunner is a hermetic permissions.Runner: it never spawns a real
// process, only records the argv it was asked to run and returns fixture
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

func TestHostCheckerGranted(t *testing.T) {
	t.Parallel()
	fr := &fakeRunner{out: []byte(`{"accessibility":true,"screenRecording":true}`)}
	c := permissions.NewHostChecker(permissions.WithRunner(fr))

	got, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !got.Accessibility || !got.ScreenRecording {
		t.Errorf("Check() = %+v, want both true", got)
	}
	if !strings.Contains(lastArg(fr), "AXIsProcessTrusted") {
		t.Errorf("script missing AXIsProcessTrusted: %s", lastArg(fr))
	}
	if !strings.Contains(lastArg(fr), "CGPreflightScreenCaptureAccess") {
		t.Errorf("script missing CGPreflightScreenCaptureAccess: %s", lastArg(fr))
	}
}

func TestHostCheckerPartial(t *testing.T) {
	t.Parallel()
	fr := &fakeRunner{out: []byte(`{"accessibility":true,"screenRecording":false}`)}
	c := permissions.NewHostChecker(permissions.WithRunner(fr))

	got, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !got.Accessibility || got.ScreenRecording {
		t.Errorf("Check() = %+v, want {true false}", got)
	}
}

func TestHostCheckerRunErrorIsConservative(t *testing.T) {
	t.Parallel()
	fr := &fakeRunner{err: errors.New("osascript: command not found")}
	c := permissions.NewHostChecker(permissions.WithRunner(fr))

	got, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v, want nil (conservative fallback)", err)
	}
	if got.Accessibility || got.ScreenRecording {
		t.Errorf("Check() = %+v, want both false on failure", got)
	}
}

func TestHostCheckerMalformedOutputIsConservative(t *testing.T) {
	t.Parallel()
	fr := &fakeRunner{out: []byte("not json")}
	c := permissions.NewHostChecker(permissions.WithRunner(fr))

	got, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v, want nil (conservative fallback)", err)
	}
	if got.Accessibility || got.ScreenRecording {
		t.Errorf("Check() = %+v, want both false on malformed output", got)
	}
}

func TestHostCheckerTimeoutIsPending(t *testing.T) {
	t.Parallel()
	// A parent context whose deadline already elapsed stands in for a
	// wedged osascript call: HostChecker's own context.WithTimeout wraps
	// this, and since the parent is already past its deadline the wrapped
	// context reports Err() == context.DeadlineExceeded immediately, with
	// no real waiting and no real subprocess involved.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Minute))
	defer cancel()
	fr := &fakeRunner{err: errors.New("signal: killed")}
	c := permissions.NewHostChecker(permissions.WithRunner(fr), permissions.WithTimeout(0))

	_, err := c.Check(ctx)
	if !errors.Is(err, permissions.ErrPending) {
		t.Fatalf("Check() error = %v, want ErrPending", err)
	}
}

func TestHostGuardianLocked(t *testing.T) {
	t.Parallel()
	fr := &fakeRunner{out: []byte("    \"CGSSessionScreenIsLocked\" = 1\n")}
	g := permissions.NewHostGuardian(permissions.WithRunner(fr))

	locked, err := g.IsLocked(context.Background())
	if err != nil {
		t.Fatalf("IsLocked() error = %v", err)
	}
	if !locked {
		t.Errorf("IsLocked() = false, want true")
	}
	if len(fr.argv) == 0 || fr.argv[0] != "ioreg" {
		t.Errorf("expected ioreg to be invoked, argv = %v", fr.argv)
	}
}

func TestHostGuardianUnlocked(t *testing.T) {
	t.Parallel()
	fr := &fakeRunner{out: []byte("    \"CGSSessionScreenIsLocked\" = 0\n")}
	g := permissions.NewHostGuardian(permissions.WithRunner(fr))

	locked, err := g.IsLocked(context.Background())
	if err != nil {
		t.Fatalf("IsLocked() error = %v", err)
	}
	if locked {
		t.Errorf("IsLocked() = true, want false")
	}
}

// A successful ioreg read with no lock marker means the screen is unlocked
// (the key is commonly absent while unlocked); it must NOT be treated as
// pending, or every capture would stall on a normal unlocked screen.
func TestHostGuardianAbsentKeyIsUnlocked(t *testing.T) {
	t.Parallel()
	fr := &fakeRunner{out: []byte("no such key here\n")}
	g := permissions.NewHostGuardian(permissions.WithRunner(fr))

	locked, err := g.IsLocked(context.Background())
	if err != nil {
		t.Fatalf("IsLocked() error = %v, want nil", err)
	}
	if locked {
		t.Error("IsLocked() = true, want false (no lock marker present)")
	}
}

func TestHostGuardianRunErrorPropagates(t *testing.T) {
	t.Parallel()
	boom := errors.New("ioreg: not found")
	fr := &fakeRunner{err: boom}
	g := permissions.NewHostGuardian(permissions.WithRunner(fr))

	_, err := g.IsLocked(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("IsLocked() error = %v, want wrapping boom", err)
	}
}

func TestHostGuardianTimeoutIsPending(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Minute))
	defer cancel()
	fr := &fakeRunner{err: errors.New("signal: killed")}
	g := permissions.NewHostGuardian(permissions.WithRunner(fr), permissions.WithTimeout(0))

	_, err := g.IsLocked(ctx)
	if !errors.Is(err, permissions.ErrPending) {
		t.Fatalf("IsLocked() error = %v, want ErrPending", err)
	}
}

func TestExecRunnerFailsFastOffDarwinIsNoOp(t *testing.T) {
	t.Parallel()
	// This test only meaningfully exercises the fast-fail branch on
	// non-darwin GOOS; on darwin ExecRunner would actually spawn a process,
	// which hermetic tests must never do, so it is intentionally left
	// unexercised here beyond construction.
	var _ permissions.Runner = permissions.ExecRunner{}
}
