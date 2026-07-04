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

// nameFromRunArgs extracts the value passed to --name in a `docker run`
// argument list, for asserting the same generated name is used consistently
// across the run call and any later exec/rm calls.
func nameFromRunArgs(args []string) string {
	for i, a := range args {
		if a == "--name" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
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
	name := sb.(*dockerSandbox).id

	res, err := sb.Exec(context.Background(), "ls -la", time.Second)
	if err != nil || res.Stdout != "cmd-output" {
		t.Fatalf("exec = %+v, %v", res, err)
	}
	if !contains(f.last(), "exec", name, "sh", "-c", "ls -la") {
		t.Errorf("exec args = %v", f.last())
	}

	data, err := sb.ReadFile(context.Background(), "/etc/hosts")
	if err != nil || string(data) != "cmd-output" {
		t.Fatalf("read = %q, %v", data, err)
	}
	if !contains(f.last(), "exec", name, "cat", "/etc/hosts") {
		t.Errorf("read args = %v", f.last())
	}

	if err := sb.WriteFile(context.Background(), "/tmp/x", []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if !contains(f.last(), "exec", "-i", name, "cat > '/tmp/x'") {
		t.Errorf("write args = %v", f.last())
	}
	if string(f.stdins[len(f.stdins)-1]) != "payload" {
		t.Errorf("write stdin = %q, want payload", f.stdins[len(f.stdins)-1])
	}

	if err := sb.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !contains(f.last(), "rm", "-f", name) {
		t.Errorf("stop args = %v", f.last())
	}
}

// TestProvisionUsesGeneratedContainerName checks the container is named
// up front (argus-guest-<hex>, via --name) and that name — not anything
// parsed from docker's stdout — is what the sandbox uses as its identifier.
func TestProvisionUsesGeneratedContainerName(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{}
	sb, err := newProvider(f).Provision(context.Background(), specEmpty())
	if err != nil {
		t.Fatal(err)
	}
	run := f.calls[0]
	if !contains(run, "run", "-d", "--rm", "--name") {
		t.Errorf("run args missing --name: %v", run)
	}
	ds, ok := sb.(*dockerSandbox)
	if !ok || !strings.HasPrefix(ds.id, "argus-guest-") {
		t.Errorf("sandbox id = %+v, want an argus-guest-<hex> prefix", sb)
	}
	if got := nameFromRunArgs(run); got != ds.id {
		t.Errorf("--name value %q does not match sandbox id %q", got, ds.id)
	}
}

// TestProvisionIgnoresRunStdout replaces the old "empty container id" check:
// since the container is named up front and --name'd explicitly, whatever
// `docker run` prints to stdout (even garbage) no longer matters for
// identity, so provisioning must still succeed.
func TestProvisionIgnoresRunStdout(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{runID: "   "}
	sb, err := newProvider(f).Provision(context.Background(), specEmpty())
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if sb == nil {
		t.Fatal("nil sandbox")
	}
}

// TestProvisionCancelledRunReapsGeneratedName is the H8-adjacent "pre-ID
// orphan fix" regression test: even when `docker run` itself returns an
// error (e.g. because ctx was cancelled mid-invocation and the daemon may
// have created the container anyway), Provision must reap the container by
// the SAME name it passed via --name.
func TestProvisionCancelledRunReapsGeneratedName(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{err: context.Canceled}
	if _, err := newProvider(f).Provision(context.Background(), specEmpty()); err == nil {
		t.Fatal("expected an error from the cancelled/failed run")
	}
	name := nameFromRunArgs(f.calls[0])
	if name == "" {
		t.Fatal("run args missing --name")
	}
	if !contains(f.last(), "rm", "-f", name) {
		t.Fatalf("expected a reap call for %q; last call = %v", name, f.last())
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
	name := nameFromRunArgs(f.calls[0])
	if name == "" {
		t.Fatal("run args missing --name")
	}
	// The failed container must be reaped by the same generated name.
	if !contains(f.last(), "rm", "-f", name) {
		t.Errorf("failed provision should reap %q; last = %v", name, f.last())
	}
}

func TestProvisionRejectsFlagShapedImage(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{}
	if _, err := newProvider(f).Provision(context.Background(), sandbox.Spec{Image: "--privileged"}); err == nil {
		t.Fatal("expected an error for a flag-shaped image")
	}
	if len(f.calls) != 0 {
		t.Errorf("docker should never be invoked for a rejected image; calls = %v", f.calls)
	}
}

func TestProvisionRejectsFlagShapedEnvKey(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{}
	spec := sandbox.Spec{Env: map[string]string{"--evil": "1"}}
	if _, err := newProvider(f).Provision(context.Background(), spec); err == nil {
		t.Fatal("expected an error for a flag-shaped env key")
	}
	if len(f.calls) != 0 {
		t.Errorf("docker should never be invoked for a rejected env key; calls = %v", f.calls)
	}
}

func TestProvisionRejectsInvalidPort(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{}
	spec := sandbox.Spec{Ports: []int{70000}}
	if _, err := newProvider(f).Provision(context.Background(), spec); err == nil {
		t.Fatal("expected an error for an out-of-range port")
	}
	if len(f.calls) != 0 {
		t.Errorf("docker should never be invoked for a rejected port; calls = %v", f.calls)
	}
}

// TestProvisionMergesSpecPorts checks sandbox.Spec.Ports are merged into the
// -p mappings (guestPort:guestPort form) alongside the provider's own
// WithPorts mapping, rather than being silently ignored.
func TestProvisionMergesSpecPorts(t *testing.T) {
	t.Parallel()
	f := &fakeRunner{}
	p := newProvider(f, WithPorts(9000, 7180))
	spec := sandbox.Spec{Ports: []int{8081, 9090}}
	if _, err := p.Provision(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	run := f.calls[0]
	if !contains(run, "-p", "9000:7180") || !contains(run, "-p", "8081:8081") || !contains(run, "-p", "9090:9090") {
		t.Errorf("run args missing merged ports: %v", run)
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
