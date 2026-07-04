// Package remote implements computer.Computer over the transport protocol: it
// forwards each driver call to an in-sandbox guest server. Because it satisfies
// the same Computer interface as the local drivers, the agent loop is identical
// whether it drives the host or a remote sandbox.
package remote

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gnanam1990/argus/internal/guest/proto"
	"github.com/gnanam1990/argus/internal/transport"
	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/computer"
)

// Computer is a driver that forwards to a guest server over the transport.
type Computer struct {
	client *transport.Client
}

// Option configures a Computer.
type Option func(*[]transport.ClientOption)

// WithToken sets the bearer token.
func WithToken(token string) Option {
	return func(o *[]transport.ClientOption) { *o = append(*o, transport.WithToken(token)) }
}

// WithTraceID sets a correlation id.
func WithTraceID(id string) Option {
	return func(o *[]transport.ClientOption) { *o = append(*o, transport.WithTraceID(id)) }
}

// WithClientOptions passes raw transport client options.
func WithClientOptions(opts ...transport.ClientOption) Option {
	return func(o *[]transport.ClientOption) { *o = append(*o, opts...) }
}

// New builds a remote computer talking to the guest at baseURL.
func New(baseURL string, opts ...Option) *Computer {
	var copts []transport.ClientOption
	for _, o := range opts {
		o(&copts)
	}
	return &Computer{client: transport.NewClient(baseURL, copts...)}
}

var _ computer.Computer = (*Computer)(nil)

// send issues a command and decodes the result into out (may be nil).
func (c *Computer) send(ctx context.Context, cmd string, params, out any) error {
	resp, err := c.client.Send(ctx, cmd, params)
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("remote %s: %s", cmd, resp.Error)
	}
	if out != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, out)
	}
	return nil
}

// Screenshot captures the guest screen.
func (c *Computer) Screenshot(ctx context.Context) (action.Image, error) {
	var r proto.ScreenshotResult
	if err := c.send(ctx, proto.CmdScreenshot, nil, &r); err != nil {
		return action.Image{}, err
	}
	return action.Image{MIME: r.MIME, Data: r.Data}, nil
}

// ScreenSize returns the guest screen dimensions.
func (c *Computer) ScreenSize(ctx context.Context) (int, int, error) {
	var r proto.ScreenSizeResult
	if err := c.send(ctx, proto.CmdScreenSize, nil, &r); err != nil {
		return 0, 0, err
	}
	return r.W, r.H, nil
}

// MoveMouse moves the guest pointer.
func (c *Computer) MoveMouse(ctx context.Context, x, y int) error {
	return c.send(ctx, proto.CmdMove, proto.PointParams{X: x, Y: y}, nil)
}

// Click clicks in the guest.
func (c *Computer) Click(ctx context.Context, x, y int, b action.Button, clicks int) error {
	return c.send(ctx, proto.CmdClick, proto.ClickParams{X: x, Y: y, Button: proto.ButtonName(b), Clicks: clicks}, nil)
}

// MouseDown presses a button in the guest.
func (c *Computer) MouseDown(ctx context.Context, x, y int, b action.Button) error {
	return c.send(ctx, proto.CmdMouseDown, proto.ClickParams{X: x, Y: y, Button: proto.ButtonName(b)}, nil)
}

// MouseUp releases a button in the guest.
func (c *Computer) MouseUp(ctx context.Context, x, y int, b action.Button) error {
	return c.send(ctx, proto.CmdMouseUp, proto.ClickParams{X: x, Y: y, Button: proto.ButtonName(b)}, nil)
}

// Drag drags in the guest.
func (c *Computer) Drag(ctx context.Context, path []action.Point, b action.Button) error {
	pts := make([]proto.Point, len(path))
	for i, p := range path {
		pts[i] = proto.Point{X: p.X, Y: p.Y}
	}
	return c.send(ctx, proto.CmdDrag, proto.DragParams{Path: pts, Button: proto.ButtonName(b)}, nil)
}

// Scroll scrolls in the guest.
func (c *Computer) Scroll(ctx context.Context, x, y, dx, dy int) error {
	return c.send(ctx, proto.CmdScroll, proto.ScrollParams{X: x, Y: y, DX: dx, DY: dy}, nil)
}

// TypeText types in the guest.
func (c *Computer) TypeText(ctx context.Context, text string) error {
	return c.send(ctx, proto.CmdType, proto.TypeParams{Text: text}, nil)
}

// KeyPress presses a key chord in the guest.
func (c *Computer) KeyPress(ctx context.Context, keys ...string) error {
	return c.send(ctx, proto.CmdKey, proto.KeyParams{Keys: keys}, nil)
}

// KeyDown presses and holds a key in the guest.
func (c *Computer) KeyDown(ctx context.Context, key string) error {
	return c.send(ctx, proto.CmdKeyDown, proto.KeyOneParams{Key: key}, nil)
}

// KeyUp releases a key in the guest.
func (c *Computer) KeyUp(ctx context.Context, key string) error {
	return c.send(ctx, proto.CmdKeyUp, proto.KeyOneParams{Key: key}, nil)
}

// CursorPosition returns the guest pointer position.
func (c *Computer) CursorPosition(ctx context.Context) (int, int, error) {
	var r proto.CursorResult
	if err := c.send(ctx, proto.CmdCursorPosition, nil, &r); err != nil {
		return 0, 0, err
	}
	return r.X, r.Y, nil
}

// Close closes the guest computer.
func (c *Computer) Close() error {
	return c.send(context.Background(), proto.CmdClose, nil, nil)
}
