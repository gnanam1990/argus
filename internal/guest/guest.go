// Package guest is the in-sandbox computer server: it dispatches transport
// commands to a computer.Computer, so the same Computer that runs on the host
// can be driven remotely by the agent over the transport. cmd/guestd wraps it.
package guest

import (
	"context"
	"encoding/json"

	"github.com/gnanam1990/argus/internal/guest/proto"
	"github.com/gnanam1990/argus/internal/transport"
	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/computer"
)

// Server dispatches commands to a Computer. It implements transport.Handler.
type Server struct {
	c computer.Computer
}

// New builds a guest server over c.
func New(c computer.Computer) *Server { return &Server{c: c} }

var _ transport.Handler = (*Server)(nil)

// Handle executes one command and returns its reply.
func (s *Server) Handle(ctx context.Context, req transport.Request) transport.Response {
	id := req.ID
	switch req.Command {
	case proto.CmdScreenshot:
		img, err := s.c.Screenshot(ctx)
		if err != nil {
			return transport.Errorf(id, "screenshot: %v", err)
		}
		return transport.Result(id, proto.ScreenshotResult{MIME: img.MIME, Data: img.Data})

	case proto.CmdScreenSize:
		w, h, err := s.c.ScreenSize(ctx)
		if err != nil {
			return transport.Errorf(id, "screen_size: %v", err)
		}
		return transport.Result(id, proto.ScreenSizeResult{W: w, H: h})

	case proto.CmdCursorPosition:
		x, y, err := s.c.CursorPosition(ctx)
		if err != nil {
			return transport.Errorf(id, "cursor_position: %v", err)
		}
		return transport.Result(id, proto.CursorResult{X: x, Y: y})

	case proto.CmdMove:
		var p proto.PointParams
		if err := decode(req.Params, &p); err != nil {
			return transport.Errorf(id, "move: %v", err)
		}
		return okOrErr(id, s.c.MoveMouse(ctx, p.X, p.Y))

	case proto.CmdClick:
		var p proto.ClickParams
		if err := decode(req.Params, &p); err != nil {
			return transport.Errorf(id, "click: %v", err)
		}
		clicks := p.Clicks
		if clicks <= 0 {
			clicks = 1
		}
		return okOrErr(id, s.c.Click(ctx, p.X, p.Y, proto.Button(p.Button), clicks))

	case proto.CmdMouseDown:
		var p proto.ClickParams
		if err := decode(req.Params, &p); err != nil {
			return transport.Errorf(id, "mouse_down: %v", err)
		}
		return okOrErr(id, s.c.MouseDown(ctx, p.X, p.Y, proto.Button(p.Button)))

	case proto.CmdMouseUp:
		var p proto.ClickParams
		if err := decode(req.Params, &p); err != nil {
			return transport.Errorf(id, "mouse_up: %v", err)
		}
		return okOrErr(id, s.c.MouseUp(ctx, p.X, p.Y, proto.Button(p.Button)))

	case proto.CmdDrag:
		var p proto.DragParams
		if err := decode(req.Params, &p); err != nil {
			return transport.Errorf(id, "drag: %v", err)
		}
		return okOrErr(id, s.c.Drag(ctx, toPoints(p.Path), proto.Button(p.Button)))

	case proto.CmdScroll:
		var p proto.ScrollParams
		if err := decode(req.Params, &p); err != nil {
			return transport.Errorf(id, "scroll: %v", err)
		}
		return okOrErr(id, s.c.Scroll(ctx, p.X, p.Y, p.DX, p.DY))

	case proto.CmdType:
		var p proto.TypeParams
		if err := decode(req.Params, &p); err != nil {
			return transport.Errorf(id, "type: %v", err)
		}
		return okOrErr(id, s.c.TypeText(ctx, p.Text))

	case proto.CmdKey:
		var p proto.KeyParams
		if err := decode(req.Params, &p); err != nil {
			return transport.Errorf(id, "key: %v", err)
		}
		return okOrErr(id, s.c.KeyPress(ctx, p.Keys...))

	case proto.CmdKeyDown:
		var p proto.KeyOneParams
		if err := decode(req.Params, &p); err != nil {
			return transport.Errorf(id, "key_down: %v", err)
		}
		return okOrErr(id, s.c.KeyDown(ctx, p.Key))

	case proto.CmdKeyUp:
		var p proto.KeyOneParams
		if err := decode(req.Params, &p); err != nil {
			return transport.Errorf(id, "key_up: %v", err)
		}
		return okOrErr(id, s.c.KeyUp(ctx, p.Key))

	case proto.CmdClose:
		return okOrErr(id, s.c.Close())

	default:
		return transport.Errorf(id, "unknown command %q", req.Command)
	}
}

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}

func okOrErr(id string, err error) transport.Response {
	if err != nil {
		return transport.Errorf(id, "%v", err)
	}
	return transport.Result(id, proto.OKResult{OK: true})
}

func toPoints(ps []proto.Point) []action.Point {
	out := make([]action.Point, len(ps))
	for i, p := range ps {
		out[i] = action.Point{X: p.X, Y: p.Y}
	}
	return out
}
