package ax

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
)

func TestParseTreeSuccess(t *testing.T) {
	t.Parallel()
	payload := wirePayload{
		Screen: &wireScreen{W: 1440, H: 900},
		Elements: []wireElement{
			{Role: "AXButton", Title: "Save", X: 10, Y: 20, W: 80, H: 24, Enabled: true},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	screen, els, err := parseTree(raw)
	if err != nil {
		t.Fatalf("parseTree: %v", err)
	}
	if screen.W != 1440 || screen.H != 900 {
		t.Errorf("screen = %+v, want 1440x900", screen)
	}
	if len(els) != 1 || els[0].Role != "AXButton" || els[0].Title != "Save" {
		t.Errorf("elements = %+v", els)
	}
}

func TestParseTreeMalformedJSON(t *testing.T) {
	t.Parallel()
	// Truncated mid-object: syntactically invalid, must error rather than
	// silently yield a partial/empty result.
	_, _, err := parseTree([]byte(`{"screen":{"w":1440,"h":900},"elements":[{"role":`))
	if err == nil {
		t.Fatal("expected an error for truncated/malformed JSON, got nil")
	}
}

func TestParseTreeGarbageOutput(t *testing.T) {
	t.Parallel()
	_, _, err := parseTree([]byte("not json at all"))
	if err == nil {
		t.Fatal("expected an error for non-JSON output, got nil")
	}
}

func TestParseTreeMissingScreenRecord(t *testing.T) {
	t.Parallel()
	_, _, err := parseTree([]byte(`{"elements":[]}`))
	if err == nil {
		t.Fatal("expected an error for a payload missing the screen record, got nil")
	}
}

// TestMapElementsRoleAndEnabledFiltering covers the interactableRoles set
// (including the AXTabGroup-vs-AXTab / AXRadioButton hedge from the design
// notes: the tab strip container itself is not a click target, but its
// individual tab items are) and the enabled gate: an interactable role that
// isn't enabled must not be marked Interactable.
func TestMapElementsRoleAndEnabledFiltering(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		role    string
		enabled bool
		want    bool
	}{
		{"button enabled", "AXButton", true, true},
		{"button disabled", "AXButton", false, false},
		{"text field enabled", "AXTextField", true, true},
		{"text area enabled", "AXTextArea", true, true},
		{"checkbox enabled", "AXCheckBox", true, true},
		{"radio button enabled", "AXRadioButton", true, true},
		{"popup button enabled", "AXPopUpButton", true, true},
		{"combo box enabled", "AXComboBox", true, true},
		{"link enabled", "AXLink", true, true},
		{"menu item enabled", "AXMenuItem", true, true},
		{"menu button enabled", "AXMenuButton", true, true},
		{"slider enabled", "AXSlider", true, true},
		{"incrementor enabled", "AXIncrementor", true, true},
		{"disclosure triangle enabled", "AXDisclosureTriangle", true, true},
		{"individual tab included", "AXTab", true, true},
		{"tab group container excluded", "AXTabGroup", true, false},
		{"static text is not interactable", "AXStaticText", true, false},
		{"group is not interactable", "AXGroup", true, false},
		{"window is not interactable", "AXWindow", true, false},
		{"link disabled", "AXLink", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			els := []wireElement{{Role: tt.role, Title: "x", X: 0, Y: 0, W: 10, H: 10, Enabled: tt.enabled}}
			got := mapElements(els, 1, 1)
			if len(got) != 1 {
				t.Fatalf("got %d elements, want 1", len(got))
			}
			if got[0].Interactable != tt.want {
				t.Errorf("role=%s enabled=%v: Interactable = %v, want %v",
					tt.role, tt.enabled, got[0].Interactable, tt.want)
			}
		})
	}
}

// TestMapElementsScaling is the point->pixel scaling case from the design
// notes: a 1440x900-point screen with a 2880x1800-pixel screenshot scales
// every box by exactly 2x on both axes.
func TestMapElementsScaling(t *testing.T) {
	t.Parallel()
	sx, sy := 2880.0/1440.0, 1800.0/900.0
	els := []wireElement{{Role: "AXButton", Title: "Save", X: 10, Y: 20, W: 80, H: 24, Enabled: true}}
	got := mapElements(els, sx, sy)
	if len(got) != 1 {
		t.Fatalf("got %d elements, want 1", len(got))
	}
	box := got[0].Box
	wantMin, wantMax := action.Point{X: 20, Y: 40}, action.Point{X: 180, Y: 88}
	if box.Min != wantMin || box.Max != wantMax {
		t.Errorf("box = %+v, want min=%+v max=%+v (point box doubled into pixel space)", box, wantMin, wantMax)
	}
}

