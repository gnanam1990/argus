// Package proto is the computer-command vocabulary shared by the in-sandbox
// guest server and the RemoteComputer client: command names plus the param and
// result payloads for each. Keeping it separate lets both the server (guest)
// and the client (driver/remote) depend on one contract.
package proto

import "github.com/gnanam1990/argus/pkg/action"

// Command names.
const (
	CmdScreenshot     = "screenshot"
	CmdScreenSize     = "screen_size"
	CmdMove           = "move"
	CmdClick          = "click"
	CmdMouseDown      = "mouse_down"
	CmdMouseUp        = "mouse_up"
	CmdDrag           = "drag"
	CmdScroll         = "scroll"
	CmdType           = "type"
	CmdKey            = "key"
	CmdKeyDown        = "key_down"
	CmdKeyUp          = "key_up"
	CmdCursorPosition = "cursor_position"
	CmdClose          = "close"
)

// Commands lists every supported command (for GET /commands).
func Commands() []string {
	return []string{
		CmdScreenshot, CmdScreenSize, CmdMove, CmdClick, CmdMouseDown, CmdMouseUp,
		CmdDrag, CmdScroll, CmdType, CmdKey, CmdKeyDown, CmdKeyUp, CmdCursorPosition, CmdClose,
	}
}

// Point mirrors action.Point on the wire.
type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// PointParams targets a coordinate (move).
type PointParams struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// ClickParams parameterizes click/mouse_down/mouse_up.
type ClickParams struct {
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Button string `json:"button"`
	Clicks int    `json:"clicks"`
}

// DragParams parameterizes a drag.
type DragParams struct {
	Path   []Point `json:"path"`
	Button string  `json:"button"`
}

// ScrollParams parameterizes a scroll.
type ScrollParams struct {
	X  int `json:"x"`
	Y  int `json:"y"`
	DX int `json:"dx"`
	DY int `json:"dy"`
}

// TypeParams parameterizes typing.
type TypeParams struct {
	Text string `json:"text"`
}

// KeyParams parameterizes a key chord.
type KeyParams struct {
	Keys []string `json:"keys"`
}

// KeyOneParams parameterizes key_down/key_up.
type KeyOneParams struct {
	Key string `json:"key"`
}

// ScreenshotResult carries an encoded screenshot ([]byte is base64 in JSON).
type ScreenshotResult struct {
	MIME string `json:"mime"`
	Data []byte `json:"data"`
}

// ScreenSizeResult carries the screen dimensions.
type ScreenSizeResult struct {
	W int `json:"w"`
	H int `json:"h"`
}

// CursorResult carries the pointer position.
type CursorResult struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// OKResult is the reply for actions with no payload.
type OKResult struct {
	OK bool `json:"ok"`
}

// Button converts a wire button name to a canonical Button.
func Button(name string) action.Button {
	switch name {
	case "right":
		return action.Right
	case "middle":
		return action.Middle
	default:
		return action.Left
	}
}

// ButtonName converts a canonical Button to its wire name.
func ButtonName(b action.Button) string {
	switch b {
	case action.Right:
		return "right"
	case action.Middle:
		return "middle"
	default:
		return "left"
	}
}
