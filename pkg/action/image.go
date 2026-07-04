package action

// MIME types Argus accepts for screenshots.
const (
	MIMEPNG  = "image/png"
	MIMEJPEG = "image/jpeg"
)

// Image is an encoded screenshot passed between the driver and the model
// adapters. It is deliberately format-agnostic: adapters re-encode to whatever
// wire shape their provider expects (base64 block, data URL, inline blob).
type Image struct {
	MIME string `json:"mime"`
	Data []byte `json:"data"`
}

// Empty reports whether the image carries no pixel data.
func (im Image) Empty() bool { return len(im.Data) == 0 }

// Valid reports whether the image has a supported MIME type and non-empty data.
func (im Image) Valid() bool {
	return !im.Empty() && (im.MIME == MIMEPNG || im.MIME == MIMEJPEG)
}
