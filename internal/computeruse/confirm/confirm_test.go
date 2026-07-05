package confirm

import (
	"context"
	"errors"
	"testing"

	"github.com/gnanam1990/argus/internal/computeruse/policy"
	"github.com/gnanam1990/argus/pkg/action"
)

// fakeApprover is a hermetic middleware.Approver test double: it never touches
// the OS, just records what it was asked and returns a canned answer.
type fakeApprover struct {
	answer bool
	err    error
	calls  []action.Action
}

func (f *fakeApprover) Approve(_ context.Context, a action.Action) (bool, error) {
	f.calls = append(f.calls, a)
	return f.answer, f.err
}

func TestNewConfirmation_NilDefaults(t *testing.T) {
	c := NewConfirmation(nil, nil)
	if c.classifier == nil {
		t.Fatal("expected a default classifier, got nil")
	}
	if _, ok := c.classifier.(policy.DefaultClassifier); !ok {
		t.Fatalf("expected policy.DefaultClassifier, got %T", c.classifier)
	}
	if c.approver == nil {
		t.Fatal("expected a default (fail-closed) approver, got nil")
	}
}

func TestConfirmation_OnAction(t *testing.T) {
	tests := []struct {
		name      string
		task      string
		action    action.Action
		approver  *fakeApprover // nil => construct with nil approver
		wantAllow bool
		wantErr   bool
		wantAsked bool
		wantLevel policy.RiskLevel
	}{
		{
			name:      "hand off denies without asking the approver",
			task:      "please change password on the account",
			action:    action.Action{Type: action.Click},
			approver:  &fakeApprover{answer: true},
			wantAllow: false,
			wantAsked: false,
			wantLevel: policy.HandOff,
		},
		{
			name:      "always confirm routes to approver and honors approval",
			task:      "delete the old report files",
			action:    action.Action{Type: action.Click},
			approver:  &fakeApprover{answer: true},
			wantAllow: true,
			wantAsked: true,
			wantLevel: policy.AlwaysConfirm,
		},
		{
			name:      "always confirm routes to approver and honors denial",
			task:      "delete the old report files",
			action:    action.Action{Type: action.Click},
			approver:  &fakeApprover{answer: false},
			wantAllow: false,
			wantAsked: true,
			wantLevel: policy.AlwaysConfirm,
		},
		{
			name:      "pre-approval routes to approver and honors approval",
			task:      "log in to the portal",
			action:    action.Action{Type: action.Click},
			approver:  &fakeApprover{answer: true},
			wantAllow: true,
			wantAsked: true,
			wantLevel: policy.PreApproval,
		},
		{
			name:      "pre-approval routes to approver and honors denial",
			task:      "log in to the portal",
			action:    action.Action{Type: action.Click},
			approver:  &fakeApprover{answer: false},
			wantAllow: false,
			wantAsked: true,
			wantLevel: policy.PreApproval,
		},
		{
			name:      "no confirm allows without asking the approver",
			task:      "click play on the video",
			action:    action.Action{Type: action.Click},
			approver:  &fakeApprover{answer: false},
			wantAllow: true,
			wantAsked: false,
			wantLevel: policy.NoConfirm,
		},
		{
			name:      "nil approver fails closed on always-confirm",
			task:      "delete the old report files",
			action:    action.Action{Type: action.Click},
			approver:  nil,
			wantAllow: false,
			wantLevel: policy.AlwaysConfirm,
		},
		{
			name:      "nil approver fails closed on pre-approval",
			task:      "log in to the portal",
			action:    action.Action{Type: action.Click},
			approver:  nil,
			wantAllow: false,
			wantLevel: policy.PreApproval,
		},
		{
			name:      "nil approver still allows no-confirm actions",
			task:      "click play on the video",
			action:    action.Action{Type: action.Click},
			approver:  nil,
			wantAllow: true,
			wantLevel: policy.NoConfirm,
		},
		{
			name:      "nil approver still denies hand-off",
			task:      "reset password for the account",
			action:    action.Action{Type: action.Click},
			approver:  nil,
			wantAllow: false,
			wantLevel: policy.HandOff,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var approver *fakeApprover
			var c *Confirmation
			if tt.approver != nil {
				approver = tt.approver
				c = NewConfirmation(nil, approver)
			} else {
				c = NewConfirmation(nil, nil)
			}

			ctx := context.Background()
			if err := c.OnRunStart(ctx, tt.task); err != nil {
				t.Fatalf("OnRunStart: unexpected error: %v", err)
			}

			a := tt.action
			allow, err := c.OnAction(ctx, &a)
			if (err != nil) != tt.wantErr {
				t.Fatalf("OnAction error = %v, wantErr %v", err, tt.wantErr)
			}
			if allow != tt.wantAllow {
				t.Fatalf("OnAction allow = %v, want %v", allow, tt.wantAllow)
			}
			if approver != nil {
				asked := len(approver.calls) == 1
				if asked != tt.wantAsked {
					t.Fatalf("approver asked = %v, want %v (calls=%d)", asked, tt.wantAsked, len(approver.calls))
				}
			}
			if c.LastLevel() != tt.wantLevel {
				t.Fatalf("LastLevel() = %v, want %v", c.LastLevel(), tt.wantLevel)
			}
			if c.LastReason() == "" {
				t.Fatal("LastReason() should not be empty after OnAction")
			}
		})
	}
}

