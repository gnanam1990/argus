//go:build robotgo

package app

import (
	"github.com/gnanam1990/argus/internal/driver/robotgo"
	"github.com/gnanam1990/argus/pkg/computer"
)

// hostDriver returns the native robotgo host driver (macOS/Windows) for the
// given display index (0 = primary). smooth animates pointer motion. Selected
// when the binary is built with -tags robotgo.
func hostDriver(display int, smooth bool) computer.Computer {
	return robotgo.New(robotgo.WithDisplay(display), robotgo.WithSmooth(smooth))
}
