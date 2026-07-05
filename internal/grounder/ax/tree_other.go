//go:build !darwin || !cgo

package ax

// nativeWalk is unavailable off a darwin cgo build; the tree source falls back
// to the osascript path (which, on non-darwin, its ExecRunner rejects anyway).
func nativeWalk() (wireScreen, []wireElement, error) {
	return wireScreen{}, nil, errNativeUnavailable
}
