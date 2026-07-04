// Package action defines the provider- and driver-neutral domain model that
// every part of Argus agrees on: encoded screenshots, screen geometry, and the
// canonical Action union that all model adapters normalize into and all drivers
// execute. Nothing in this package imports a vendor SDK or a driver; it is the
// stable contract at the center of the system.
package action

import (
	"encoding/json"
	"fmt"
	"time"
)

// ActionType enumerates every canonical action the agent can request. Model
// adapters map each provider's raw vocabulary onto these; the executor maps
// these onto driver calls. The last group (RunCommand..WindowMove) is gated by
// the capability allowlist and is off by default.
type ActionType int

const (
	// Unknown is the zero value and is never a valid action.
	Unknown ActionType = iota

	Click
	DoubleClick
	TripleClick
	Move
	MouseDown
	MouseUp
	Drag
	Scroll

	Type
	Key
	KeyDown
	KeyUp

	Wait
	Screenshot
	CursorPosition
	Terminate

	// Gated capabilities (allowlist, off by default).
	RunCommand
	ReadFile
	WriteFile
	WindowFocus
	WindowMove
)

// actionTypeNames is the single source of truth for the wire name of each type.
// Both String and JSON (un)marshaling derive from it, so a name can never drift
// between logging and serialization.
var actionTypeNames = map[ActionType]string{
	Click:          "click",
	DoubleClick:    "double_click",
	TripleClick:    "triple_click",
	Move:           "move",
	MouseDown:      "mouse_down",
	MouseUp:        "mouse_up",
	Drag:           "drag",
	Scroll:         "scroll",
	Type:           "type",
	Key:            "key",
	KeyDown:        "key_down",
	KeyUp:          "key_up",
	Wait:           "wait",
	Screenshot:     "screenshot",
	CursorPosition: "cursor_position",
	Terminate:      "terminate",
	RunCommand:     "run_command",
	ReadFile:       "read_file",
	WriteFile:      "write_file",
	WindowFocus:    "window_focus",
	WindowMove:     "window_move",
}

var actionTypeByName = func() map[string]ActionType {
	m := make(map[string]ActionType, len(actionTypeNames))
	for t, name := range actionTypeNames {
		m[name] = t
	}
	return m
}()

// String returns the canonical wire name, or "unknown" for the zero/invalid
// value.
func (t ActionType) String() string {
	if name, ok := actionTypeNames[t]; ok {
		return name
	}
	return "unknown"
}

// Valid reports whether t is a known, non-zero action type.
func (t ActionType) Valid() bool {
	_, ok := actionTypeNames[t]
	return ok
}

// Gated reports whether t is a sensitive capability that must be explicitly
// allowlisted before the executor will run it.
func (t ActionType) Gated() bool {
	switch t {
	case RunCommand, ReadFile, WriteFile, WindowFocus, WindowMove:
		return true
	default:
		return false
	}
}

// MarshalJSON encodes the type as its canonical string name.
func (t ActionType) MarshalJSON() ([]byte, error) {
	name, ok := actionTypeNames[t]
	if !ok {
		return nil, fmt.Errorf("action: cannot marshal unknown ActionType(%d)", int(t))
	}
	return json.Marshal(name)
}

// UnmarshalJSON decodes a canonical string name into an ActionType.
func (t *ActionType) UnmarshalJSON(b []byte) error {
	var name string
	if err := json.Unmarshal(b, &name); err != nil {
		return err
	}
	typ, ok := actionTypeByName[name]
	if !ok {
		return fmt.Errorf("action: unknown action type %q", name)
	}
	*t = typ
	return nil
}

// NoMark is the sentinel Action.Mark value meaning "no set-of-marks index";
// coordinates come from Point instead.
const NoMark = -1

