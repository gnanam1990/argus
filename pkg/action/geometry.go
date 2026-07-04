package action

import "math"

// Button identifies a mouse button.
type Button int

const (
	// Left is the primary mouse button.
	Left Button = iota
	// Right is the secondary (context-menu) mouse button.
	Right
	// Middle is the tertiary (wheel) mouse button.
	Middle
)

// String returns the lowercase button name.
func (b Button) String() string {
	switch b {
	case Left:
		return "left"
	case Right:
		return "right"
	case Middle:
		return "middle"
	default:
		return "unknown"
	}
}

// Valid reports whether b is a known button.
func (b Button) Valid() bool { return b >= Left && b <= Middle }

// Point is an integer 2D coordinate. Depending on context it is expressed in
// screenshot-pixel space or logical-screen space; conversions between the two
// go through Scale and are owned by the action executor, never guessed inline.
type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Scale converts a point by independent per-axis factors, rounding to the
// nearest integer. Asymmetric factors (sx != sy) are supported because HiDPI
// displays and non-square scaling are real; callers must not assume sx == sy.
func (p Point) Scale(sx, sy float64) Point {
	return Point{
		X: int(math.Round(float64(p.X) * sx)),
		Y: int(math.Round(float64(p.Y) * sy)),
	}
}

// Add returns the component-wise sum, used to offset a point by a display
// origin on multi-monitor layouts.
func (p Point) Add(q Point) Point { return Point{X: p.X + q.X, Y: p.Y + q.Y} }

// Rect is an axis-aligned rectangle defined by its inclusive minimum and
// maximum corners, in screenshot-pixel space.
type Rect struct {
	Min Point `json:"min"`
	Max Point `json:"max"`
}

// Width returns the horizontal extent (may be negative if not normalized).
func (r Rect) Width() int { return r.Max.X - r.Min.X }

// Height returns the vertical extent (may be negative if not normalized).
func (r Rect) Height() int { return r.Max.Y - r.Min.Y }

// Empty reports whether the rectangle has no positive area.
func (r Rect) Empty() bool { return r.Width() <= 0 || r.Height() <= 0 }

// Center returns the midpoint of the rectangle. This is the click target a
// set-of-marks index resolves to.
func (r Rect) Center() Point {
	return Point{
		X: (r.Min.X + r.Max.X) / 2,
		Y: (r.Min.Y + r.Max.Y) / 2,
	}
}

// Contains reports whether p lies within r, inclusive of the boundary.
func (r Rect) Contains(p Point) bool {
	return p.X >= r.Min.X && p.X <= r.Max.X &&
		p.Y >= r.Min.Y && p.Y <= r.Max.Y
}

// Normalize returns an equivalent rectangle with Min <= Max on both axes.
func (r Rect) Normalize() Rect {
	if r.Min.X > r.Max.X {
		r.Min.X, r.Max.X = r.Max.X, r.Min.X
	}
	if r.Min.Y > r.Max.Y {
		r.Min.Y, r.Max.Y = r.Max.Y, r.Min.Y
	}
	return r
}

// Scale converts both corners by independent per-axis factors.
func (r Rect) Scale(sx, sy float64) Rect {
	return Rect{Min: r.Min.Scale(sx, sy), Max: r.Max.Scale(sx, sy)}
}
