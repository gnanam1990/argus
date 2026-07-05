//go:build !robotgo && !darwin

package app

import (
	"github.com/gnanam1990/argus/internal/driver/shell"
	"github.com/gnanam1990/argus/pkg/computer"
)

// hostDriver returns the default host driver: the CGo-free X11 shell driver.
// The display index and smooth flag are accepted for signature parity with the
// robotgo build but not used — the X11 driver captures the whole virtual screen
// and doesn't animate motion. Build with -tags robotgo for the native
// macOS/Windows per-display backend.
func hostDriver(_ int, _ bool) computer.Computer { return shell.New() }
