// Package permissions gates computer-use automation on the macOS
// preconditions it depends on: the screen must be unlocked (input events sent
// to a locked screen are either dropped or, worse, land on the login window)
// and the process must hold both the Accessibility permission (to read the
// element tree and to synthesize input) and the Screen Recording permission
// (to capture screenshots). Ensure is the single call a driver makes before
// acting; it returns one of the sentinel errors below so a caller can decide
// whether to retry, prompt the user to fix System Settings, or hand off.
//
// The real, best-effort macOS detection lives in a darwin-only file behind
// HostChecker/HostGuardian; every other platform gets a conservative
// placeholder (see the package doc on the !darwin file) so the rest of the
// tree — the Orchestrator, the driver that calls it — is fully
// cross-platform and testable without a Mac.
package permissions

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Status is a snapshot of the two macOS permissions computer-use automation
// needs. Both must be true for Ensure to succeed.
type Status struct {
	// Accessibility reports whether the process may read the accessibility
	// tree and synthesize mouse/keyboard input (System Settings > Privacy &
	// Security > Accessibility).
	Accessibility bool
	// ScreenRecording reports whether the process may capture the screen
	// (System Settings > Privacy & Security > Screen Recording).
	ScreenRecording bool
}

// Checker reports the current permission grants.
type Checker interface {
	// Check returns the current Status, or an error wrapping ErrPending if
	// the status could not be determined right now but a retry may succeed.
	Check(ctx context.Context) (Status, error)
}

// Guardian reports whether the screen is currently locked.
type Guardian interface {
	// IsLocked returns whether the screen is locked, or an error wrapping
	// ErrPending if that could not be determined right now but a retry may
	// succeed.
	IsLocked(ctx context.Context) (bool, error)
}

// Orchestrator is the single precondition gate a driver calls before it acts.
type Orchestrator interface {
	// Ensure returns nil only when the screen is unlocked and both
	// permissions are granted. Otherwise it returns ErrScreenLocked,
	// ErrPermissionsMissing (wrapped with the specific pane(s) to fix), or
	// ErrPending — see each sentinel's doc for how a caller should react.
	Ensure(ctx context.Context) error
	// IsLocked reports whether the screen is currently locked.
	IsLocked(ctx context.Context) (bool, error)
}

var (
	// ErrScreenLocked means the screen is locked. Input synthesized against a
	// locked screen is unreliable or hits the login window instead of the
	// intended app, so callers must not act until this clears; the fix is
	// for the user to unlock the machine, not something the caller can
	// change, so it is not retryable on its own — a caller may poll IsLocked
	// and retry Ensure once it reports false.
	ErrScreenLocked = errors.New("permissions: screen is locked")

	// ErrPermissionsMissing means the process lacks Accessibility and/or
	// Screen Recording. Ensure always wraps this with a message naming which
	// of the two is missing and the exact System Settings pane to grant it
	// in: "System Settings > Privacy & Security > Accessibility" and/or
	// "System Settings > Privacy & Security > Screen Recording". Fixing it
	// requires the user to grant the permission (and, for Accessibility,
	// often relaunch the process), so this is not retryable without that
	// user action.
	ErrPermissionsMissing = errors.New("permissions: required macOS permission missing")

	// ErrPending means the permission or lock status could not be
	// determined right now (e.g. the check timed out or the OS hasn't
	// finished reporting a just-changed grant) but is expected to resolve on
	// its own — callers should retry Ensure, ideally with a short backoff,
	// rather than treat it as a hard failure.
	ErrPending = errors.New("permissions: status pending, retry")

	// ErrUnsupported means permission/lock detection isn't implemented for
	// this platform or environment (e.g. a non-macOS build attempting the
	// real detector, or osascript unavailable).
	ErrUnsupported = errors.New("permissions: unsupported platform")
)

// accessibilityPane and screenRecordingPane are the exact System Settings
// panes named in ErrPermissionsMissing's message, kept as constants so the
// Ensure message and any documentation referencing them can't drift apart.
const (
	accessibilityPane   = "Accessibility (System Settings > Privacy & Security > Accessibility)"
	screenRecordingPane = "Screen Recording (System Settings > Privacy & Security > Screen Recording)"
)

// DefaultOrchestrator implements Orchestrator by composing an injected
// Checker and Guardian. It holds no platform-specific logic itself: the
// darwin-only HostChecker/HostGuardian (or a fake, in tests) supply that.
type DefaultOrchestrator struct {
	checker  Checker
	guardian Guardian
}

var _ Orchestrator = (*DefaultOrchestrator)(nil)

// New builds a DefaultOrchestrator from a Checker and Guardian. Both must be
// non-nil.
func New(checker Checker, guardian Guardian) *DefaultOrchestrator {
	return &DefaultOrchestrator{checker: checker, guardian: guardian}
}

// IsLocked delegates to the underlying Guardian.
func (o *DefaultOrchestrator) IsLocked(ctx context.Context) (bool, error) {
	return o.guardian.IsLocked(ctx)
}

// Ensure checks, in order: (1) the screen is unlocked, then (2) both
// permissions are granted. It returns nil only when every precondition
// holds. A Checker/Guardian error that wraps ErrPending is returned as-is
// (still wrapping ErrPending) so errors.Is(err, ErrPending) lets a caller
// distinguish "retry me" from a hard failure; any other Checker/Guardian
// error is likewise returned unchanged.
func (o *DefaultOrchestrator) Ensure(ctx context.Context) error {
	locked, err := o.guardian.IsLocked(ctx)
	if err != nil {
		return err
	}
	if locked {
		return ErrScreenLocked
	}

	status, err := o.checker.Check(ctx)
	if err != nil {
		return err
	}

	var missing []string
	if !status.Accessibility {
		missing = append(missing, accessibilityPane)
	}
	if !status.ScreenRecording {
		missing = append(missing, screenRecordingPane)
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %s", ErrPermissionsMissing, strings.Join(missing, "; "))
	}
	return nil
}
