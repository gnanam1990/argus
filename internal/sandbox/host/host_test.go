package host_test

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gnanam1990/argus/internal/sandbox/host"
	compfake "github.com/gnanam1990/argus/pkg/computer/fake"
	"github.com/gnanam1990/argus/pkg/sandbox"
)

func provision(t *testing.T) (sandbox.Sandbox, *compfake.Computer) {
	t.Helper()
	f := compfake.New()
	sb, err := host.New(f).Provision(context.Background(), sandbox.Spec{})
	if err != nil {
		t.Fatal(err)
	}
	return sb, f
}

func TestHostSandboxComputerAndStop(t *testing.T) {
	t.Parallel()
	sb, f := provision(t)
	if sb.Computer() != f {
		t.Error("Computer() should return the injected driver")
	}
	if err := sb.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !f.Closed() {
		t.Error("Stop should close the computer")
	}
}

func TestHostSandboxExec(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("exec uses sh -c")
	}
	sb, _ := provision(t)

	res, err := sb.Exec(context.Background(), "echo hi; echo oops >&2", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(res.Stdout) != "hi" || strings.TrimSpace(res.Stderr) != "oops" || res.ExitCode != 0 {
		t.Errorf("Exec = %+v", res)
	}

	// A non-zero exit is a result, not an error.
	res, err = sb.Exec(context.Background(), "exit 3", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 3 {
		t.Errorf("ExitCode = %d, want 3", res.ExitCode)
	}
}

func TestHostSandboxFiles(t *testing.T) {
	t.Parallel()
	sb, _ := provision(t)
	path := filepath.Join(t.TempDir(), "note.txt")

	if err := sb.WriteFile(context.Background(), path, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	b, err := sb.ReadFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello" {
		t.Errorf("ReadFile = %q", b)
	}
	if _, err := sb.ReadFile(context.Background(), filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("ReadFile on a missing path should error")
	}
}
