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
	"crypto/rand"
	"encoding/hex"
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
	if err := validateImage(image); err != nil {
		return nil, err
	}
	if err := validatePort(p.hostPort); err != nil {
		return nil, err
	}
	if err := validatePort(p.guestPort); err != nil {
		return nil, err
	}

	// The container name is generated up front (rather than parsed from
	// `docker run`'s stdout) so it's known even if the run invocation never
	// returns one — e.g. ctx is cancelled mid-provision after the daemon has
	// already created the container — and Stop/reap can always target it.
	name, err := generateContainerName()
	if err != nil {
		return nil, err
	}

	args := []string{"run", "-d", "--rm", "--name", name, "-p", fmt.Sprintf("%d:%d", p.hostPort, p.guestPort)}
	for _, port := range spec.Ports {
		if err := validatePort(port); err != nil {
			return nil, err
		}
		args = append(args, "-p", fmt.Sprintf("%d:%d", port, port))
	}
	for k, v := range spec.Env {
		if err := validateEnvKey(k); err != nil {
			return nil, err
		}
		args = append(args, "-e", k+"="+v)
	}
	if p.token != "" {
		args = append(args, "-e", "ARGUS_GUEST_TOKEN="+p.token)
	}
	args = append(args, image)

	if _, err := p.run.Run(ctx, nil, "docker", args...); err != nil {
		// The daemon may have created the container even though this CLI
		// invocation failed client-side (e.g. ctx cancellation killed `docker
		// run` before it printed anything) — reap it by the name chosen
		// above, using a background context so the same cancellation can't
		// also abort the cleanup.
		reap(p.run, name)
		return nil, fmt.Errorf("docker run: %w", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", p.hostPort)
	if err := p.health(ctx, baseURL); err != nil {
		reap(p.run, name)
		return nil, fmt.Errorf("docker: guest not ready: %w", err)
	}

	var ropts []remote.Option
	if p.token != "" {
		ropts = append(ropts, remote.WithToken(p.token))
	}
	return &dockerSandbox{run: p.run, id: name, comp: remote.New(baseURL, ropts...)}, nil
}

// generateContainerName returns a fresh argus-guest-<hex> name using
// crypto/rand (not math/rand — this becomes part of a command line, and
// crypto/rand keeps it unpredictable and collision-resistant across
// concurrent provisions).
func generateContainerName() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("docker: generate container name: %w", err)
	}
	return "argus-guest-" + hex.EncodeToString(b), nil
}

// reap best-effort force-removes a container by name, using a background
// context so a cancellation that caused the failure being handled can't also
// prevent cleanup.
func reap(run Runner, name string) {
	_, _ = run.Run(context.Background(), nil, "docker", "rm", "-f", name)
}

// validateImage rejects an image value that docker's CLI parser could read as
// a flag instead of the image positional (e.g. spec.Image == "--privileged"
// landing where the image argument is expected). `docker run` has no "--"
// separator between options and the image, so rejecting flag-shaped values is
// the mitigation.
func validateImage(image string) error {
	if image == "" || strings.HasPrefix(image, "-") {
		return fmt.Errorf("docker: invalid image %q", image)
	}
	return nil
}

// validateEnvKey rejects an env var name that could be misread as a flag.
func validateEnvKey(key string) error {
	if key == "" || strings.HasPrefix(key, "-") {
		return fmt.Errorf("docker: invalid env key %q", key)
	}
	return nil
}

// validatePort rejects a port number outside the valid TCP range.
func validatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("docker: invalid port %d (must be 1-65535)", port)
	}
	return nil
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
