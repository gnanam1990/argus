package computer

import "context"

// Optional capability interfaces a Computer may additionally implement. The
// Executor type-asserts for these when dispatching gated actions: a Computer
// that implements them (e.g. a sandbox-backed computer assembled by the app
// layer) gains run_command / read_file / write_file execution; everything else
// keeps returning ErrUnsupported. The capability allowlist and the approval /
// injection middleware still gate these actions regardless of the interfaces.

// Commander runs a shell command where the computer lives and returns its
// combined output.
type Commander interface {
	RunCommand(ctx context.Context, cmd string) (string, error)
}

// FileReader reads a file from where the computer lives.
type FileReader interface {
	ReadFile(ctx context.Context, path string) ([]byte, error)
}

// FileWriter writes a file where the computer lives.
type FileWriter interface {
	WriteFile(ctx context.Context, path string, data []byte) error
}
