package main

import (
	"log/slog"
	"os"

	"github.com/gnanam1990/argus/internal/platform"
	"github.com/gnanam1990/argus/pkg/trajectory"
)

// displayServer reports the host display server.
func displayServer() string { return platform.DisplayServer() }

// preflight validates the host can be driven.
func preflight() error { return platform.Preflight(os.Getenv) }

// logger returns the telemetry logger (stderr, structured).
func logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// trajectoryRecorder returns the run recorder. The disk recorder arrives with
// the trajectory stage; for now runs are not persisted.
func trajectoryRecorder() trajectory.Recorder { return trajectory.NoOp{} }
