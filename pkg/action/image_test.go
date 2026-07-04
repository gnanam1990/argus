package action

import "testing"

func TestImage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		im    Image
		empty bool
		valid bool
	}{
		{"png with data", Image{MIMEPNG, []byte{0x89, 'P'}}, false, true},
		{"jpeg with data", Image{MIMEJPEG, []byte{0xFF, 0xD8}}, false, true},
		{"empty data", Image{MIMEPNG, nil}, true, false},
		{"unknown mime", Image{"image/gif", []byte{1}}, false, false},
		{"data but no mime", Image{"", []byte{1}}, false, false},
		{"zero value", Image{}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.im.Empty(); got != tt.empty {
				t.Errorf("Empty() = %v, want %v", got, tt.empty)
			}
			if got := tt.im.Valid(); got != tt.valid {
				t.Errorf("Valid() = %v, want %v", got, tt.valid)
			}
		})
	}
}
