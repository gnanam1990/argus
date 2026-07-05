package instructions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestList(t *testing.T) {
	insts, err := List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(insts) != 3 {
		t.Fatalf("List() returned %d instructions, want 3", len(insts))
	}

	want := map[string]string{
		"com.apple.clock":      "Clock",
		"com.apple.Notes":      "Notes",
		"com.apple.calculator": "Calculator",
	}
	seen := make(map[string]bool)
	for _, inst := range insts {
		wantName, ok := want[inst.BundleIdentifier]
		if !ok {
			t.Errorf("unexpected bundle id in List(): %q", inst.BundleIdentifier)
			continue
		}
		if inst.AppName != wantName {
			t.Errorf("List()[%q].AppName = %q, want %q", inst.BundleIdentifier, inst.AppName, wantName)
		}
		if inst.Markdown == "" {
			t.Errorf("List()[%q].Markdown is empty", inst.BundleIdentifier)
		}
		seen[inst.BundleIdentifier] = true
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("List() missing bundle id %q", id)
		}
	}
}

func TestDefaultDir(t *testing.T) {
	dir, err := dirFor(func() (string, error) { return "/home/example/.config", nil })
	if err != nil {
		t.Fatalf("dirFor() error = %v", err)
	}
	want := filepath.Join("/home/example/.config", "argus", "cu-instructions")
	if dir != want {
		t.Errorf("dirFor() = %q, want %q", dir, want)
	}
}

func TestDefaultDir_PropagatesError(t *testing.T) {
	wantErr := errors.New("no config dir")
	_, err := dirFor(func() (string, error) { return "", wantErr })
	if !errors.Is(err, wantErr) {
		t.Errorf("dirFor() error = %v, want %v", err, wantErr)
	}
}

// tempConfigDir returns a UserConfigDirFunc rooted at t.TempDir(), keeping
// the test hermetic (no writes outside the test's own temp directory).
func tempConfigDir(t *testing.T) (string, UserConfigDirFunc) {
	t.Helper()
	root := t.TempDir()
	return root, func() (string, error) { return root, nil }
}

func writeOverride(t *testing.T, root, bundleID, content string) {
	t.Helper()
	dir := filepath.Join(root, "argus", "cu-instructions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	path := filepath.Join(dir, bundleID+".md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func TestChainLoader_FilesystemOverrideWinsOverEmbedded(t *testing.T) {
	root, ucd := tempConfigDir(t)
	const override = "# Clock (custom)\n\nMy own tips.\n"
	writeOverride(t, root, "com.apple.clock", override)

	loader := NewChainLoader(nil, ucd)
	got, err := loader.Load(context.Background(), "com.apple.clock")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Markdown != override {
		t.Errorf("Load().Markdown = %q, want override %q", got.Markdown, override)
	}
	if got.BundleIdentifier != "com.apple.clock" {
		t.Errorf("Load().BundleIdentifier = %q, want com.apple.clock", got.BundleIdentifier)
	}
	if got.AppName != "Clock" {
		t.Errorf("Load().AppName = %q, want Clock", got.AppName)
	}
}

func TestChainLoader_FallsBackToEmbedded(t *testing.T) {
	_, ucd := tempConfigDir(t) // empty dir: no override file written

	loader := NewChainLoader(nil, ucd)
	got, err := loader.Load(context.Background(), "com.apple.Notes")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	builtins, err := List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	var want Instruction
	for _, b := range builtins {
		if b.BundleIdentifier == "com.apple.Notes" {
			want = b
		}
	}
	if got != want {
		t.Errorf("Load() = %+v, want embedded %+v", got, want)
	}
}

func TestChainLoader_UnknownBundleReturnsEmpty(t *testing.T) {
	_, ucd := tempConfigDir(t)

	loader := NewChainLoader(nil, ucd)
	got, err := loader.Load(context.Background(), "com.example.nonexistent")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != (Instruction{}) {
		t.Errorf("Load() = %+v, want zero value", got)
	}
}

func TestChainLoader_UserConfigDirErrorFallsBackToEmbedded(t *testing.T) {
	ucd := func() (string, error) { return "", errors.New("no home dir") }

	loader := NewChainLoader(nil, ucd)
	got, err := loader.Load(context.Background(), "com.apple.calculator")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.BundleIdentifier != "com.apple.calculator" || got.Markdown == "" {
		t.Errorf("Load() = %+v, want embedded calculator instruction", got)
	}
}

func TestChainLoader_InjectedReadFile(t *testing.T) {
	var gotPath string
	readFile := func(path string) ([]byte, error) {
		gotPath = path
		return []byte("fake content"), nil
	}
	ucd := func() (string, error) { return "/cfg", nil }

	loader := NewChainLoader(readFile, ucd)
	got, err := loader.Load(context.Background(), "com.apple.clock")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	wantPath := filepath.Join("/cfg", "argus", "cu-instructions", "com.apple.clock.md")
	if gotPath != wantPath {
		t.Errorf("readFile called with path %q, want %q", gotPath, wantPath)
	}
	if got.Markdown != "fake content" {
		t.Errorf("Load().Markdown = %q, want %q", got.Markdown, "fake content")
	}
}

func TestChainLoader_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	loader := NewChainLoader(
		func(string) ([]byte, error) { return nil, os.ErrNotExist },
		func() (string, error) { return "/cfg", nil },
	)
	_, err := loader.Load(ctx, "com.apple.clock")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Load() error = %v, want context.Canceled", err)
	}
}

func TestDefaultDir_Function(t *testing.T) {
	// Smoke test: DefaultDir must not panic and, when it succeeds, must
	// end in the expected suffix.
	dir, err := DefaultDir()
	if err != nil {
		// os.UserConfigDir can fail in constrained CI sandboxes; that's
		// an acceptable outcome for this smoke test.
		return
	}
	want := filepath.Join("argus", "cu-instructions")
	if !strings.HasSuffix(dir, want) {
		t.Errorf("DefaultDir() = %q, want to end with %q", dir, want)
	}
}
