package main

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/gnanam1990/argus/pkg/trajectory"
)

// logger returns the telemetry logger (stderr, structured).
func logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// discardLogger drops all telemetry — used in TUI mode so structured logs do
// not corrupt the alternate-screen display.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
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
