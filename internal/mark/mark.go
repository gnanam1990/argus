// Package mark renders set-of-marks overlays and prepares screenshots for
// grounding. The numbered-overlay rendering and the number→box index live here
// in pure Go (fogleman/gg + a built-in bitmap font, no CGo, no font file), so
// swapping the detector backend never changes the marking or click-resolution
// code.
package mark

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"strconv"

	"github.com/fogleman/gg"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font/basicfont"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/grounder"
)

// Marker draws numbered boxes over detected elements. It implements
// grounder.Marker.
type Marker struct{}

var _ grounder.Marker = Marker{}

// Overlay draws each element's box and ID onto a copy of img and returns the
// marked PNG plus the ID→box index the executor resolves clicks against.
func (Marker) Overlay(img action.Image, els []grounder.Element) (action.Image, map[int]action.Rect, error) {
	src, _, err := image.Decode(bytes.NewReader(img.Data))
	if err != nil {
		return action.Image{}, nil, fmt.Errorf("mark: decode: %w", err)
	}

	dc := gg.NewContextForImage(src)
	dc.SetFontFace(basicfont.Face7x13)

	for _, el := range els {
		b := el.Box
		label := strconv.Itoa(el.ID)

		// Box outline.
		dc.SetRGB(1, 0, 0)
		dc.SetLineWidth(2)
		dc.DrawRectangle(float64(b.Min.X), float64(b.Min.Y), float64(b.Width()), float64(b.Height()))
		dc.Stroke()

		// Filled label chip in the top-left corner.
		tw, th := dc.MeasureString(label)
		dc.SetRGB(1, 0, 0)
		dc.DrawRectangle(float64(b.Min.X), float64(b.Min.Y), tw+4, th+4)
		dc.Fill()
		dc.SetRGB(1, 1, 1)
		dc.DrawStringAnchored(label, float64(b.Min.X)+2, float64(b.Min.Y)+2, 0, 1)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, dc.Image()); err != nil {
		return action.Image{}, nil, fmt.Errorf("mark: encode: %w", err)
	}
	return action.Image{MIME: action.MIMEPNG, Data: buf.Bytes()}, grounder.Index(els), nil
}

// DownscaleLongEdge shrinks img so its longer edge is at most maxEdge, returning
// the new image and the scale factor (original/new, >= 1). Detections made on
// the returned image are multiplied by the scale to map back to the original
// screenshot space. Images already within the bound are returned unchanged with
// scale 1.
func DownscaleLongEdge(img action.Image, maxEdge int) (action.Image, float64, error) {
	if maxEdge <= 0 {
		return img, 1, nil
	}
	src, _, err := image.Decode(bytes.NewReader(img.Data))
	if err != nil {
		return action.Image{}, 0, fmt.Errorf("mark: decode: %w", err)
	}
	b := src.Bounds()
	ow, oh := b.Dx(), b.Dy()
	long := ow
	if oh > long {
		long = oh
	}
	if long <= maxEdge {
		return img, 1, nil
	}

	scale := float64(long) / float64(maxEdge)
	nw := int(float64(ow) / scale)
	nh := int(float64(oh) / scale)
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return action.Image{}, 0, fmt.Errorf("mark: encode: %w", err)
	}
	return action.Image{MIME: action.MIMEPNG, Data: buf.Bytes()}, scale, nil
}
