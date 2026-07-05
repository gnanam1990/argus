// Package instructions loads optional operating guidance for the
// computer-use agent, keyed by macOS application bundle identifier.
//
// Guidance is Markdown text intended to be folded into the agent's prompt
// when it operates a particular application (for example, hints about
// which tab to click before acting, or how a mode toggle changes button
// labels). Guidance is entirely optional: an application with no guidance
// simply yields an empty Instruction and a nil error.
//
// Lookup follows a chain: a user-editable file on disk is preferred over a
// small built-in set embedded in the binary. The filesystem read and the
// user config directory lookup are both injectable so tests never touch
// the real filesystem or environment.
package instructions

import (
	"context"
	"embed"
	"errors"
	"os"
	"path/filepath"
)

//go:embed builtins/*.md
var builtinFS embed.FS

// Instruction is operating guidance for a single application.
type Instruction struct {
	// BundleIdentifier is the macOS application bundle id the guidance
	// applies to, e.g. "com.apple.Notes".
	BundleIdentifier string
	// AppName is a human-readable application name, e.g. "Notes".
	AppName string
	// Markdown is the guidance text itself.
	Markdown string
}

// Loader loads the Instruction for a given application bundle identifier.
// A bundle id with no known guidance returns a zero-value Instruction and
// a nil error; guidance is always optional.
type Loader interface {
	Load(ctx context.Context, bundleID string) (Instruction, error)
}

// ReadFileFunc reads the contents of a file, matching the signature of
// os.ReadFile. It is injected into ChainLoader so tests can avoid touching
// the real filesystem outside of t.TempDir.
type ReadFileFunc func(path string) ([]byte, error)

// UserConfigDirFunc returns the user's configuration directory, matching
// the signature of os.UserConfigDir. It is injected into ChainLoader so
// tests can avoid depending on the real environment.
type UserConfigDirFunc func() (string, error)

// builtinManifest lists the applications shipped with built-in guidance,
// in the order List returns them.
var builtinManifest = []struct {
	BundleIdentifier string
	AppName          string
	File             string
}{
	{"com.apple.clock", "Clock", "com.apple.clock.md"},
	{"com.apple.Notes", "Notes", "com.apple.Notes.md"},
	{"com.apple.calculator", "Calculator", "com.apple.calculator.md"},
}

// builtinByID is a lookup index over the built-in instructions, populated
// once at init from the embedded files.
var builtinByID map[string]Instruction

func init() {
	insts, err := List()
	if err != nil {
		// The embedded files are compiled into the binary; a read
		// failure here indicates a build-time defect, not a runtime
		// condition callers can recover from.
		panic("instructions: failed to load embedded built-ins: " + err.Error())
	}
	builtinByID = make(map[string]Instruction, len(insts))
	for _, inst := range insts {
		builtinByID[inst.BundleIdentifier] = inst
	}
}

// List returns all built-in instructions shipped with the binary, in a
// stable order. It is used to power an "instructions list" command as
// well as to populate the built-in fallback lookup.
func List() ([]Instruction, error) {
	out := make([]Instruction, 0, len(builtinManifest))
	for _, m := range builtinManifest {
		data, err := builtinFS.ReadFile("builtins/" + m.File)
		if err != nil {
			return nil, err
		}
		out = append(out, Instruction{
			BundleIdentifier: m.BundleIdentifier,
			AppName:          m.AppName,
			Markdown:         string(data),
		})
	}
	return out, nil
}

// DefaultDir returns the directory that ChainLoader's default construction
// searches for user-supplied instruction overrides:
// <userConfigDir>/argus/cu-instructions.
func DefaultDir() (string, error) {
	return dirFor(os.UserConfigDir)
}

func dirFor(userConfigDir UserConfigDirFunc) (string, error) {
	dir, err := userConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "argus", "cu-instructions"), nil
}

// ChainLoader is a Loader that first checks a user-editable Markdown file
// on disk, then falls back to the built-in embedded set. A bundle id
// found in neither location yields an empty Instruction and a nil error.
type ChainLoader struct {
	readFile      ReadFileFunc
	userConfigDir UserConfigDirFunc
	extraDirs     []string // additional override dirs, checked after the default
}

// NewChainLoader builds a ChainLoader. Either function dependency may be nil,
// in which case it defaults to the real os.ReadFile / os.UserConfigDir; tests
// should always supply both explicitly to remain hermetic. extraDirs are
// additional directories searched for per-app overrides (from
// config.ComputerUse.InstructionDirs), checked after the default
// <userConfigDir>/argus/cu-instructions dir and before the built-in set.
func NewChainLoader(readFile ReadFileFunc, userConfigDir UserConfigDirFunc, extraDirs ...string) *ChainLoader {
	if readFile == nil {
		readFile = os.ReadFile
	}
	if userConfigDir == nil {
		userConfigDir = os.UserConfigDir
	}
	return &ChainLoader{readFile: readFile, userConfigDir: userConfigDir, extraDirs: extraDirs}
}

// Load implements Loader.
func (c *ChainLoader) Load(ctx context.Context, bundleID string) (Instruction, error) {
	if err := ctx.Err(); err != nil {
		return Instruction{}, err
	}

	// Search the default user dir first, then any configured extra dirs, then
	// the built-in set. The first Markdown file found wins.
	dirs := make([]string, 0, 1+len(c.extraDirs))
	if dir, err := dirFor(c.userConfigDir); err == nil {
		dirs = append(dirs, dir)
	}
	dirs = append(dirs, c.extraDirs...)

	for _, dir := range dirs {
		path := filepath.Join(dir, bundleID+".md")
		data, ferr := c.readFile(path)
		if ferr == nil {
			return Instruction{
				BundleIdentifier: bundleID,
				AppName:          appNameFor(bundleID),
				Markdown:         string(data),
			}, nil
		}
		// Any error other than "no override present" is treated the same way:
		// fall through to the next dir / the built-in set. The override is a
		// best-effort convenience, not a hard dependency, so a permission error
		// or similar should not prevent guidance from being available at all.
		if !errors.Is(ferr, os.ErrNotExist) {
			_ = ferr
		}
	}

	if inst, ok := builtinByID[bundleID]; ok {
		return inst, nil
	}

	return Instruction{}, nil
}

// appNameFor returns the human-readable app name for a known bundle id, or
// the bundle id itself if it is not part of the built-in manifest.
func appNameFor(bundleID string) string {
	for _, m := range builtinManifest {
		if m.BundleIdentifier == bundleID {
			return m.AppName
		}
	}
	return bundleID
}
