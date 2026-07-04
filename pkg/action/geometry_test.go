package action

import "testing"

func TestPointScale(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		p      Point
		sx, sy float64
		want   Point
	}{
		{"identity", Point{10, 20}, 1, 1, Point{10, 20}},
		{"retina 2x uniform", Point{50, 75}, 2, 2, Point{100, 150}},
		{"downscale half", Point{100, 200}, 0.5, 0.5, Point{50, 100}},
		{"asymmetric axes", Point{10, 10}, 2, 3, Point{20, 30}},
		{"rounds to nearest", Point{3, 3}, 1.5, 1.5, Point{5, 5}}, // 4.5 -> 5 (round half away from zero)
		{"rounds down", Point{3, 3}, 1.4, 1.4, Point{4, 4}},       // 4.2 -> 4
		{"zero origin invariant", Point{0, 0}, 2, 2, Point{0, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.p.Scale(tt.sx, tt.sy); got != tt.want {
				t.Errorf("%v.Scale(%v,%v) = %v, want %v", tt.p, tt.sx, tt.sy, got, tt.want)
			}
		})
	}
}

func TestPointScaleRoundTripHiDPI(t *testing.T) {
	t.Parallel()
	// A screenshot-space point scaled to logical space and back must be stable
	// for integer-friendly factors.
	p := Point{200, 300}
	got := p.Scale(0.5, 0.5).Scale(2, 2)
	if got != p {
		t.Errorf("round-trip 0.5 then 2 = %v, want %v", got, p)
	}
}

func TestPointAdd(t *testing.T) {
	t.Parallel()
	if got := (Point{5, 5}).Add(Point{-2, 3}); got != (Point{3, 8}) {
		t.Errorf("Add = %v, want {3 8}", got)
	}
}

func TestRectCenter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		r    Rect
		want Point
	}{
		{"even box", Rect{Point{0, 0}, Point{10, 20}}, Point{5, 10}},
		{"odd box truncates", Rect{Point{0, 0}, Point{5, 5}}, Point{2, 2}},
		{"offset box", Rect{Point{10, 10}, Point{20, 30}}, Point{15, 20}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.r.Center(); got != tt.want {
				t.Errorf("%v.Center() = %v, want %v", tt.r, got, tt.want)
			}
		})
	}
}

func TestRectContains(t *testing.T) {
	t.Parallel()
	r := Rect{Point{0, 0}, Point{10, 10}}
	tests := []struct {
		p    Point
		want bool
	}{
		{Point{5, 5}, true},
		{Point{0, 0}, true},   // inclusive min corner
		{Point{10, 10}, true}, // inclusive max corner
		{Point{10, 0}, true},
		{Point{11, 5}, false},
		{Point{-1, 5}, false},
		{Point{5, 11}, false},
	}
	for _, tt := range tests {
		if got := r.Contains(tt.p); got != tt.want {
			t.Errorf("%v.Contains(%v) = %v, want %v", r, tt.p, got, tt.want)
		}
	}
}

func TestRectWidthHeightEmpty(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		r     Rect
		w, h  int
		empty bool
	}{
		{"normal", Rect{Point{0, 0}, Point{10, 20}}, 10, 20, false},
		{"zero width", Rect{Point{5, 0}, Point{5, 20}}, 0, 20, true},
		{"inverted", Rect{Point{10, 10}, Point{0, 0}}, -10, -10, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.r.Width(); got != tt.w {
				t.Errorf("Width() = %d, want %d", got, tt.w)
			}
			if got := tt.r.Height(); got != tt.h {
				t.Errorf("Height() = %d, want %d", got, tt.h)
			}
			if got := tt.r.Empty(); got != tt.empty {
				t.Errorf("Empty() = %v, want %v", got, tt.empty)
			}
		})
	}
}

func TestRectNormalize(t *testing.T) {
	t.Parallel()
	r := Rect{Point{10, 30}, Point{0, 5}}.Normalize()
	want := Rect{Point{0, 5}, Point{10, 30}}
	if r != want {
		t.Errorf("Normalize() = %v, want %v", r, want)
	}
	if r.Empty() {
		t.Error("normalized rect should not be empty")
	}
}

func TestRectScale(t *testing.T) {
	t.Parallel()
	r := Rect{Point{10, 10}, Point{20, 20}}.Scale(2, 2)
	want := Rect{Point{20, 20}, Point{40, 40}}
	if r != want {
		t.Errorf("Scale(2,2) = %v, want %v", r, want)
	}
}

func TestButton(t *testing.T) {
	t.Parallel()
	tests := []struct {
		b     Button
		str   string
		valid bool
	}{
		{Left, "left", true},
		{Right, "right", true},
		{Middle, "middle", true},
		{Button(99), "unknown", false},
		{Button(-1), "unknown", false},
	}
	for _, tt := range tests {
		if got := tt.b.String(); got != tt.str {
			t.Errorf("Button(%d).String() = %q, want %q", tt.b, got, tt.str)
		}
		if got := tt.b.Valid(); got != tt.valid {
			t.Errorf("Button(%d).Valid() = %v, want %v", tt.b, got, tt.valid)
		}
	}
}
