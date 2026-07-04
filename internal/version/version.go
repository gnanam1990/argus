// Package version exposes build metadata for the argus binaries.
//
// The exported variables are intended to be overwritten at build time via the
// linker, e.g.:
//
//	go build -ldflags "-X github.com/gnanam1990/argus/internal/version.Version=1.2.3 ..."
//
// See the Makefile for the canonical invocation.
package version

import "fmt"

// Build metadata. Defaults describe an unstamped local ("dev") build and are
// overwritten by -ldflags -X for release binaries.
var (
	// Version is the semantic version (e.g. "1.2.3") or "dev" for local builds.
	Version = "dev"
	// Commit is the short git SHA the binary was built from.
	Commit = ""
	// Date is the RFC 3339 UTC build timestamp.
	Date = ""
)

// Info formats the given build fields into a single human-readable line,
// applying dev-build fallbacks for any empty field. It is a pure function so
// the formatting is unit-testable without depending on link-time variables.
func Info(version, commit, date string) string {
	if version == "" {
		version = "dev"
	}
	if commit == "" {
		commit = "none"
	}
	if date == "" {
		date = "unknown"
	}
	return fmt.Sprintf("argus %s (commit %s, built %s)", version, commit, date)
}

// String returns the formatted version info for the current build.
func String() string {
	return Info(Version, Commit, Date)
}
