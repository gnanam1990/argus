//go:build !robotgo

// Package robotgo's native driver is available only under the `robotgo` build
// tag (it needs CGo and per-OS native libraries). This stub keeps the package
// present and buildable in the default, CGo-free build so `go build ./...`,
// `go vet`, and staticcheck do not fail on an all-excluded directory.
package robotgo
