//go:build !robotgo

package app

import (
	"github.com/gnanam1990/argus/internal/driver/shell"
	"github.com/gnanam1990/argus/pkg/computer"
)

// hostDriver returns the default host driver: the CGo-free X11 shell driver.
// Build with -tags robotgo for the native macOS/Windows backend.
func hostDriver() computer.Computer { return shell.New() }
