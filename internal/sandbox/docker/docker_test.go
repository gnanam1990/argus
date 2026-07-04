package docker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gnanam1990/argus/pkg/sandbox"
)

type fakeRunner struct {
	calls  [][]string
	stdins [][]byte
	runID  string
	err    error
}

func (f *fakeRunner) Run(_ context.Context, stdin []byte, _ string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	f.stdins = append(f.stdins, stdin)
	if f.err != nil {
		return nil, f.err
	}
	if len(args) > 0 && args[0] == "run" {
		id := f.runID
		if id == "" {
			id = "container123"
		}
		return []byte(id + "\n"), nil
	}
	return []byte("cmd-output"), nil
}

func (f *fakeRunner) last() []string { return f.calls[len(f.calls)-1] }

func noHealth(context.Context, string) error { return nil }

func contains(hay []string, needles ...string) bool {
	joined := strings.Join(hay, " ")
	for _, n := range needles {
		if !strings.Contains(joined, n) {
			return false
		}
	}
	return true
}

func newProvider(f *fakeRunner, opts ...Option) *Provider {
	base := []Option{WithRunner(f), WithHealthCheck(noHealth)}
	return New(append(base, opts...)...)
}

func specEmpty() sandbox.Spec { return sandbox.Spec{} }

func specWithEnv() sandbox.Spec { return sandbox.Spec{Env: map[string]string{"FOO": "bar"}} }

func TestProvisionRunArgs(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{}
	p := newProvider(f, WithImage("myimg"), WithPorts(9000, 7180), WithToken("tok"))

	sb, err := p.Provision(context.Background(), specWithEnv())
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if sb == nil {
		t.Fatal("nil sandbox")
	}
	run := f.calls[0]
	if !contains(run, "run", "-d", "--rm", "-p", "9000:7180", "myimg", "FOO=bar", "ARGUS_GUEST_TOKEN=tok") {
		t.Errorf("run args = %v", run)
	}
}

func TestExecReadWriteStop(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{}
	sb, err := newProvider(f).Provision(context.Background(), specEmpty())
	if err != nil {
		t.Fatal(err)
	}

	res, err := sb.Exec(context.Background(), "ls -la", time.Second)
	if err != nil || res.Stdout != "cmd-output" {
		t.Fatalf("exec = %+v, %v", res, err)
	}
	if !contains(f.last(), "exec", "container123", "sh", "-c", "ls -la") {
		t.Errorf("exec args = %v", f.last())
	}

	data, err := sb.ReadFile(context.Background(), "/etc/hosts")
	if err != nil || string(data) != "cmd-output" {
		t.Fatalf("read = %q, %v", data, err)
	}
	if !contains(f.last(), "exec", "container123", "cat", "/etc/hosts") {
		t.Errorf("read args = %v", f.last())
	}

	if err := sb.WriteFile(context.Background(), "/tmp/x", []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if !contains(f.last(), "exec", "-i", "container123", "cat > '/tmp/x'") {
		t.Errorf("write args = %v", f.last())
	}
	if string(f.stdins[len(f.stdins)-1]) != "payload" {
		t.Errorf("write stdin = %q, want payload", f.stdins[len(f.stdins)-1])
	}

	if err := sb.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !contains(f.last(), "rm", "-f", "container123") {
		t.Errorf("stop args = %v", f.last())
	}
}

func TestProvisionEmptyID(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{runID: "   "}
	if _, err := newProvider(f).Provision(context.Background(), specEmpty()); err == nil {
		t.Error("empty container id should error")
	}
}

func TestProvisionHealthFailsRemovesContainer(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{}
	p := New(WithRunner(f), WithHealthCheck(func(context.Context, string) error {
		return errors.New("never ready")
	}))
	if _, err := p.Provision(context.Background(), specEmpty()); err == nil {
		t.Fatal("expected health failure")
	}
	// The failed container must be reaped.
	if !contains(f.last(), "rm", "-f", "container123") {
		t.Errorf("failed provision should reap the container; last = %v", f.last())
	}
}

// exitRunner returns a fixed stdout plus a preset error (used to exercise the
// non-zero-exit branch with a real *exec.ExitError).
type exitRunner struct{ err error }

func (r exitRunner) Run(context.Context, []byte, string, ...string) ([]byte, error) {
	return []byte("out"), r.err
}

func TestExecRunnerStdin(t *testing.T) {
	t.Parallel()
	out, err := ExecRunner{}.Run(context.Background(), []byte("hello"), "cat")
	if err != nil {
		t.Fatalf("cat: %v", err)
	}
	if string(out) != "hello" {
		t.Errorf("stdin echo = %q, want hello", out)
	}
}

func TestExecNonZeroExit(t *testing.T) {
	t.Parallel()
	// Obtain a real *exec.ExitError with code 3.
	_, exitErr := ExecRunner{}.Run(context.Background(), nil, "sh", "-c", "exit 3")
	var ee *exec.ExitError
	if !errors.As(exitErr, &ee) {
		t.Fatalf("expected ExitError, got %v", exitErr)
	}
	sb := &dockerSandbox{run: exitRunner{err: exitErr}, id: "c"}
	res, err := sb.Exec(context.Background(), "false", time.Second)
	if err != nil {
		t.Fatalf("non-zero exit should not be an error: %v", err)
	}
	if res.ExitCode != 3 {
		t.Errorf("ExitCode = %d, want 3", res.ExitCode)
	}
}

func TestHealthCheckOK(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	if err := httpHealthCheck(context.Background(), srv.URL); err != nil {
		t.Errorf("healthy service = %v, want nil", err)
	}
}

func TestHealthCheckCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := httpHealthCheck(ctx, "http://127.0.0.1:0"); err == nil {
		t.Error("cancelled ctx should fail fast")
	}
}
