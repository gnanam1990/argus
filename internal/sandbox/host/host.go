// Package host is a sandbox.Provider that drives the local machine's GUI. It is
// the trusted, no-isolation option — exec and file operations are gated off
// (ErrNotPermitted), because "run a command on the host" is not something the
// host GUI-control sandbox should offer; use a container sandbox for that.
package host

import (
	"context"
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

// Exec is gated off on the host sandbox.
func (s *hostSandbox) Exec(context.Context, string, time.Duration) (sandbox.ExecResult, error) {
	return sandbox.ExecResult{}, sandbox.ErrNotPermitted
}

// ReadFile is gated off on the host sandbox.
func (s *hostSandbox) ReadFile(context.Context, string) ([]byte, error) {
	return nil, sandbox.ErrNotPermitted
}

// WriteFile is gated off on the host sandbox.
func (s *hostSandbox) WriteFile(context.Context, string, []byte) error {
	return sandbox.ErrNotPermitted
}

// Stop closes the underlying computer.
func (s *hostSandbox) Stop(context.Context) error { return s.comp.Close() }
