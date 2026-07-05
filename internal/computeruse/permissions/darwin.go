//go:build darwin

// Real macOS permission/lock detection. It shells out to `osascript -l
// JavaScript` (JXA) and `ioreg`, the same no-CGo approach
// internal/grounder/ax uses, so this file needs no Objective-C bridging code
// of its own and every path is exercised in tests through a fake Runner —
// never a real subprocess.
//
// Detection here is best-effort and approximate, not authoritative:
//   - Accessibility is read via AXIsProcessTrusted(), the same API System
//     Settings itself uses, so it should be exact.
//   - Screen Recording is read via CGPreflightScreenCaptureAccess(), which
//     macOS documents as a preflight check (it may not reflect a
//     just-revoked grant until the process is relaunched).
//   - Screen-lock state is inferred from ioreg's IOKit registry dump, an
//     undocumented but long-stable convention (the "CGSSessionScreenIsLocked"
//     key under the IOKit root), not a public API.
//
// Where a value can't be determined at all, Check conservatively reports it
// as not granted (so a caller gets an actionable "grant this permission"
// message rather than stalling), while IsLocked reports ErrPending (lock
// state is expected to resolve quickly, so retrying is the more useful
// signal than guessing locked/unlocked).

package permissions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Runner executes an external command and returns its stdout. It mirrors
// internal/grounder/ax's Runner exactly so a fake can be shared in shape
// (not in code — this package defines its own to avoid a dependency between
// otherwise-unrelated internal packages) across tests in this package.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner runs commands with os/exec. Both osascript and ioreg are
// macOS-only, so Run fails fast on any other GOOS instead of attempting to
// spawn them.
type ExecRunner struct{}

// Run executes name with args and returns its stdout.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("%s is macOS-only, unsupported on %s: %w", name, runtime.GOOS, ErrUnsupported)
	}
	return exec.CommandContext(ctx, name, args...).Output()
}

// defaultTimeout bounds a single osascript/ioreg run so a wedged call can't
// stall Ensure forever.
const defaultTimeout = 5 * time.Second

// HostOption configures a HostChecker or HostGuardian.
type HostOption func(*hostConfig)

type hostConfig struct {
	run     Runner
	timeout time.Duration
}

// WithRunner overrides the command runner (for tests).
func WithRunner(r Runner) HostOption { return func(c *hostConfig) { c.run = r } }

// WithTimeout overrides the default 5s command timeout.
func WithTimeout(d time.Duration) HostOption { return func(c *hostConfig) { c.timeout = d } }

func newHostConfig(opts []HostOption) hostConfig {
	c := hostConfig{run: ExecRunner{}, timeout: defaultTimeout}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// HostChecker is the real macOS Checker (see the package doc above).
type HostChecker struct {
	cfg hostConfig
}

var _ Checker = (*HostChecker)(nil)

// NewHostChecker builds a HostChecker.
func NewHostChecker(opts ...HostOption) *HostChecker {
	return &HostChecker{cfg: newHostConfig(opts)}
}

// permissionsJXA is a JXA program that reads both permission grants without
// prompting: AXIsProcessTrusted() (ApplicationServices) and
// CGPreflightScreenCaptureAccess() (CoreGraphics), each guarded by try/catch
// so a bridge failure on either one doesn't take down the other's result.
// CGPreflightScreenCaptureAccess is not auto-exposed on the JXA bridge, so it
// is bound explicitly via ObjC.bindFunction — importing CoreGraphics alone
// leaves the symbol undefined (and every read would falsely report "missing").
const permissionsJXA = `function run() {
  var acc = false, scr = false;
  try { ObjC.import('ApplicationServices'); acc = $.AXIsProcessTrusted() === true; } catch (e1) {}
  try { ObjC.bindFunction('CGPreflightScreenCaptureAccess', ['bool', []]); scr = $.CGPreflightScreenCaptureAccess() === true; } catch (e2) {}
  return JSON.stringify({accessibility: acc, screenRecording: scr});
}
`

// wireStatus is the JSON payload permissionsJXA prints on stdout.
type wireStatus struct {
	Accessibility   bool `json:"accessibility"`
	ScreenRecording bool `json:"screenRecording"`
}

// Check runs permissionsJXA and parses its result. See the package doc for
// how indeterminate cases are handled.
func (h *HostChecker) Check(ctx context.Context) (Status, error) {
	cctx, cancel := context.WithTimeout(ctx, h.cfg.timeout)
	defer cancel()

	out, err := h.cfg.run.Run(cctx, "osascript", "-l", "JavaScript", "-e", permissionsJXA)
	if err != nil {
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return Status{}, fmt.Errorf("permissions: osascript timed out checking permission status: %w", ErrPending)
		}
		// Best-effort: a hard failure to even ask (e.g. osascript missing)
		// can't be distinguished from "denied" here, so conservatively
		// report both as not granted rather than surface a raw exec error —
		// that keeps Ensure's message actionable.
		return Status{}, nil
	}

	var ws wireStatus
	if jsonErr := json.Unmarshal(bytes.TrimSpace(out), &ws); jsonErr != nil {
		// Same conservative fallback for unparsable output.
		return Status{}, nil
	}
	return Status(ws), nil
}

// HostGuardian is the real macOS Guardian (see the package doc above).
type HostGuardian struct {
	cfg hostConfig
}

var _ Guardian = (*HostGuardian)(nil)

// NewHostGuardian builds a HostGuardian.
func NewHostGuardian(opts ...HostOption) *HostGuardian {
	return &HostGuardian{cfg: newHostConfig(opts)}
}

// lockedKey and unlockedKey are the two forms ioreg's text dump uses for the
// IOKit registry's screen-lock flag.
const (
	lockedKey = `"CGSSessionScreenIsLocked" = 1`
)

// IsLocked runs `ioreg -n Root -d1 -l` and looks for the screen-lock key in
// its text dump. See the package doc for how an indeterminate result is
// handled.
func (h *HostGuardian) IsLocked(ctx context.Context) (bool, error) {
	cctx, cancel := context.WithTimeout(ctx, h.cfg.timeout)
	defer cancel()

	out, err := h.cfg.run.Run(cctx, "ioreg", "-n", "Root", "-d1", "-l")
	if err != nil {
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return false, fmt.Errorf("permissions: ioreg timed out checking screen-lock state: %w", ErrPending)
		}
		return false, fmt.Errorf("permissions: ioreg failed checking screen-lock state: %w", err)
	}

	// A successful ioreg read is authoritative: the lock key is present and 1
	// only while the screen is locked. Its absence (a common unlocked state) or
	// an explicit 0 both mean unlocked — only a failed/timed-out read is
	// genuinely indeterminate (handled above as ErrPending).
	if strings.Contains(string(out), lockedKey) {
		return true, nil
	}
	return false, nil
}
