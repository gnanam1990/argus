package host_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gnanam1990/argus/internal/sandbox/host"
	compfake "github.com/gnanam1990/argus/pkg/computer/fake"
	"github.com/gnanam1990/argus/pkg/sandbox"
)

func TestHostSandbox(t *testing.T) {
	t.Parallel()
	f := compfake.New()
	sb, err := host.New(f).Provision(context.Background(), sandbox.Spec{})
	if err != nil {
		t.Fatal(err)
	}

	if sb.Computer() != f {
		t.Error("Computer() should return the injected driver")
	}

	// Gated operations are not permitted on the host sandbox.
	if _, err := sb.Exec(context.Background(), "ls", time.Second); !errors.Is(err, sandbox.ErrNotPermitted) {
		t.Errorf("Exec err = %v, want ErrNotPermitted", err)
	}
	if _, err := sb.ReadFile(context.Background(), "/etc/hosts"); !errors.Is(err, sandbox.ErrNotPermitted) {
		t.Errorf("ReadFile err = %v, want ErrNotPermitted", err)
	}
	if err := sb.WriteFile(context.Background(), "/tmp/x", []byte("y")); !errors.Is(err, sandbox.ErrNotPermitted) {
		t.Errorf("WriteFile err = %v, want ErrNotPermitted", err)
	}

	if err := sb.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !f.Closed() {
		t.Error("Stop should close the computer")
	}
}
