package ax

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Clicker presses the accessibility element at a screen point without moving
// the pointer, via osascript + the ApplicationServices AX API
// (AXUIElementCopyElementAtPosition → AXUIElementPerformAction "AXPress"). It
// is CGo-free and, like HostSource, requires the Accessibility permission and
// works only on macOS. It satisfies pkg/computer.BackgroundClicker through the
// app-layer adapter.
type Clicker struct {
	run     Runner
	timeout time.Duration
}

// NewClicker builds a Clicker. Options are shared with HostSource
// (WithRunner / WithTimeout).
func NewClicker(opts ...HostOption) *Clicker {
	h := &hostSource{run: ExecRunner{}, timeout: defaultTimeout}
	for _, o := range opts {
		o(h)
	}
	return &Clicker{run: h.run, timeout: h.timeout}
}

// errNoTarget is returned when there is no actionable element at the point. The
// app-layer adapter maps it to computer.ErrNoBackgroundTarget so the executor
// falls back to a cursor click.
var errNoTarget = fmt.Errorf("ax: no actionable element at point")

// ErrNoTarget reports whether err is the no-actionable-element signal.
func ErrNoTarget(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no actionable element")
}

// ErrPermission reports whether err is the Accessibility-permission-denied
// signal, so a caller can fall back to a cursor click (and tell the user to
// grant the permission) rather than failing the run.
func ErrPermission(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Accessibility permission denied")
}

// Click presses the element at screen point (x, y). It returns errNoTarget
// (see ErrNoTarget) when nothing pressable is there.
func (c *Clicker) Click(ctx context.Context, x, y int) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	out, err := c.run.Run(ctx, "osascript", "-l", "JavaScript", "-e", clickScript(x, y))
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if isAssistiveDenied(msg) || isAssistiveDenied(err.Error()) {
			return fmt.Errorf("ax: Accessibility permission denied — grant it to this app in System Settings → Privacy & Security → Accessibility, then restart: %w", err)
		}
		return fmt.Errorf("ax: click: %w", err)
	}
	switch strings.TrimSpace(string(out)) {
	case "ok":
		return nil
	case "notarget":
		return errNoTarget
	default:
		return fmt.Errorf("ax: click: unexpected result %q", strings.TrimSpace(string(out)))
	}
}

// clickScript builds the JXA program: hit-test the system-wide element at the
// point, then perform its AXPress action. Prints "ok", "notarget", or lets
// osascript surface a permission error on stderr.
func clickScript(x, y int) string {
	return fmt.Sprintf(`ObjC.import('ApplicationServices');
(function () {
  var sys = $.AXUIElementCreateSystemWide();
  var el = Ref();
  var err = $.AXUIElementCopyElementAtPosition(sys, %d, %d, el);
  if (err !== 0) { return 'notarget'; }
  var perr = $.AXUIElementPerformAction(el[0], $('AXPress'));
  return perr === 0 ? 'ok' : 'notarget';
})();`, x, y)
}
