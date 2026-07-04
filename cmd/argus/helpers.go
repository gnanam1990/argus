package main

import (
	"log/slog"
	"os"
	"strings"

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

// buildRecorder returns a disk recorder when dir is set, else a no-op recorder.
func buildRecorder(dir string, m trajectory.Manifest, mask func(string) string) (trajectory.Recorder, error) {
	if dir == "" {
		return trajectory.NoOp{}, nil
	}
	return trajectory.NewDisk(dir, m, trajectory.WithMask(mask))
}

// maskFunc builds a redactor over the given secret values.
func maskFunc(secrets []string) func(string) string {
	return func(s string) string {
		for _, secret := range secrets {
			if secret != "" {
				s = strings.ReplaceAll(s, secret, "«redacted»")
			}
		}
		return s
	}
}