// Action is the canonical, provider-neutral description of a single thing to do
// on the computer. Adapters populate only the fields relevant to Type; Validate
// enforces the per-type invariants.
type Action struct {
	Type ActionType `json:"type"`

	// Point is the target in model/screenshot space. The executor resolves it
	// to screen space (and resolves Mark, when set, to a Rect center).
	Point Point `json:"point,omitempty"`
	// Mark is a set-of-marks index, or NoMark when unused.
	Mark int `json:"mark,omitempty"`

	Button Button `json:"button,omitempty"`
	Clicks int    `json:"clicks,omitempty"`

	// Text carries typed characters (Type) or a modifier chord for a click.
	Text string   `json:"text,omitempty"`
	Keys []string `json:"keys,omitempty"`

	// Path is the ordered waypoint list for a Drag.
	Path []Point `json:"path,omitempty"`

	// DX, DY are scroll deltas (positive DY scrolls down).
	DX int `json:"dx,omitempty"`
	DY int `json:"dy,omitempty"`

	// Dur is the sleep duration for Wait.
	Dur time.Duration `json:"dur,omitempty"`

	// Untrusted marks an action whose values derive from on-screen content and
	// therefore crossed the prompt-injection boundary; middleware inspects it.
	Untrusted bool `json:"untrusted,omitempty"`
}

// HasMark reports whether the action targets a set-of-marks index rather than a
// raw point.
func (a Action) HasMark() bool { return a.Mark > NoMark }

// Validate checks the per-type invariants and returns a descriptive error if
// the action is malformed. It does not mutate the action.
func (a Action) Validate() error {
	if !a.Type.Valid() {
		return fmt.Errorf("action: invalid type %q", a.Type)
	}
	if a.Mark < NoMark {
		return fmt.Errorf("action: mark must be >= %d, got %d", NoMark, a.Mark)
	}

	switch a.Type {
	case Click, DoubleClick, TripleClick, MouseDown, MouseUp, Move:
		if !a.Button.Valid() {
			return fmt.Errorf("action: %s has invalid button %d", a.Type, a.Button)
		}
		if a.Type == Click && a.Clicks < 0 {
			return fmt.Errorf("action: click has negative clicks %d", a.Clicks)
		}
	case Drag:
		if !a.Button.Valid() {
			return fmt.Errorf("action: drag has invalid button %d", a.Button)
		}
		if len(a.Path) < 2 {
			return fmt.Errorf("action: drag needs >= 2 path points, got %d", len(a.Path))
		}
	case Scroll:
		if a.DX == 0 && a.DY == 0 {
			return fmt.Errorf("action: scroll has zero delta")
		}
	case Type:
		if a.Text == "" {
			return fmt.Errorf("action: type has empty text")
		}
	case Key, KeyDown, KeyUp:
		if len(a.Keys) == 0 {
			return fmt.Errorf("action: %s has no keys", a.Type)
		}
		for i, k := range a.Keys {
			if k == "" {
				return fmt.Errorf("action: %s keys[%d] is empty", a.Type, i)
			}
		}
	case Wait:
		if a.Dur < 0 {
			return fmt.Errorf("action: wait has negative duration %s", a.Dur)
		}
	case RunCommand:
		if a.Text == "" {
			return fmt.Errorf("action: run_command has empty command")
		}
	case ReadFile, WriteFile:
		if a.Text == "" {
			return fmt.Errorf("action: %s has empty path", a.Type)
		}
	case Screenshot, CursorPosition, Terminate, WindowFocus, WindowMove:
		// No required fields beyond a valid type.
	}
	return nil
}

// Result is the outcome of executing an Action. Adapters attach the relevant
// field for the action they ran; the rest stay zero.
type Result struct {
	// Screenshot holds a captured frame (Screenshot, or a post-action capture).
	Screenshot Image `json:"screenshot,omitempty"`
	// Cursor holds the pointer position (CursorPosition).
	Cursor Point `json:"cursor,omitempty"`
	// Output holds textual output (RunCommand stdout, ReadFile text).
	Output string `json:"output,omitempty"`
	// Data holds raw bytes (ReadFile binary payload).
	Data []byte `json:"data,omitempty"`
	// Terminated is true when the action ended the run (Terminate).
	Terminated bool `json:"terminated,omitempty"`
}
