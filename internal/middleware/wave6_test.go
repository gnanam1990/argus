package middleware

import (
	"context"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
)

// An approval gate with no approver must fail closed (deny), never panic and
// never wave risky actions through.
func TestNewApprovalNilApproverDenies(t *testing.T) {
	t.Parallel()
	ap := NewApproval(nil, nil)

	a := action.Action{Type: action.RunCommand, Text: "rm -rf /", Untrusted: true}
	ok, err := ap.OnAction(context.Background(), &a)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("nil approver must deny risky actions (fail closed)")
	}

	// Safe actions still pass through without an approver.
	safe := action.Action{Type: action.Screenshot, Mark: action.NoMark}
	ok, err = ap.OnAction(context.Background(), &safe)
	if err != nil || !ok {
		t.Fatalf("safe action = %v, %v; want allowed", ok, err)
	}
}
