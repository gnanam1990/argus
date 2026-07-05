//go:build !darwin

package app

import (
	"github.com/gnanam1990/argus/internal/computeruse/capture"
	"github.com/gnanam1990/argus/internal/computeruse/permissions"
)

// cuPlatform returns the non-macOS stubs: permissions are assumed granted (the
// driver fails loudly if not), the screen is never reported locked, focus is
// unsupported, and no apps are enumerated. The app-aware computer-use subsystem
// is a macOS feature; elsewhere it compiles but does little.
func cuPlatform() cuParts {
	return cuParts{
		checker:  permissions.NoopChecker{},
		guardian: permissions.NoopGuardian{},
		focuser:  capture.NoopFocuser{},
		lister:   capture.NoopAppLister{},
	}
}
