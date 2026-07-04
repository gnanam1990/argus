//go:build robotgo

package app

import (
	"github.com/gnanam1990/argus/internal/driver/robotgo"
	"github.com/gnanam1990/argus/pkg/computer"
)

// hostDriver returns the native robotgo host driver (macOS/Windows). Selected
// when the binary is built with -tags robotgo.
func hostDriver() computer.Computer { return robotgo.New() }
