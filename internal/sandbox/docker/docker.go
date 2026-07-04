// Package docker is a sandbox.Provider backed by the docker CLI. It launches a
// container running guestd, drives it with a RemoteComputer, and offers gated
// exec/file operations via `docker exec`. Provisioning uses an injectable
// command runner so the run-args are unit-tested hermetically; a build-tagged
// integration test exercises a real daemon.
//
// Teardown is guaranteed: Stop force-removes the container, and the container is
// started with --rm so a crashed agent does not orphan it.
package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/gnanam1990/argus/internal/driver/remote"
	"github.com/gnanam1990/argus/pkg/computer"
	"github.com/gnanam1990/argus/pkg/sandbox"
)

// Runner executes a command with optional stdin and returns stdout. On a
// non-zero exit it returns the stdout plus the *exec.ExitError.
type Runner interface {
	Run(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error)
}

// ExecRunner runs commands with os/exec.
type ExecRunner struct{}

// Run executes name with args, feeding stdin, returning stdout.
func (ExecRunner) Run(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	return out.Bytes(), err
}

// Provider provisions docker container sandboxes.
type Provider struct {
	run       Runner
	image     string
	guestPort int
	hostPort  int
	token     string
	health    func(ctx context.Context, baseURL string) error
}

// Option configures a Provider.
type Option func(*Provider)

// WithRunner overrides the command runner (for tests).
func WithRunner(r Runner) Option { return func(p *Provider) { p.run = r } }

// WithImage sets the default guest image.
func WithImage(image string) Option { return func(p *Provider) { p.image = image } }

// WithPorts sets the guest and published host ports.
func WithPorts(host, guest int) Option {
	return func(p *Provider) { p.hostPort, p.guestPort = host, guest }
}

// WithToken sets the guest bearer token (also injected into the container).
func WithToken(token string) Option { return func(p *Provider) { p.token = token } }

// WithHealthCheck overrides the readiness check (default: HTTP poll of /status).
func WithHealthCheck(fn func(ctx context.Context, baseURL string) error) Option {
	return func(p *Provider) { p.health = fn }
}

// New builds a docker provider.
func New(opts ...Option) *Provider {
	p := &Provider{
		run:       ExecRunner{},
		image:     "argus-guest:latest",
		guestPort: 7180,
		hostPort:  7180,
	}
	for _, o := range opts {
		o(p)
	}
	if p.health == nil {
		p.health = httpHealthCheck
	}
	return p
}

var _ sandbox.Provider = (*Provider)(nil)

// Provision launches a container running guestd and returns a sandbox driving it.
func (p *Provider) Provision(ctx context.Context, spec sandbox.Spec) (sandbox.Sandbox, error) {
	image := p.image
	if spec.Image != "" {
		image = spec.Image
	}

	args := []string{"run", "-d", "--rm", "-p", fmt.Sprintf("%d:%d", p.hostPort, p.guestPort)}
	for k, v := range spec.Env {
		args = append(args, "-e", k+"="+v)
	}
	if p.token != "" {
		args = append(args, "-e", "ARGUS_GUEST_TOKEN="+p.token)
	}
	args = append(args, image)

	out, err := p.run.Run(ctx, nil, "docker", args...)
	if err != nil {
		return nil, fmt.Errorf("docker run: %w", err)
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		return nil, fmt.Errorf("docker run: empty container id")
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", p.hostPort)
	if err := p.health(ctx, baseURL); err != nil {
		_, _ = p.run.Run(context.Background(), nil, "docker", "rm", "-f", id)
		return nil, fmt.Errorf("docker: guest not ready: %w", err)
	}

	var ropts []remote.Option
	if p.token != "" {
		ropts = append(ropts, remote.WithToken(p.token))
	}
	return &dockerSandbox{run: p.run, id: id, comp: remote.New(baseURL, ropts...)}, nil
}

type dockerSandbox struct {
	run  Runner
	id   string
	comp computer.Computer
}

func (s *dockerSandbox) Computer() computer.Computer { return s.comp }

// Exec runs a command inside the container. A non-zero exit is reported via
// ExecResult.ExitCode, not as an error.
func (s *dockerSandbox) Exec(ctx context.Context, cmd string, timeout time.Duration) (sandbox.ExecResult, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	out, err := s.run.Run(ctx, nil, "docker", "exec", s.id, "sh", "-c", cmd)
	res := sandbox.ExecResult{Stdout: string(out)}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
			return res, nil
		}
		return res, fmt.Errorf("docker exec: %w", err)
	}
	return res, nil
}

// ReadFile reads a file from the container.
func (s *dockerSandbox) ReadFile(ctx context.Context, path string) ([]byte, error) {
	out, err := s.run.Run(ctx, nil, "docker", "exec", s.id, "cat", path)
	if err != nil {
		return nil, fmt.Errorf("docker read %s: %w", path, err)
	}
	return out, nil
}

// WriteFile writes data to a file in the container via stdin.
func (s *dockerSandbox) WriteFile(ctx context.Context, path string, data []byte) error {
	_, err := s.run.Run(ctx, data, "docker", "exec", "-i", s.id, "sh", "-c", "cat > "+shellQuote(path))
	if err != nil {
		return fmt.Errorf("docker write %s: %w", path, err)
	}
	return nil
}

// Stop force-removes the container (guaranteed teardown / orphan reaping).
func (s *dockerSandbox) Stop(ctx context.Context) error {
	_, err := s.run.Run(ctx, nil, "docker", "rm", "-f", s.id)
	if err != nil {
		return fmt.Errorf("docker stop: %w", err)
	}
	return nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
