//go:build darwin

package app

import (
	"github.com/gnanam1990/argus/internal/computeruse/capture"
	"github.com/gnanam1990/argus/internal/computeruse/permissions"
)

// cuPlatform returns the real macOS computer-use host implementations:
// accessibility/screen-recording checks, screen-lock detection, app focus, and
// running-app enumeration, all via osascript/ioreg.
func cuPlatform() cuParts {
	return cuParts{
		checker:  permissions.NewHostChecker(),
		guardian: permissions.NewHostGuardian(),
		focuser:  capture.NewHostFocuser(),
		lister:   capture.NewHostAppLister(),
	}
}
