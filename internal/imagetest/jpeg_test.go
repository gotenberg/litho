package imagetest

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// gradient returns a picture whose colours run along both axes.
func gradient(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 255 / (w - 1)), G: uint8(y * 255 / (h - 1)), B: 128, A: 255})
		}
	}
	return img
}

func TestJPEG_DecodesCloseToItsPicture(t *testing.T) {
	t.Parallel()
	src := gradient(37, 21)
	for _, o := range []JPEGOptions{
		{Components: 1},
		{Components: 3},
		{Components: 4},
		{Components: 1, Progressive: true},
		{Components: 3, Progressive: true},
		{Components: 4, Progressive: true},
	} {
		data := JPEG(src, o)
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%+v: %v", o, err)
		}
		if img.Bounds() != src.Bounds() {
			t.Fatalf("%+v: decoded %v", o, img.Bounds())
		}
		worst := 0
		for y := range 21 {
			for x := range 37 {
				want := src.NRGBAAt(x, y)
				var got color.NRGBA
				switch p := img.(type) {
				case *image.CMYK:
					// Go reads an Adobe CMYK file inverted back.
					got = color.NRGBAModel.Convert(p.CMYKAt(x, y)).(color.NRGBA)
				default:
					got = color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				}
				if o.Components == 1 {
					g := color.GrayModel.Convert(want).(color.Gray).Y
					want = color.NRGBA{g, g, g, 255}
				}
				for _, d := range []int{int(got.R) - int(want.R), int(got.G) - int(want.G), int(got.B) - int(want.B)} {
					worst = max(worst, d, -d)
				}
			}
		}
		if worst > 8 {
			t.Errorf("%+v: a channel differs by %d", o, worst)
		}
	}
}
