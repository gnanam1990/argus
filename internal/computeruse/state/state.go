// Package state defines the app-aware observation types the computer-use
// subsystem exposes: a list of apps, and a single app's window + accessibility
// element tree + screenshot. These are the neutral shapes the MCP tools and the
// capture worker exchange; nothing here talks to the OS.
package state

import (
	"context"
	"time"

	"github.com/gnanam1990/argus/pkg/action"
)

// AppInfo describes an app the agent may target.
type AppInfo struct {
	BundleIdentifier string    `json:"bundle_identifier"`
	Name             string    `json:"name"`
	IconPath         string    `json:"icon_path,omitempty"`
	IsRunning        bool      `json:"is_running"`
	LastUsedAt       time.Time `json:"last_used_at,omitempty"`
	UsageScore       float64   `json:"usage_score,omitempty"`
}

// Rect is a frame in screen points (accessibility space), floating-point
// because AX frames are not pixel-aligned.
type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Element is one node of an app's accessibility tree. Index is a stable
// depth-first position the model references when acting ("click element 12").
type Element struct {
	Index           int       `json:"index"`
	ID              string    `json:"id,omitempty"`
	Role            string    `json:"role"`
	Label           string    `json:"label,omitempty"`
	Value           string    `json:"value,omitempty"`
	Frame           Rect      `json:"frame"`
	Actions         []string  `json:"actions,omitempty"`
	ScrollDirection string    `json:"scroll_direction,omitempty"`
	Children        []Element `json:"children,omitempty"`
}

// AppState is a full observation of one app: its focused window, element tree,
// a screenshot, and any per-app instruction to inject into the prompt.
type AppState struct {
	BundleIdentifier string       `json:"bundle_identifier"`
	WindowTitle      string       `json:"window_title,omitempty"`
	WindowFrame      Rect         `json:"window_frame"`
	Elements         []Element    `json:"elements,omitempty"`
	Screenshot       action.Image `json:"-"`
	Instruction      string       `json:"instruction,omitempty"`
}

// Flatten returns the tree as a depth-first slice (children after their parent),
// which is how indices are assigned and resolved.
func (s AppState) Flatten() []Element {
	var out []Element
	var walk func(els []Element)
	walk = func(els []Element) {
		for _, e := range els {
			children := e.Children
			e.Children = nil
			out = append(out, e)
			walk(children)
		}
	}
	walk(s.Elements)
	return out
}

// FindByIndex returns the element with the given stable index, or false.
func (s AppState) FindByIndex(idx int) (Element, bool) {
	for _, e := range s.Flatten() {
		if e.Index == idx {
			return e, true
		}
	}
	return Element{}, false
}

// StateProvider produces app-aware observations.
type StateProvider interface {
	GetAppState(ctx context.Context, bundleID string) (AppState, error)
	ListApps(ctx context.Context) ([]AppInfo, error)
}
