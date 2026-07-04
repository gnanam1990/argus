package app_test

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/gnanam1990/argus/internal/app"
	"github.com/gnanam1990/argus/internal/config"
	"github.com/gnanam1990/argus/pkg/computer"
)

// BuildComputer must expose the sandbox's gated operations through the
// pkg/computer optional interfaces so allowlisted actions can execute.
func TestBuildComputerBridgesSandboxOps(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("host exec uses sh -c")
	}
	cfg := config.Defaults()
	cfg.Sandbox.Kind = "host"

	comp, cleanup, err := app.BuildComputer(context.Background(), cfg, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cleanup() }()

	cmder, ok := comp.(computer.Commander)
	if !ok {
		t.Fatal("host computer must implement computer.Commander")
	}
	out, err := cmder.RunCommand(context.Background(), "echo bridged")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "bridged") {
		t.Errorf("RunCommand output = %q", out)
	}

	if _, ok := comp.(computer.FileReader); !ok {
		t.Error("host computer must implement computer.FileReader")
	}
	if _, ok := comp.(computer.FileWriter); !ok {
		t.Error("host computer must implement computer.FileWriter")
	}
}