func TestConfirmation_OnAction_ApproverError(t *testing.T) {
	wantErr := errors.New("boom")
	approver := &fakeApprover{answer: false, err: wantErr}
	c := NewConfirmation(nil, approver)

	ctx := context.Background()
	if err := c.OnRunStart(ctx, "delete the report"); err != nil {
		t.Fatalf("OnRunStart: unexpected error: %v", err)
	}

	a := action.Action{Type: action.Click}
	allow, err := c.OnAction(ctx, &a)
	if !errors.Is(err, wantErr) {
		t.Fatalf("OnAction err = %v, want %v", err, wantErr)
	}
	if allow {
		t.Fatal("OnAction should not allow when the approver errors")
	}
	if len(approver.calls) != 1 {
		t.Fatalf("expected the approver to be asked once, got %d calls", len(approver.calls))
	}
}

func TestConfirmation_OnAction_WithoutOnRunStart(t *testing.T) {
	// If OnRunStart was never called, task defaults to empty and the
	// classifier should fall back to NoConfirm for an ordinary click.
	approver := &fakeApprover{answer: false}
	c := NewConfirmation(nil, approver)

	a := action.Action{Type: action.Click}
	allow, err := c.OnAction(context.Background(), &a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allow {
		t.Fatal("expected an ordinary click with no task set to be allowed")
	}
	if len(approver.calls) != 0 {
		t.Fatalf("approver should not have been asked, got %d calls", len(approver.calls))
	}
}

// customClassifier lets tests exercise a non-default classifier, confirming
// Confirmation uses whatever ActionClassifier it was given rather than
// hardcoding policy.DefaultClassifier.
type customClassifier struct {
	level  policy.RiskLevel
	reason string
}

func (c customClassifier) Classify(context.Context, action.Action, string) (policy.RiskLevel, string) {
	return c.level, c.reason
}

func TestConfirmation_UsesInjectedClassifier(t *testing.T) {
	cc := customClassifier{level: policy.HandOff, reason: "custom hand off"}
	approver := &fakeApprover{answer: true}
	c := NewConfirmation(cc, approver)

	a := action.Action{Type: action.Click}
	allow, err := c.OnAction(context.Background(), &a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allow {
		t.Fatal("expected the custom classifier's HandOff verdict to deny")
	}
	if len(approver.calls) != 0 {
		t.Fatalf("approver should not have been asked for a HandOff verdict, got %d calls", len(approver.calls))
	}
	if c.LastReason() != "custom hand off" {
		t.Fatalf("LastReason() = %q, want %q", c.LastReason(), "custom hand off")
	}
}
