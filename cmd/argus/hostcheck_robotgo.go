//go:build robotgo

package main

import (
	"context"
	"fmt"
	"runtime"

	"github.com/gnanam1990/argus/internal/driver/robotgo"
)

// displayServer reports the native backend (robotgo build).
func displayServer() string { return "native (robotgo/" + runtime.GOOS + ")" }

// preflight reports host readiness for the native backend. robotgo drives the
// host directly; there is no reliable static check for the macOS Screen
// Recording/Accessibility grants short of actually using them, so preflight
// always succeeds here and defers to captureCheck's real probe below instead
// of printing a reminder that would contradict it (doctor must be able to
// report "host control: ok" once permissions are actually granted).
func preflight() error { return nil }

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
