//go:build !robotgo

package main

import (
	"os"

	"github.com/gnanam1990/argus/internal/platform"
)

// displayServer reports the host display server (X11 shell driver build).
func displayServer() string { return platform.DisplayServer() }

// preflight validates the host can be driven by the X11 shell driver.
func preflight() error { return platform.Preflight(os.Getenv) }

// captureCheck is a no-op for the X11 build (capture is validated at run time).
func captureCheck() string { return "" }
