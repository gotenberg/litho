package imagetest

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"golang.org/x/image/tiff"
)

func TestTIFF_DecodesToItsPages(t *testing.T) {
	t.Parallel()
	src := gradient(53, 37)
	for _, c := range []int{TIFFNone, TIFFPackBits, TIFFDeflate, TIFFLZW} {
		for _, rows := range []int{0, 5} {
			data := TIFF([]TIFFPage{{Image: src, Compression: c, RowsPerStrip: rows, DPI: 150}}, nil)
			img, err := tiff.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("compression %d, rows %d: %v", c, rows, err)
			}
			for y := range 37 {
				for x := range 53 {
					got := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
					if want := src.NRGBAAt(x, y); got != want {
						t.Fatalf("compression %d, rows %d: pixel %d,%d is %v, want %v", c, rows, x, y, got, want)
					}
				}
			}
		}
	}
	// A long run of distinct strings fills the LZW table past its widths.
	noise := image.NewGray(image.Rect(0, 0, 300, 200))
	for i := range noise.Pix {
		noise.Pix[i] = uint8(i*i*7 + i/3)
	}
	img, err := tiff.Decode(bytes.NewReader(TIFF([]TIFFPage{{Image: noise, Compression: TIFFLZW}}, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(img.(*image.Gray).Pix, noise.Pix) {
		t.Error("the LZW page does not decode to its samples")
	}
}
