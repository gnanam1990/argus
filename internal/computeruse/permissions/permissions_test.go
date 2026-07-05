package permissions_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gnanam1990/argus/internal/computeruse/permissions"
)

// fakeChecker is a hermetic permissions.Checker: it returns whatever status
// and error were configured, never touching the OS.
type fakeChecker struct {
	status Status
	err    error
}

// Status is a local alias so table entries below read a bit shorter; it is
// exactly permissions.Status.
type Status = permissions.Status

func (f fakeChecker) Check(context.Context) (permissions.Status, error) {
	return f.status, f.err
}

// fakeGuardian is a hermetic permissions.Guardian.
type fakeGuardian struct {
	locked bool
	err    error
}

func (f fakeGuardian) IsLocked(context.Context) (bool, error) {
	return f.locked, f.err
}

func TestEnsure(t *testing.T) {
	t.Parallel()

	granted := Status{Accessibility: true, ScreenRecording: true}
	boom := errors.New("boom")

	tests := []struct {
		name        string
		checker     fakeChecker
		guardian    fakeGuardian
		wantErr     error    // checked with errors.Is; nil means Ensure must return nil
		wantSubstrs []string // substrings the error message must contain
	}{
		{
			name:     "all granted and unlocked returns nil",
			checker:  fakeChecker{status: granted},
			guardian: fakeGuardian{locked: false},
			wantErr:  nil,
		},
		{
			name:     "locked screen short-circuits before checking permissions",
			checker:  fakeChecker{status: granted}, // would be fine, must not matter
			guardian: fakeGuardian{locked: true},
			wantErr:  permissions.ErrScreenLocked,
		},
		{
			name:        "missing accessibility names the pane",
			checker:     fakeChecker{status: Status{Accessibility: false, ScreenRecording: true}},
			guardian:    fakeGuardian{locked: false},
			wantErr:     permissions.ErrPermissionsMissing,
			wantSubstrs: []string{"Accessibility", "System Settings > Privacy & Security > Accessibility"},
		},
		{
			name:        "missing screen recording names the pane",
			checker:     fakeChecker{status: Status{Accessibility: true, ScreenRecording: false}},
			guardian:    fakeGuardian{locked: false},
			wantErr:     permissions.ErrPermissionsMissing,
			wantSubstrs: []string{"Screen Recording", "System Settings > Privacy & Security > Screen Recording"},
		},
		{
			name:     "missing both names both panes",
			checker:  fakeChecker{status: Status{}},
			guardian: fakeGuardian{locked: false},
			wantErr:  permissions.ErrPermissionsMissing,
			wantSubstrs: []string{
				"System Settings > Privacy & Security > Accessibility",
				"System Settings > Privacy & Security > Screen Recording",
			},
		},
		{
			name:     "guardian pending propagates as ErrPending",
			checker:  fakeChecker{status: granted},
			guardian: fakeGuardian{err: fmt.Errorf("wrap: %w", permissions.ErrPending)},
			wantErr:  permissions.ErrPending,
		},
		{
			name:     "checker pending propagates as ErrPending",
			checker:  fakeChecker{err: fmt.Errorf("wrap: %w", permissions.ErrPending)},
			guardian: fakeGuardian{locked: false},
			wantErr:  permissions.ErrPending,
		},
		{
			name:     "guardian hard error propagates unwrapped",
			checker:  fakeChecker{status: granted},
			guardian: fakeGuardian{err: boom},
			wantErr:  boom,
		},
		{
			name:     "checker hard error propagates unwrapped",
			checker:  fakeChecker{err: boom},
			guardian: fakeGuardian{locked: false},
			wantErr:  boom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			o := permissions.New(tt.checker, tt.guardian)
			err := o.Ensure(context.Background())
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Ensure() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Ensure() = %v, want error wrapping %v", err, tt.wantErr)
			}
			for _, s := range tt.wantSubstrs {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("Ensure() error %q missing substring %q", err.Error(), s)
				}
			}
		})
	}
}

func TestDefaultOrchestratorIsLocked(t *testing.T) {
	t.Parallel()

	g := fakeGuardian{locked: true}
	o := permissions.New(fakeChecker{}, g)

	locked, err := o.IsLocked(context.Background())
	if err != nil {
		t.Fatalf("IsLocked() error = %v", err)
	}
	if !locked {
		t.Errorf("IsLocked() = false, want true (delegates to Guardian)")
	}
}

func TestDefaultOrchestratorIsLockedError(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	o := permissions.New(fakeChecker{}, fakeGuardian{err: boom})

	if _, err := o.IsLocked(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("IsLocked() error = %v, want boom", err)
	}
}
