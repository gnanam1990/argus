//go:build !darwin

// Non-macOS placeholders. Activation and running-app enumeration via
// osascript/System Events are macOS-specific, so on every other platform
// NoopFocuser reports an unsupported error (there is no real app to bring
// forward, and pretending to succeed would let a caller act blind) while
// NoopAppLister reports an empty, error-free list (there is nothing to list,
// but that's not a failure in itself).
package capture

import (
	"context"
	"fmt"
	"runtime"

	"github.com/gnanam1990/argus/internal/computeruse/state"
)

// NoopFocuser is the non-macOS Focuser: it cannot activate an app.
type NoopFocuser struct{}

var _ Focuser = NoopFocuser{}

// Focus always fails with an unsupported-platform error.
func (NoopFocuser) Focus(_ context.Context, bundleID string) error {
	return fmt.Errorf("capture: activate %s: unsupported on %s", bundleID, runtime.GOOS)
}

// NoopAppLister is the non-macOS AppLister: it cannot enumerate running
// apps.
type NoopAppLister struct{}

var _ AppLister = NoopAppLister{}

// ListApps always reports an empty list; see the package doc.
func (NoopAppLister) ListApps(context.Context) ([]state.AppInfo, error) {
	return nil, nil
}
