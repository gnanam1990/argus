package main

import (
	"strings"
	"testing"

	"github.com/gnanam1990/argus/pkg/trajectory"
)

func TestRunDryRun(t *testing.T) {
	t.Parallel()
	var buf strings.Builder
	if err := run([]string{"run", "--dry-run", "click", "the", "button"}, &buf); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !strings.Contains(buf.String(), "plan:") || !strings.Contains(buf.String(), "provider=anthropic") {
		t.Errorf("dry-run output = %q", buf.String())
	}
}

func TestRunRequiresTask(t *testing.T) {
	t.Parallel()
	var buf strings.Builder
	// No task, no dry-run → error before any network/driver work.
	if err := run([]string{"run"}, &buf); err == nil {
		t.Error("expected a task-required error")
	}
}

func TestParseRun(t *testing.T) {
	t.Parallel()
	cfg, traj, dry, task := parseRun([]string{"--config", "x.json", "--trajectory", "out/", "--dry-run", "do", "it"})
	if cfg != "x.json" || traj != "out/" || !dry || task != "do it" {
		t.Errorf("parseRun = %q, %q, %v, %q", cfg, traj, dry, task)
	}
}

func TestBuildRecorder(t *testing.T) {
	t.Parallel()
	// No dir → no-op recorder.
	rec, err := buildRecorder("", trajectory.NewManifest("t"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rec.(trajectory.NoOp); !ok {
		t.Errorf("empty dir should give NoOp, got %T", rec)
	}
	// A dir gives a disk recorder.
	disk, err := buildRecorder(t.TempDir(), trajectory.NewManifest("t"), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = disk.Close()

	// Masking replaces secrets.
	if got := maskFunc([]string{"sk-secret"})("key sk-secret end"); strings.Contains(got, "sk-secret") {
		t.Error("maskFunc did not redact")
	}
}

func TestDoctor(t *testing.T) {
	t.Parallel()
	var buf strings.Builder
	if err := run([]string{"doctor"}, &buf); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	for _, want := range []string{"argus doctor", "display server", "config:", "api key:"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("doctor output missing %q:\n%s", want, buf.String())
		}
	}
}

func TestRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		wantErr    bool
		wantSubstr string
	}{
		{name: "no args prints usage", args: nil, wantSubstr: "Usage:"},
		{name: "version subcommand", args: []string{"version"}, wantSubstr: "argus "},
		{name: "version long flag", args: []string{"--version"}, wantSubstr: "argus "},
		{name: "help subcommand", args: []string{"help"}, wantSubstr: "argus run"},
		{name: "unknown command errors", args: []string{"bogus"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf strings.Builder
			err := run(tt.args, &buf)

			if tt.wantErr && err == nil {
				t.Fatalf("run(%v) = nil error, want error", tt.args)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("run(%v) = %v, want nil error", tt.args, err)
			}
			if tt.wantSubstr != "" && !strings.Contains(buf.String(), tt.wantSubstr) {
				t.Errorf("run(%v) output = %q, want substring %q", tt.args, buf.String(), tt.wantSubstr)
			}
		})
	}
}
