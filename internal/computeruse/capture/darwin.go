//go:build darwin

package capture

// HostFocuser and HostAppLister are the real macOS implementations of
// Focuser and AppLister, both driven by `osascript` (AppleScript for
// activation, JXA for enumeration via System Events) through an injected
// Runner — mirroring internal/grounder/ax's Runner so every test in this
// package feeds fixture output through a fake and never spawns a real
// process.
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/gnanam1990/argus/internal/computeruse/state"
)

// Runner executes an external command and returns its stdout.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner runs commands with os/exec. osascript only exists on macOS, so
// Run fails fast on any other GOOS instead of attempting to spawn it.
type ExecRunner struct{}

// Run executes name with args and returns its stdout.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("capture: %s is macOS-only, unsupported on %s", name, runtime.GOOS)
	}
	return exec.CommandContext(ctx, name, args...).Output()
}

// defaultRunnerTimeout bounds a single osascript invocation so a wedged call
// can't stall a capture forever.
const defaultRunnerTimeout = 5 * time.Second

// HostFocuserOption configures a HostFocuser.
type HostFocuserOption func(*HostFocuser)

// WithFocuserRunner overrides the command runner (for tests).
func WithFocuserRunner(r Runner) HostFocuserOption {
	return func(h *HostFocuser) { h.run = r }
}

// WithFocuserTimeout overrides the default 5s osascript timeout.
func WithFocuserTimeout(d time.Duration) HostFocuserOption {
	return func(h *HostFocuser) { h.timeout = d }
}

// HostFocuser activates an app by bundle identifier via
// `osascript -e 'tell application id "<bundle>" to activate'`.
type HostFocuser struct {
	run     Runner
	timeout time.Duration
}

var _ Focuser = (*HostFocuser)(nil)

// NewHostFocuser builds a HostFocuser.
func NewHostFocuser(opts ...HostFocuserOption) *HostFocuser {
	h := &HostFocuser{run: ExecRunner{}, timeout: defaultRunnerTimeout}
	for _, o := range opts {
		o(h)
	}
	return h
}

// Focus activates the app identified by bundleID.
func (h *HostFocuser) Focus(ctx context.Context, bundleID string) error {
	cctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	script := fmt.Sprintf(`tell application id %q to activate`, bundleID)
	if _, err := h.run.Run(cctx, "osascript", "-e", script); err != nil {
		return fmt.Errorf("capture: activate %s: %w", bundleID, err)
	}
	return nil
}

// HostAppListerOption configures a HostAppLister.
type HostAppListerOption func(*HostAppLister)

// WithAppListerRunner overrides the command runner (for tests).
func WithAppListerRunner(r Runner) HostAppListerOption {
	return func(h *HostAppLister) { h.run = r }
}

// WithAppListerTimeout overrides the default 5s osascript timeout.
func WithAppListerTimeout(d time.Duration) HostAppListerOption {
	return func(h *HostAppLister) { h.timeout = d }
}

// HostAppLister enumerates the currently running apps via
// `osascript -l JavaScript` driving System Events.
type HostAppLister struct {
	run     Runner
	timeout time.Duration
}

var _ AppLister = (*HostAppLister)(nil)

// NewHostAppLister builds a HostAppLister.
func NewHostAppLister(opts ...HostAppListerOption) *HostAppLister {
	h := &HostAppLister{run: ExecRunner{}, timeout: defaultRunnerTimeout}
	for _, o := range opts {
		o(h)
	}
	return h
}

// wireApp is one entry of the JSON array listAppsScript emits.
type wireApp struct {
	BundleIdentifier string `json:"bundleIdentifier"`
	Name             string `json:"name"`
}

// listAppsScript is a JXA program that walks System Events' running
// application processes and returns their bundle identifier and name as a
// JSON array. Each read is wrapped in try/catch: a process this account
// can't introspect (e.g. a sandboxed helper) must not abort the whole walk.
const listAppsScript = `function run() {
  var out = [];
  try {
    var se = Application("System Events");
    var procs = se.applicationProcesses();
    for (var i = 0; i < procs.length; i++) {
      try {
        var bid = procs[i].bundleIdentifier();
        var nm = procs[i].name();
        if (bid) { out.push({bundleIdentifier: bid, name: nm}); }
      } catch (eProc) {}
    }
  } catch (eSE) {}
  return JSON.stringify(out);
}
`

// ListApps returns the currently running apps.
func (h *HostAppLister) ListApps(ctx context.Context) ([]state.AppInfo, error) {
	cctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	out, err := h.run.Run(cctx, "osascript", "-l", "JavaScript", "-e", listAppsScript)
	if err != nil {
		return nil, fmt.Errorf("capture: list running apps: %w", err)
	}

	var wire []wireApp
	if err := json.Unmarshal(bytes.TrimSpace(out), &wire); err != nil {
		return nil, fmt.Errorf("capture: parse running apps: %w", err)
	}

	apps := make([]state.AppInfo, 0, len(wire))
	for _, w := range wire {
		apps = append(apps, state.AppInfo{
			BundleIdentifier: w.BundleIdentifier,
			Name:             w.Name,
			IsRunning:        true,
		})
	}
	return apps, nil
}
