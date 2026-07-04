package action

import (
	"encoding/json"
	"testing"
	"time"
)

func TestActionTypeString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		typ  ActionType
		want string
	}{
		{Click, "click"},
		{DoubleClick, "double_click"},
		{CursorPosition, "cursor_position"},
		{RunCommand, "run_command"},
		{Unknown, "unknown"},
		{ActionType(9999), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.typ.String(); got != tt.want {
			t.Errorf("ActionType(%d).String() = %q, want %q", tt.typ, got, tt.want)
		}
	}
}

func TestActionTypeValidAndGated(t *testing.T) {
	t.Parallel()
	if Unknown.Valid() {
		t.Error("Unknown must not be Valid")
	}
	if !Click.Valid() {
		t.Error("Click must be Valid")
	}
	gated := []ActionType{RunCommand, ReadFile, WriteFile, WindowFocus, WindowMove}
	for _, g := range gated {
		if !g.Gated() {
			t.Errorf("%s must be Gated", g)
		}
	}
	notGated := []ActionType{Click, Type, Screenshot, Scroll, Terminate}
	for _, ng := range notGated {
		if ng.Gated() {
			t.Errorf("%s must not be Gated", ng)
		}
	}
}

func TestActionTypeJSONRoundTrip(t *testing.T) {
	t.Parallel()
	// Every known type must survive a marshal/unmarshal cycle by name.
	for typ := range actionTypeNames {
		b, err := json.Marshal(typ)
		if err != nil {
			t.Fatalf("marshal %s: %v", typ, err)
		}
		var got ActionType
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", b, err)
		}
		if got != typ {
			t.Errorf("round-trip %s: got %s", typ, got)
		}
	}
}

func TestActionTypeMarshalUnknownFails(t *testing.T) {
	t.Parallel()
	if _, err := json.Marshal(Unknown); err == nil {
		t.Error("marshaling Unknown should fail")
	}
	if _, err := json.Marshal(ActionType(1234)); err == nil {
		t.Error("marshaling an out-of-range type should fail")
	}
}

func TestActionTypeUnmarshalUnknownFails(t *testing.T) {
	t.Parallel()
	var typ ActionType
	if err := json.Unmarshal([]byte(`"not_a_real_action"`), &typ); err == nil {
		t.Error("unmarshaling an unknown name should fail")
	}
}

func TestActionJSONRoundTrip(t *testing.T) {
	t.Parallel()
	orig := Action{
		Type:      Drag,
		Button:    Right,
		Path:      []Point{{0, 0}, {10, 10}, {20, 5}},
		Mark:      NoMark,
		Dur:       250 * time.Millisecond,
		Untrusted: true,
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Action
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != orig.Type || got.Button != orig.Button ||
		got.Dur != orig.Dur || got.Untrusted != orig.Untrusted ||
		len(got.Path) != len(orig.Path) {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, orig)
	}
	for i := range orig.Path {
		if got.Path[i] != orig.Path[i] {
			t.Errorf("path[%d] = %v, want %v", i, got.Path[i], orig.Path[i])
		}
	}
}

func TestActionHasMark(t *testing.T) {
	t.Parallel()
	if (Action{Mark: NoMark}).HasMark() {
		t.Error("NoMark must report HasMark=false")
	}
	if !(Action{Mark: 0}).HasMark() {
		t.Error("mark 0 must report HasMark=true")
	}
	if !(Action{Mark: 7}).HasMark() {
		t.Error("mark 7 must report HasMark=true")
	}
}

func TestActionValidate_Valid(t *testing.T) {
	t.Parallel()
	valid := []struct {
		name string
		a    Action
	}{
		{"click", Action{Type: Click, Button: Left, Clicks: 1}},
		{"double_click", Action{Type: DoubleClick, Button: Left}},
		{"move", Action{Type: Move, Button: Left, Point: Point{5, 5}}},
		{"drag", Action{Type: Drag, Button: Left, Path: []Point{{0, 0}, {1, 1}}}},
		{"scroll", Action{Type: Scroll, DY: -3}},
		{"type", Action{Type: Type, Text: "hello"}},
		{"key", Action{Type: Key, Keys: []string{"ctrl", "c"}}},
		{"wait", Action{Type: Wait, Dur: time.Second}},
		{"screenshot", Action{Type: Screenshot}},
		{"cursor_position", Action{Type: CursorPosition}},
		{"terminate", Action{Type: Terminate}},
		{"run_command", Action{Type: RunCommand, Text: "ls"}},
		{"read_file", Action{Type: ReadFile, Text: "/etc/hosts"}},
	}
	for _, tt := range valid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.a.Validate(); err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestActionValidate_Invalid(t *testing.T) {
	t.Parallel()
	invalid := []struct {
		name string
		a    Action
	}{
		{"unknown type", Action{Type: Unknown}},
		{"out of range type", Action{Type: ActionType(9999)}},
		{"mark below sentinel", Action{Type: Screenshot, Mark: -2}},
		{"click bad button", Action{Type: Click, Button: Button(42)}},
		{"click negative clicks", Action{Type: Click, Button: Left, Clicks: -1}},
		{"drag too few points", Action{Type: Drag, Button: Left, Path: []Point{{0, 0}}}},
		{"drag bad button", Action{Type: Drag, Button: Button(9), Path: []Point{{0, 0}, {1, 1}}}},
		{"scroll zero delta", Action{Type: Scroll}},
		{"type empty text", Action{Type: Type}},
		{"key no keys", Action{Type: Key}},
		{"key empty element", Action{Type: Key, Keys: []string{"ctrl", ""}}},
		{"wait negative", Action{Type: Wait, Dur: -time.Second}},
		{"run_command empty", Action{Type: RunCommand}},
		{"write_file empty path", Action{Type: WriteFile}},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.a.Validate(); err == nil {
				t.Errorf("Validate() = nil, want error for %+v", tt.a)
			}
		})
	}
}
