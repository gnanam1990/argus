//go:build !darwin

// Non-macOS placeholder detection. This package's real value only exists on
// macOS (the permissions and lock-state concepts here are macOS-specific:
// System Settings > Privacy & Security has no equivalent elsewhere), so on
// every other platform detection can't actually be performed. Rather than
// fail every Ensure call outright — which would make the driver unusable
// off Mac — the fallback Checker reports both permissions as granted (there
// is nothing to grant, so nothing should block automation) and the fallback
// Guardian reports the screen as never locked, letting the driver proceed
// and fail loudly at the point of actual use (e.g. a real input/screenshot
// call erroring) instead of being silently blocked here on a platform this
// package cannot reason about.
package permissions

import "context"

// NoopChecker is the non-macOS Checker: it cannot determine real permission
// state, so it reports both permissions granted (see the package doc).
type NoopChecker struct{}

var _ Checker = NoopChecker{}

// Check always reports both permissions granted; see the package doc.
func (NoopChecker) Check(context.Context) (Status, error) {
	return Status{Accessibility: true, ScreenRecording: true}, nil
}

// NoopGuardian is the non-macOS Guardian: it cannot determine real lock
// state, so it reports the screen as never locked; see the package doc.
type NoopGuardian struct{}

var _ Guardian = NoopGuardian{}

// IsLocked always reports false; see the package doc.
func (NoopGuardian) IsLocked(context.Context) (bool, error) {
	return false, nil
}
