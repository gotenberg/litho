package imgpaint

import (
	"errors"
	"image"
	"image/color"
	"slices"
	"testing"
)

func TestColorKey(t *testing.T) {
	tests := []struct {
		name                     string
		data                     []byte
		width, height, comps, bc int
		ranges                   []int
		want                     []uint8
	}{
		{
			name:  "8-bit RGB",
			data:  []byte{0, 255, 0, 10, 20, 30},
			width: 2, height: 1, comps: 3, bc: 8,
			ranges: []int{0, 0, 250, 255, 0, 0},
			want:   []uint8{0, 255},
		},
		{
			// Rows pad to a byte: 3 one-bit samples, then 5 bits of padding.
			name:  "1-bit grey",
			data:  []byte{0b101_00000, 0b010_00000},
			width: 3, height: 2, comps: 1, bc: 1,
			ranges: []int{1, 1},
			want:   []uint8{0, 255, 0, 255, 0, 255},
		},
		{
			name:  "2-bit indexed range",
			data:  []byte{0b00_01_10_11},
			width: 4, height: 1, comps: 1, bc: 2,
			ranges: []int{1, 2},
			want:   []uint8{255, 0, 0, 255},
		},
		{
			// ISO 32000-1, 8.9.6.4 compares the raw value, all 16 bits.
			name:  "16-bit grey",
			data:  []byte{0x12, 0x34, 0x12, 0x35},
			width: 2, height: 1, comps: 1, bc: 16,
			ranges: []int{0x1234, 0x1234},
			want:   []uint8{0, 255},
		},
		{
			name:  "short data paints the missing rows",
			data:  []byte{7},
			width: 1, height: 3, comps: 1, bc: 8,
			ranges: []int{7, 7},
			want:   []uint8{0, 255, 255},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := ColorKey(tt.data, tt.width, tt.height, tt.comps, tt.bc, tt.ranges, testMaxPixels)
			if err != nil {
				t.Fatal(err)
			}
			if a.Width != tt.width || a.Height != tt.height || !slices.Equal(a.A, tt.want) {
				t.Errorf("got %dx%d %v, want %dx%d %v", a.Width, a.Height, a.A, tt.width, tt.height, tt.want)
			}
		})
	}
}

func TestColorKeyRefuses(t *testing.T) {
	tests := []struct {
		name                     string
		width, height, comps, bc int
		ranges                   []int
	}{
		{"zero width", 0, 1, 1, 8, []int{0, 0}},
		{"over the pixel budget", 1 << 13, 1 << 12, 1, 8, []int{0, 0}},
		{"ranges do not match components", 1, 1, 3, 8, []int{0, 0}},
		{"too many components", 1, 1, 33, 8, make([]int, 66)},
		{"depth 3", 1, 1, 1, 3, []int{0, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ColorKey(nil, tt.width, tt.height, tt.comps, tt.bc, tt.ranges, testMaxPixels)
			if !errors.Is(err, ErrBadMask) {
				t.Errorf("err = %v, want ErrBadMask", err)
			}
		})
	}
}

func TestStencilAndSoftMaskPlanes(t *testing.T) {
	g := greyImage(2, 1, 0, 200)
	if s := StencilMask(g, true); !slices.Equal(s.A, []uint8{255, 55}) || !s.Interpolate {
		t.Errorf("StencilMask = %v, want inverted grey", s)
	}
	if s := SoftMask(g, false); !slices.Equal(s.A, []uint8{0, 200}) {
		t.Errorf("SoftMask = %v, want the grey levels", s.A)
	}
	// An image that is not Gray is read through the grey model, and a
	// sub-image is read from its own origin.
	rgba := image.NewRGBA(image.Rect(0, 0, 3, 1))
	rgba.SetRGBA(1, 0, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	if s := SoftMask(rgba.SubImage(image.Rect(1, 0, 3, 1)), false); !slices.Equal(s.A, []uint8{255, 0}) {
		t.Errorf("SoftMask of a sub-image = %v, want [255 0]", s.A)
	}
}
