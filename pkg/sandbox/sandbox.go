// Package sandbox is the contract for a provisioned environment the agent
// drives: a Computer to observe/act, plus gated exec and file operations. The
// same Sandbox interface covers a local host, a container, or a cloud VM — only
// the provider differs — so the agent loop is identical everywhere.
package sandbox

import (
	"context"
	"errors"
	"time"

	"github.com/gnanam1990/argus/pkg/computer"
)

// ErrNotPermitted is returned by gated operations a provider does not allow
// (e.g. exec on a bare host sandbox).
var ErrNotPermitted = errors.New("sandbox: operation not permitted")

// ExecResult is the outcome of running a command in the sandbox.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Spec describes an environment to provision.
type Spec struct {
	Image string            // container/VM image (provider-specific)
	Env   map[string]string // environment variables
	Ports []int             // guest ports to publish
}

// Sandbox is a provisioned environment. Exec/ReadFile/WriteFile are gated: a
// provider may return ErrNotPermitted for operations outside its trust model.
type Sandbox interface {
	Computer() computer.Computer
	Exec(ctx context.Context, cmd string, timeout time.Duration) (ExecResult, error)
	ReadFile(ctx context.Context, path string) ([]byte, error)
	WriteFile(ctx context.Context, path string, data []byte) error
	Stop(ctx context.Context) error
}

// Provider provisions sandboxes.
type Provider interface {
	Provision(ctx context.Context, spec Spec) (Sandbox, error)
}
