package ax_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gnanam1990/argus/internal/grounder/ax"
)

// scriptOf returns the JXA program a fakeRunner (see darwin_test.go) was asked
// to run — the last argv element.
func scriptOf(fr *fakeRunner) string {
	if len(fr.argv) == 0 {
		return ""
	}
	return fr.argv[len(fr.argv)-1]
}

func TestClickerOK(t *testing.T) {
	t.Parallel()
	fr := &fakeRunner{out: []byte("ok\n")}
	c := ax.NewClicker(ax.WithRunner(fr))
	if err := c.Click(context.Background(), 820, 540); err != nil {
		t.Fatalf("Click: %v", err)
	}
	// The script hit-tests the exact point and performs AXPress.
	s := scriptOf(fr)
	if !strings.Contains(s, "820") || !strings.Contains(s, "540") {
		t.Errorf("script missing coordinates: %s", s)
	}
	if !strings.Contains(s, "AXPress") || !strings.Contains(s, "AXUIElementCopyElementAtPosition") {
		t.Errorf("script missing AX hit-test/press: %s", s)
	}
}

func TestClickerNoTarget(t *testing.T) {
	t.Parallel()
	fr := &fakeRunner{out: []byte("notarget\n")}
	c := ax.NewClicker(ax.WithRunner(fr))
	err := c.Click(context.Background(), 1, 2)
	if !ax.ErrNoTarget(err) {
		t.Fatalf("err = %v, want no-target signal", err)
	}
}

func TestClickerAssistiveDenied(t *testing.T) {
	t.Parallel()
	fr := &fakeRunner{err: errors.New("osascript: assistive access is not enabled (-25211)")}
	c := ax.NewClicker(ax.WithRunner(fr))
	err := c.Click(context.Background(), 1, 2)
	if err == nil || ax.ErrNoTarget(err) {
		t.Fatalf("permission denial must be a real error, got %v", err)
	}
	if !strings.Contains(err.Error(), "Accessibility") {
		t.Errorf("error should name the Accessibility permission: %v", err)
	}
}

func TestClickerUnexpectedOutput(t *testing.T) {
	t.Parallel()
	fr := &fakeRunner{out: []byte("weird")}
	c := ax.NewClicker(ax.WithRunner(fr))
	if err := c.Click(context.Background(), 1, 2); err == nil {
		t.Fatal("unexpected output should error")
	}
}
