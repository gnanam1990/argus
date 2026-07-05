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

// DisplayBounder reports the global bounds (in logical points) of the display
// the computer is driving: (x, y) top-left origin in the whole-desktop space,
// and (w, h) size. It lets callers that also work in the global coordinate
// space — e.g. the accessibility tree/hit-test — align with the display this
// computer captures. A single-display or whole-desktop driver need not
// implement it.
type DisplayBounder interface {
	DisplayBounds() (x, y, w, h int)
}

// BackgroundClicker presses the UI element at a screen point WITHOUT moving the
// pointer — e.g. via the accessibility API's press action — so the agent can
// drive an app while the operator keeps using the mouse. It returns
// ErrNoBackgroundTarget when there is no actionable element at that point, so
// the executor can fall back to a normal cursor click.
type BackgroundClicker interface {
	BackgroundClick(ctx context.Context, x, y int) error
}
