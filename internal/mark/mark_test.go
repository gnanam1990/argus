package mark

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/grounder"
)

func solidPNG(t *testing.T, w, h int) action.Image {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.Gray{Y: 128})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return action.Image{MIME: action.MIMEPNG, Data: buf.Bytes()}
}

func decode(t *testing.T, img action.Image) image.Image {
	t.Helper()
	m, _, err := image.Decode(bytes.NewReader(img.Data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

func rect(x0, y0, x1, y1 int) action.Rect {
	return action.Rect{Min: action.Point{X: x0, Y: y0}, Max: action.Point{X: x1, Y: y1}}
}

func TestOverlayMarksAndIndex(t *testing.T) {
	t.Parallel()
	in := solidPNG(t, 100, 100)
	els := []grounder.Element{
		{ID: 0, Box: rect(10, 10, 40, 30)},
		{ID: 7, Box: rect(50, 50, 90, 80)},
	}
	marked, idx, err := Marker{}.Overlay(in, els)
	if err != nil {
		t.Fatal(err)
	}
	// Result decodes and preserves dimensions.
	m := decode(t, marked)
	if m.Bounds().Dx() != 100 || m.Bounds().Dy() != 100 {
		t.Errorf("marked dims = %v, want 100x100", m.Bounds())
	}
	// Index maps IDs to boxes.
	if idx[7] != rect(50, 50, 90, 80) {
		t.Errorf("idx[7] = %v", idx[7])
	}
	// Something was drawn: the marked image differs from the input.
	if bytes.Equal(marked.Data, in.Data) {
		t.Error("overlay did not modify the image")
	}
}

func TestOverlayEmptyElements(t *testing.T) {
	t.Parallel()
	in := solidPNG(t, 20, 20)
	marked, idx, err := Marker{}.Overlay(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx) != 0 {
		t.Errorf("index should be empty, got %v", idx)
	}
	if decode(t, marked).Bounds().Dx() != 20 {
		t.Error("dims changed")
	}
}

func TestOverlayBadImage(t *testing.T) {
	t.Parallel()
	if _, _, err := (Marker{}).Overlay(action.Image{Data: []byte("not a png")}, nil); err == nil {
		t.Error("expected decode error")
	}
}

func TestDownscaleLongEdge(t *testing.T) {
	t.Parallel()
	in := solidPNG(t, 200, 100)
	out, scale, err := DownscaleLongEdge(in, 100)
	if err != nil {
		t.Fatal(err)
	}
	if scale != 2.0 {
		t.Errorf("scale = %v, want 2", scale)
	}
	m := decode(t, out)
	if m.Bounds().Dx() != 100 || m.Bounds().Dy() != 50 {
		t.Errorf("downscaled dims = %v, want 100x50", m.Bounds())
	}
}

func TestDownscaleNoOpWhenSmall(t *testing.T) {
	t.Parallel()
	in := solidPNG(t, 50, 40)
	out, scale, err := DownscaleLongEdge(in, 100)
	if err != nil {
		t.Fatal(err)
	}
	if scale != 1.0 {
		t.Errorf("scale = %v, want 1", scale)
	}
	if !bytes.Equal(out.Data, in.Data) {
		t.Error("small image should be returned unchanged")
	}
}

func TestDownscaleDisabled(t *testing.T) {
	t.Parallel()
	in := solidPNG(t, 200, 100)
	out, scale, err := DownscaleLongEdge(in, 0)
	if err != nil || scale != 1 || !bytes.Equal(out.Data, in.Data) {
		t.Errorf("maxEdge=0 should be a no-op: scale=%v err=%v", scale, err)
	}
}
