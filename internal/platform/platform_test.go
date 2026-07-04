package platform

import (
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDisplayServer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"XDG_SESSION_TYPE": "wayland"}, "wayland"},
		{map[string]string{"XDG_SESSION_TYPE": "x11"}, "x11"},
		{map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, "wayland"},
		{map[string]string{"DISPLAY": ":0"}, "x11"},
		{map[string]string{}, "unknown"},
	}
	for _, c := range cases {
		if got := displayServerFrom(env(c.env)); got != c.want {
			t.Errorf("displayServerFrom(%v) = %q, want %q", c.env, got, c.want)
		}
	}
}

func TestPreflight(t *testing.T) {
	t.Parallel()
	if err := Preflight(env(map[string]string{"DISPLAY": ":0"})); err != nil {
		t.Errorf("x11 preflight should pass: %v", err)
	}
	if err := Preflight(env(map[string]string{"XDG_SESSION_TYPE": "wayland"})); err == nil {
		t.Error("wayland preflight should fail")
	}
	if err := Preflight(env(map[string]string{})); err == nil {
		t.Error("no-display preflight should fail")
	}
}

func TestParseMonitors(t *testing.T) {
	t.Parallel()
	fixture := "Monitors: 2\n" +
		" 0: +*HDMI-1 1920/510x1080/290+0+0  HDMI-1\n" +
		" 1: +DP-1 1920/510x1080/290+1920+0  DP-1\n"
	got := ParseMonitors(fixture)
	if len(got) != 2 {
		t.Fatalf("parsed %d monitors, want 2: %+v", len(got), got)
	}
	if got[0].Name != "HDMI-1" || got[0].Bounds != (action.Rect{Max: action.Point{X: 1920, Y: 1080}}) {
		t.Errorf("monitor 0 = %+v", got[0])
	}
	// Second monitor is offset by +1920 in X.
	if got[1].Bounds.Min.X != 1920 || got[1].Bounds.Max.X != 3840 {
		t.Errorf("monitor 1 bounds = %+v", got[1].Bounds)
	}
}

func TestDisplayAt(t *testing.T) {
	t.Parallel()
	displays := []Display{
		{Name: "A", Bounds: action.Rect{Max: action.Point{X: 1920, Y: 1080}}},
		{Name: "B", Bounds: action.Rect{Min: action.Point{X: 1920}, Max: action.Point{X: 3840, Y: 1080}}},
	}
	if d := DisplayAt(displays, action.Point{X: 100, Y: 100}); d == nil || d.Name != "A" {
		t.Errorf("point on A = %+v", d)
	}
	if d := DisplayAt(displays, action.Point{X: 2000, Y: 100}); d == nil || d.Name != "B" {
		t.Errorf("point on B = %+v", d)
	}
	if d := DisplayAt(displays, action.Point{X: 5000, Y: 5000}); d != nil {
		t.Errorf("off-screen point should be nil, got %+v", d)
	}
}
