//go:build robotgo

package main

import (
	"errors"
	"runtime"
)

// errPermissionsReminder notes the macOS TCC grants the native backend needs.
var errPermissionsReminder = errors.New(
	"native backend ready — grant Screen Recording + Accessibility to this binary if capture/input fail")

// displayServer reports the native backend (robotgo build).
func displayServer() string { return "native (robotgo/" + runtime.GOOS + ")" }

// preflight reports host readiness for the native backend. robotgo drives the
// host directly, but macOS requires the operator to grant Screen Recording and
// Accessibility permissions; those are surfaced as a reminder rather than a
// hard failure (they cannot be probed without additional native calls).
func preflight() error {
	if runtime.GOOS == "darwin" {
		return errPermissionsReminder
	}
	return nil
}
