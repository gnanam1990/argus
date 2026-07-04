package guest_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gnanam1990/argus/internal/guest"
	"github.com/gnanam1990/argus/internal/guest/proto"
	"github.com/gnanam1990/argus/internal/transport"
	"github.com/gnanam1990/argus/pkg/action"
	compfake "github.com/gnanam1990/argus/pkg/computer/fake"
)

func handle(t *testing.T, s *guest.Server, cmd string, params any) transport.Response {
	t.Helper()
	req := transport.Request{ID: "1", Command: cmd}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		req.Params = b
	}
	return s.Handle(context.Background(), req)
}

func TestScreenshotCommand(t *testing.T) {
	t.Parallel()
	f := compfake.New().WithScreenshot(action.Image{MIME: action.MIMEPNG, Data: []byte{1, 2, 3}}, 800, 600)
	s := guest.New(f)

	resp := handle(t, s, proto.CmdScreenshot, nil)
	if !resp.OK {
		t.Fatalf("resp = %+v", resp)
	}
	var res proto.ScreenshotResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		t.Fatal(err)
	}
	if res.MIME != action.MIMEPNG || len(res.Data) != 3 {
		t.Errorf("screenshot result = %+v", res)
	}
}

func TestScreenSizeAndCursor(t *testing.T) {
	t.Parallel()
	f := compfake.New().WithScreenshot(action.Image{MIME: action.MIMEPNG, Data: []byte{1}}, 800, 600).WithCursor(11, 22)
	s := guest.New(f)

	resp := handle(t, s, proto.CmdScreenSize, nil)
	var sz proto.ScreenSizeResult
	_ = json.Unmarshal(resp.Result, &sz)
	if sz.W != 800 || sz.H != 600 {
		t.Errorf("size = %+v", sz)
	}

	resp = handle(t, s, proto.CmdCursorPosition, nil)
	var cur proto.CursorResult
	_ = json.Unmarshal(resp.Result, &cur)
	if cur.X != 11 || cur.Y != 22 {
		t.Errorf("cursor = %+v", cur)
	}
}

func TestInputCommands(t *testing.T) {
	t.Parallel()
	f := compfake.New()
	s := guest.New(f)

	tests := []struct {
		cmd    string
		params any
		method string
	}{
		{proto.CmdMove, proto.PointParams{X: 1, Y: 2}, "MoveMouse"},
		{proto.CmdClick, proto.ClickParams{X: 3, Y: 4, Button: "right", Clicks: 2}, "Click"},
		{proto.CmdMouseDown, proto.ClickParams{X: 1, Y: 1}, "MouseDown"},
		{proto.CmdMouseUp, proto.ClickParams{X: 1, Y: 1}, "MouseUp"},
		{proto.CmdDrag, proto.DragParams{Path: []proto.Point{{X: 0, Y: 0}, {X: 5, Y: 5}}}, "Drag"},
		{proto.CmdScroll, proto.ScrollParams{X: 1, Y: 1, DY: 2}, "Scroll"},
		{proto.CmdType, proto.TypeParams{Text: "hi"}, "TypeText"},
		{proto.CmdKey, proto.KeyParams{Keys: []string{"ctrl", "c"}}, "KeyPress"},
		{proto.CmdKeyDown, proto.KeyOneParams{Key: "shift"}, "KeyDown"},
		{proto.CmdKeyUp, proto.KeyOneParams{Key: "shift"}, "KeyUp"},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			resp := handle(t, s, tt.cmd, tt.params)
			if !resp.OK {
				t.Fatalf("%s failed: %+v", tt.cmd, resp)
			}
			last, _ := f.Last()
			if last.Method != tt.method {
				t.Errorf("%s → %s, want %s", tt.cmd, last.Method, tt.method)
			}
		})
	}

	// Click button + clicks were carried through.
	handle(t, s, proto.CmdClick, proto.ClickParams{X: 3, Y: 4, Button: "right", Clicks: 2})
	last, _ := f.Last()
	if last.Button != action.Right || last.Clicks != 2 {
		t.Errorf("click params = %+v", last)
	}
}

func TestUnknownAndClose(t *testing.T) {
	t.Parallel()
	f := compfake.New()
	s := guest.New(f)

	if resp := handle(t, s, "bogus", nil); resp.OK {
		t.Error("unknown command should fail")
	}
	if resp := handle(t, s, proto.CmdClose, nil); !resp.OK {
		t.Errorf("close failed: %+v", resp)
	}
	if !f.Closed() {
		t.Error("close command did not close the computer")
	}
}

func TestBadParamsError(t *testing.T) {
	t.Parallel()
	s := guest.New(compfake.New())
	// A string where an object is expected fails decode → error response.
	req := transport.Request{ID: "1", Command: proto.CmdClick, Params: json.RawMessage(`"not-an-object"`)}
	if resp := s.Handle(context.Background(), req); resp.OK {
		t.Error("malformed params should produce an error response")
	}
}

func TestEndToEndOverTransport(t *testing.T) {
	t.Parallel()
	f := compfake.New()
	srv := httptest.NewServer(transport.NewServer(guest.New(f)))
	t.Cleanup(srv.Close)

	c := transport.NewClient(srv.URL)
	resp, err := c.Send(context.Background(), proto.CmdClick, proto.ClickParams{X: 7, Y: 8})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("click failed: %+v", resp)
	}
	last, _ := f.Last()
	if last.Method != "Click" || last.X != 7 || last.Y != 8 {
		t.Errorf("guest did not execute the click over transport: %+v", last)
	}
}
