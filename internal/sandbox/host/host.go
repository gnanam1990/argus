// Package host is a sandbox.Provider that drives the local machine's GUI. It is
// the trusted, no-isolation option. Exec and file operations are implemented
// for real so the gated-capability path works on the primary local use case —
// but they never run unless the operator explicitly enables the capability in
// config (the executor allowlist is off by default) and the approval/injection
// middleware let the action through.
package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/gnanam1990/argus/pkg/computer"
	"github.com/gnanam1990/argus/pkg/sandbox"
)

// Provider provisions a sandbox backed by a local Computer.
type Provider struct {
	comp computer.Computer
}

// New builds a host provider over the given local driver.
func New(comp computer.Computer) *Provider { return &Provider{comp: comp} }

var _ sandbox.Provider = (*Provider)(nil)

// Provision returns a sandbox wrapping the local computer. Spec is ignored.
func (p *Provider) Provision(_ context.Context, _ sandbox.Spec) (sandbox.Sandbox, error) {
	return &hostSandbox{comp: p.comp}, nil
}

type hostSandbox struct {
	comp computer.Computer
}

func (s *hostSandbox) Computer() computer.Computer { return s.comp }

// Exec runs cmd under `sh -c` on the host. A non-zero exit is reported via
// ExecResult.ExitCode, not an error; errors mean the command could not run.
func (s *hostSandbox) Exec(ctx context.Context, cmd string, timeout time.Duration) (sandbox.ExecResult, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	var stdout, stderr bytes.Buffer
	c.Stdout, c.Stderr = &stdout, &stderr
	err := c.Run()
	res := sandbox.ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
			return res, nil
		}
		return res, fmt.Errorf("host: exec: %w", err)
	}
	return res, nil
}

// ReadFile reads a file from the host filesystem.
func (s *hostSandbox) ReadFile(_ context.Context, path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("host: read file: %w", err)
	}
	return b, nil
}

// WriteFile writes a file on the host filesystem.
func (s *hostSandbox) WriteFile(_ context.Context, path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("host: write file: %w", err)
	}
	return nil
}

// Stop closes the underlying computer.
func (s *hostSandbox) Stop(context.Context) error { return s.comp.Close() }
