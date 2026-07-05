package actor

import (
	"context"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/computer/fake"
)

func TestClick(t *testing.T) {
	tests := []struct {
		name       string
		buttonStr  string
		wantButton action.Button
	}{
		{name: "left explicit", buttonStr: "left", wantButton: action.Left},
		{name: "right", buttonStr: "right", wantButton: action.Right},
		{name: "middle", buttonStr: "middle", wantButton: action.Middle},
		{name: "empty defaults left", buttonStr: "", wantButton: action.Left},
		{name: "unknown defaults left", buttonStr: "bogus", wantButton: action.Left},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := fake.New()
			a := New(fc)

			req := ClickRequest{
				BundleIdentifier: "com.example.app",
				ElementIndex:     7,
				X:                10,
				Y:                20,
				Button:           tt.buttonStr,
			}
			if err := a.Click(context.Background(), req); err != nil {
				t.Fatalf("Click: %v", err)
			}

			call, ok := fc.Last()
			if !ok {
				t.Fatal("no call recorded")
			}
			if call.Method != "Click" {
				t.Fatalf("method = %q, want Click", call.Method)
			}
			if call.X != 10 || call.Y != 20 {
				t.Fatalf("coords = (%d,%d), want (10,20)", call.X, call.Y)
			}
			if call.Button != tt.wantButton {
				t.Fatalf("button = %v, want %v", call.Button, tt.wantButton)
			}
			if call.Clicks != 1 {
				t.Fatalf("clicks = %d, want 1", call.Clicks)
			}
		})
	}
}

func TestTypeText(t *testing.T) {
	fc := fake.New()
	a := New(fc)

	req := TypeRequest{BundleIdentifier: "com.example.app", Text: "hello world"}
	if err := a.TypeText(context.Background(), req); err != nil {
		t.Fatalf("TypeText: %v", err)
	}

	call, ok := fc.Last()
	if !ok {
		t.Fatal("no call recorded")
	}
	if call.Method != "TypeText" {
		t.Fatalf("method = %q, want TypeText", call.Method)
	}
	if call.Text != "hello world" {
		t.Fatalf("text = %q, want %q", call.Text, "hello world")
	}
}

func TestPressKey(t *testing.T) {
	fc := fake.New()
	a := New(fc)

	req := KeyRequest{BundleIdentifier: "com.example.app", Keys: []string{"cmd", "shift", "4"}}
	if err := a.PressKey(context.Background(), req); err != nil {
		t.Fatalf("PressKey: %v", err)
	}

	call, ok := fc.Last()
	if !ok {
		t.Fatal("no call recorded")
	}
	if call.Method != "KeyPress" {
		t.Fatalf("method = %q, want KeyPress", call.Method)
	}
	if len(call.Keys) != 3 || call.Keys[0] != "cmd" || call.Keys[1] != "shift" || call.Keys[2] != "4" {
		t.Fatalf("keys = %v, want [cmd shift 4]", call.Keys)
	}
}

func TestScroll(t *testing.T) {
	tests := []struct {
		name      string
		direction string
		pages     int
		wantDX    int
		wantDY    int
		wantErr   bool
	}{
		{name: "down one page", direction: "down", pages: 1, wantDX: 0, wantDY: linesPerPage},
		{name: "up one page", direction: "up", pages: 1, wantDX: 0, wantDY: -linesPerPage},
		{name: "right one page", direction: "right", pages: 1, wantDX: linesPerPage, wantDY: 0},
		{name: "left one page", direction: "left", pages: 1, wantDX: -linesPerPage, wantDY: 0},
		{name: "down two pages", direction: "down", pages: 2, wantDX: 0, wantDY: 2 * linesPerPage},
		{name: "zero pages defaults to one", direction: "down", pages: 0, wantDX: 0, wantDY: linesPerPage},
		{name: "negative pages defaults to one", direction: "up", pages: -5, wantDX: 0, wantDY: -linesPerPage},
		{name: "unknown direction errors", direction: "sideways", pages: 1, wantErr: true},
		{name: "empty direction errors", direction: "", pages: 1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := fake.New()
			a := New(fc)

			req := ScrollRequest{
				BundleIdentifier: "com.example.app",
				ElementIndex:     3,
				Direction:        tt.direction,
				Pages:            tt.pages,
			}
			err := a.Scroll(context.Background(), req)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if len(fc.Calls()) != 0 {
					t.Fatalf("expected no calls recorded on error, got %v", fc.Calls())
				}
				return
			}
			if err != nil {
				t.Fatalf("Scroll: %v", err)
			}

			call, ok := fc.Last()
			if !ok {
				t.Fatal("no call recorded")
			}
			if call.Method != "Scroll" {
				t.Fatalf("method = %q, want Scroll", call.Method)
			}
			if call.DX != tt.wantDX || call.DY != tt.wantDY {
				t.Fatalf("delta = (%d,%d), want (%d,%d)", call.DX, call.DY, tt.wantDX, tt.wantDY)
			}
		})
	}
}