func TestMapElementsIdentityScale(t *testing.T) {
	t.Parallel()
	els := []wireElement{{Role: "AXButton", Title: "Save", X: 10, Y: 20, W: 80, H: 24, Enabled: true}}
	got := mapElements(els, 1, 1)
	box := got[0].Box
	if box.Min != (action.Point{X: 10, Y: 20}) || box.Max != (action.Point{X: 90, Y: 44}) {
		t.Errorf("box = %+v, want an unscaled 1:1 box", box)
	}
}

func TestMapElementsDropsZeroAreaBoxes(t *testing.T) {
	t.Parallel()
	els := []wireElement{
		{Role: "AXButton", Title: "ghost", X: 0, Y: 0, W: 0, H: 0, Enabled: true},
		{Role: "AXButton", Title: "real", X: 0, Y: 0, W: 10, H: 10, Enabled: true},
	}
	got := mapElements(els, 1, 1)
	if len(got) != 1 || got[0].Label != "real" {
		t.Errorf("got %+v, want only the non-zero-area element", got)
	}
}

func TestMapElementsLabelFallbackAndSequentialIDs(t *testing.T) {
	t.Parallel()
	els := []wireElement{
		{Role: "AXButton", Title: "Save", Value: "ignored", X: 0, Y: 0, W: 10, H: 10, Enabled: true},
		{Role: "AXTextField", Title: "", Value: "current text", X: 20, Y: 0, W: 10, H: 10, Enabled: true},
	}
	got := mapElements(els, 1, 1)
	if len(got) != 2 {
		t.Fatalf("got %d elements, want 2", len(got))
	}
	if got[0].ID != 0 || got[1].ID != 1 {
		t.Errorf("IDs = %d, %d, want sequential 0, 1", got[0].ID, got[1].ID)
	}
	if got[0].Label != "Save" {
		t.Errorf("Label = %q, want title \"Save\"", got[0].Label)
	}
	if got[1].Label != "current text" {
		t.Errorf("Label = %q, want value fallback \"current text\"", got[1].Label)
	}
	if got[0].Confidence != 1.0 || got[1].Confidence != 1.0 {
		t.Errorf("Confidence = %v, %v, want 1.0 (the tree is exact)", got[0].Confidence, got[1].Confidence)
	}
	if got[1].Text != "current text" {
		t.Errorf("Text = %q, want the raw value \"current text\"", got[1].Text)
	}
}

func TestIsAssistiveDenied(t *testing.T) {
	t.Parallel()
	tests := []struct {
		msg  string
		want bool
	}{
		{"osascript is not allowed assistive access.", true},
		{"OSAScript IS NOT ALLOWED ASSISTIVE ACCESS", true},
		{"An error of type -25211 has occurred.", true},
		{"System Events got an error: not allowed. (1002)", true},
		{"exit status 1", false},
		{`exec: "osascript": executable file not found in $PATH`, false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isAssistiveDenied(tt.msg); got != tt.want {
			t.Errorf("isAssistiveDenied(%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}

func TestRunErrorAssistiveDenied(t *testing.T) {
	t.Parallel()
	err := runError(errors.New("An error of type -25211 has occurred."))
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("runError should wrap ErrUnavailable, got %v", err)
	}
	if !strings.Contains(err.Error(), "System Settings") || !strings.Contains(err.Error(), "Accessibility") {
		t.Errorf("runError message should mention System Settings > Accessibility, got %q", err.Error())
	}
}

func TestRunErrorGenericWrapsErrUnavailable(t *testing.T) {
	t.Parallel()
	err := runError(errors.New("boom"))
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("runError should wrap ErrUnavailable, got %v", err)
	}
}

func TestDecodeSizeEdgeCases(t *testing.T) {
	t.Parallel()
	if _, _, ok := decodeSize(action.Image{}); ok {
		t.Error("decodeSize of an empty image should not be ok")
	}
	if _, _, ok := decodeSize(action.Image{MIME: action.MIMEPNG, Data: []byte("not a png")}); ok {
		t.Error("decodeSize of undecodable data should not be ok")
	}
}

func TestDecodeSizeDecodable(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 7, 5))); err != nil {
		t.Fatal(err)
	}
	w, h, ok := decodeSize(action.Image{MIME: action.MIMEPNG, Data: buf.Bytes()})
	if !ok || w != 7 || h != 5 {
		t.Errorf("decodeSize = (%d,%d,%v), want (7,5,true)", w, h, ok)
	}
}
