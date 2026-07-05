//go:build robotgo && !darwin

package robotgo

import (
	"bytes"
	"context"
	"fmt"
	"image/png"

	"github.com/go-vgo/robotgo"

	"github.com/gnanam1990/argus/pkg/action"
)

// captureDisplay screenshots the driven display via robotgo and encodes it as
// PNG. On non-macOS platforms robotgo's per-display capture is correct, so no
// screencapture shim is needed.
func (d *Driver) captureDisplay(_ context.Context) (action.Image, error) {
	img, err := robotgo.CaptureImg(d.display)
	if err != nil {
		return action.Image{}, captureError(err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return action.Image{}, fmt.Errorf("robotgo encode: %w", err)
	}
	return action.Image{MIME: action.MIMEPNG, Data: buf.Bytes()}, nil
}
