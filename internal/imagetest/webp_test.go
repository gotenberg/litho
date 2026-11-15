package imagetest

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"golang.org/x/image/webp"
)

func TestVP8L_DecodesToItsPicture(t *testing.T) {
	t.Parallel()
	for _, translucent := range []bool{false, true} {
		src := gradient(19, 13)
		if translucent {
			for i := 3; i < len(src.Pix); i += 4 {
				src.Pix[i] = uint8(i)
			}
		}
		data := RIFFWebP(Chunk{"VP8L", VP8L(src)})
		img, err := webp.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		for y := range 13 {
			for x := range 19 {
				if got := color.NRGBAModel.Convert(img.At(x, y)); got != src.NRGBAAt(x, y) {
					t.Fatalf("translucent %t: pixel %d,%d is %v, want %v", translucent, x, y, got, src.NRGBAAt(x, y))
				}
			}
		}
	}
	_ = image.Rect
}

func TestVP8_DecodesToItsBlocks(t *testing.T) {
	t.Parallel()
	// A picture of flat 16 by 16 blocks, which the encoder's one level a
	// macroblock holds up to the YCbCr round trip.
	src := image.NewNRGBA(image.Rect(0, 0, 40, 35))
	for y := range 35 {
		for x := range 40 {
			k := x/16 + 3*(y/16)
			src.SetNRGBA(x, y, color.NRGBA{uint8(40 * k), uint8(255 - 30*k), uint8(90 + 15*k), 255})
		}
	}
	data := RIFFWebP(Chunk{"VP8 ", VP8(src)})
	img, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	worst := 0
	for y := range 35 {
		for x := range 40 {
			g := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			w := src.NRGBAAt(x, y)
			for _, d := range []int{int(g.R) - int(w.R), int(g.G) - int(w.G), int(g.B) - int(w.B)} {
				worst = max(worst, d, -d)
			}
		}
	}
	if worst > 4 {
		t.Errorf("a channel differs by %d", worst)
	}
}