func TestScrollCoordinates(t *testing.T) {
	fc := fake.New()
	a := New(fc)

	// The gesture must be issued at the request's resolved point (X,Y), not the
	// origin — otherwise the driver warps the cursor to the display corner and
	// scrolls the wrong pane.
	req := ScrollRequest{Direction: "down", Pages: 1, X: 640, Y: 480}
	if err := a.Scroll(context.Background(), req); err != nil {
		t.Fatalf("Scroll: %v", err)
	}
	call, _ := fc.Last()
	if call.X != 640 || call.Y != 480 {
		t.Fatalf("coords = (%d,%d), want (640,480) from the request", call.X, call.Y)
	}
}

func TestDrag(t *testing.T) {
	fc := fake.New()
	a := New(fc)

	req := DragRequest{
		BundleIdentifier: "com.example.app",
		FromX:            1, FromY: 2,
		ToX: 3, ToY: 4,
	}
	if err := a.Drag(context.Background(), req); err != nil {
		t.Fatalf("Drag: %v", err)
	}

	call, ok := fc.Last()
	if !ok {
		t.Fatal("no call recorded")
	}
	if call.Method != "Drag" {
		t.Fatalf("method = %q, want Drag", call.Method)
	}
	wantPath := []action.Point{{X: 1, Y: 2}, {X: 3, Y: 4}}
	if len(call.Path) != 2 || call.Path[0] != wantPath[0] || call.Path[1] != wantPath[1] {
		t.Fatalf("path = %v, want %v", call.Path, wantPath)
	}
	if call.Button != action.Left {
		t.Fatalf("button = %v, want Left", call.Button)
	}
}

func TestPerformSecondaryAction(t *testing.T) {
	fc := fake.New()
	a := New(fc)

	req := SecondaryActionRequest{
		BundleIdentifier: "com.example.app",
		ElementIndex:     5,
		X:                50, Y: 60,
	}
	if err := a.PerformSecondaryAction(context.Background(), req); err != nil {
		t.Fatalf("PerformSecondaryAction: %v", err)
	}

	call, ok := fc.Last()
	if !ok {
		t.Fatal("no call recorded")
	}
	if call.Method != "Click" {
		t.Fatalf("method = %q, want Click", call.Method)
	}
	if call.X != 50 || call.Y != 60 {
		t.Fatalf("coords = (%d,%d), want (50,60)", call.X, call.Y)
	}
	if call.Button != action.Right {
		t.Fatalf("button = %v, want Right", call.Button)
	}
	if call.Clicks != 1 {
		t.Fatalf("clicks = %d, want 1", call.Clicks)
	}
}

func TestErrorPropagation(t *testing.T) {
	wantErr := "boom"
	fc := fake.New().WithError(errString(wantErr))
	a := New(fc)

	ctx := context.Background()
	if err := a.Click(ctx, ClickRequest{}); err == nil {
		t.Error("Click: expected error")
	}
	if err := a.TypeText(ctx, TypeRequest{}); err == nil {
		t.Error("TypeText: expected error")
	}
	if err := a.PressKey(ctx, KeyRequest{Keys: []string{"a"}}); err == nil {
		t.Error("PressKey: expected error")
	}
	if err := a.Scroll(ctx, ScrollRequest{Direction: "down"}); err == nil {
		t.Error("Scroll: expected error")
	}
	if err := a.Drag(ctx, DragRequest{}); err == nil {
		t.Error("Drag: expected error")
	}
	if err := a.PerformSecondaryAction(ctx, SecondaryActionRequest{}); err == nil {
		t.Error("PerformSecondaryAction: expected error")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
