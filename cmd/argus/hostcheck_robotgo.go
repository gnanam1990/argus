//go:build robotgo

package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"github.com/gnanam1990/argus/internal/driver/robotgo"
)

// errPermissionsReminder notes the macOS TCC grants the native backend needs.
var errPermissionsReminder = errors.New(
	"native backend ready — grant Screen Recording + Accessibility to this binary if capture/input fail")

// displayServer reports the native backend (robotgo build).
func displayServer() string { return "native (robotgo/" + runtime.GOOS + ")" }

// preflight reports host readiness for the native backend. robotgo drives the
// host directly, but macOS requires the operator to grant Screen Recording and
// Accessibility permissions.
func preflight() error {
	if runtime.GOOS == "darwin" {
		return errPermissionsReminder
	}
	return nil
}

// captureCheck actually attempts a screen capture and reports the result, so
// doctor gives a definitive answer instead of a guess.
func captureCheck() string {
	img, err := robotgo.New().Screenshot(context.Background())
	if err != nil || img.Empty() {
		msg := "FAILED"
		if err != nil {
			msg += " (" + err.Error() + ")"
		}
		if runtime.GOOS == "darwin" {
			msg += "\n                  fix: System Settings > Privacy & Security > Screen Recording —" +
				"\n                       enable the terminal/app that launched argus, then fully quit and reopen it"
		}
		return msg
	}
	return fmt.Sprintf("ok (%d bytes)", len(img.Data))
}
