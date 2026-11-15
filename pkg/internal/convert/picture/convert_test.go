package picture

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"testing"

	"github.com/gotenberg/litho/internal/imagetest"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/svg"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestSVGSize checks an SVG page's size: its width and height, one of them
// through the view box's ratio, else the view box, else 300 by 150 CSS
// pixels.
func TestSVGSize(t *testing.T) {
	t.Parallel()
	view := svgRoot{viewW: 400, viewH: 200}
	for _, tc := range []struct {
		name string
		s    svg.Size
		root svgRoot
		w, h float64
	}{
		{"both", svg.Size{Width: 72, Height: 36, HasWidth: true, HasHeight: true}, view, 72, 36},
		{"width and ratio", svg.Size{Width: 90, HasWidth: true, Ratio: 2}, view, 90, 45},
		{"view box", svg.Size{Ratio: 2}, view, 300, 150},
		{"nothing", svg.Size{}, svgRoot{}, 225, 112.5},
	} {
		if w, h := svgSize(tc.s, tc.root); w != tc.w || h != tc.h {
			t.Errorf("%s: %v by %v, want %v by %v", tc.name, w, h, tc.w, tc.h)
		}
	}
	if w, h := viewBoxSize("0,0 , 30 20"); w != 30 || h != 20 {
		t.Errorf("view box: %v by %v", w, h)
	}
	if w, h := viewBoxSize("0 0 -1 20"); w != 0 || h != 0 {
		t.Errorf("negative view box: %v by %v", w, h)
	}
}

// TestOrientation_DrawsTheStoredCornersWhereTheTagSays checks that each
// orientation's transform takes the stored picture's corners to the
// corners EXIF 2.32, 4.6.4 names for them on the displayed one.
func TestOrientation_DrawsTheStoredCornersWhereTheTagSays(t *testing.T) {
	t.Parallel()
	const w, h = 4, 2
	apply := func(m typeset.Matrix, x, y float64) (float64, float64) {
		return m.A*x + m.C*y + m.E, m.B*x + m.D*y + m.F
	}
	// Where the stored top-left corner lands for each orientation, on a
	// displayed picture of w by h (1 to 4) or h by w (5 to 8).
	want := map[int][2]float64{1: {0, 0}, 2: {w, 0}, 3: {w, h}, 4: {0, h}, 5: {0, 0}, 6: {h, 0}, 7: {h, w}, 8: {0, w}}
	for o, p := range want {
		if x, y := apply(orientation(o, w, h), 0, 0); x != p[0] || y != p[1] {
			t.Errorf("orientation %d: the top-left corner lands at %v, %v, want %v", o, x, y, p)
		}
	}
}

// convert converts data with o under a default budget.
func convert(data []byte, o Options) (*typeset.Document, error) {
	ctx, _, end := limits.WithBudget(context.Background(), limits.Limits{}.WithDefaults())
	defer end()
	return Convert(ctx, bytes.NewReader(data), nil, o)
}

// TestConvert_ClassifiesItsErrors checks the sentinels a conversion ends
// with: malformed for what is no image or does not read, too large past
// the input cap or the pixel cap, too many pages past the page cap.
func TestConvert_ClassifiesItsErrors(t *testing.T) {
	t.Parallel()
	img := image.NewGray(image.Rect(0, 0, 4, 4))
	pages := imagetest.TIFF([]imagetest.TIFFPage{{Image: img, Compression: imagetest.TIFFNone}, {Image: img, Compression: imagetest.TIFFNone}}, nil)
	for _, tc := range []struct {
		name string
		data []byte
		o    Options
		want error
	}{
		{"text", []byte("plain text"), Options{}, office.ErrMalformed},
		{"pixel bomb", imagetest.PNGBomb(), Options{}, limits.ErrInputTooLarge},
		{"input cap", pages, Options{MaxInputBytes: 16}, limits.ErrInputTooLarge},
		{"page cap", pages, Options{MaxPages: 1}, limits.ErrTooManyPages},
	} {
		if _, err := convert(tc.data, tc.o); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
	doc, err := convert(pages, Options{})
	if err != nil || len(doc.Pages) != 2 || len(doc.Structure.Kids) != 2 || doc.Structure.Kids[1].Role != "Figure" {
		t.Errorf("two pages: %v", err)
	}
}

// FuzzConvert converts attacker-controlled bytes: no panic, and an error
// only of the sentinels a conversion documents.
func FuzzConvert(f *testing.F) {
	pic := image.NewNRGBA(image.Rect(0, 0, 12, 8))
	for i := range pic.Pix {
		pic.Pix[i] = uint8(i * 7)
	}
	gray := image.NewGray(image.Rect(0, 0, 9, 9))
	gray.SetGray(3, 3, color.Gray{Y: 200})
	for _, data := range [][]byte{
		imagetest.JPEG(pic, imagetest.JPEGOptions{Components: 3, EXIF: imagetest.EXIF(imagetest.EXIFOptions{Orientation: 5})}),
		imagetest.TIFF([]imagetest.TIFFPage{{Image: gray, Compression: imagetest.TIFFPackBits}}, nil),
		imagetest.RIFFWebP(imagetest.Chunk{FourCC: "VP8L", Body: imagetest.VP8L(pic)}),
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><title>t</title><rect width="5" height="5"/></svg>`),
	} {
		f.Add(data)
		f.Add(data[:len(data)/2])
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		ctx, _, end := limits.WithBudget(context.Background(), limits.Limits{MaxImagePixels: 1 << 16, MaxConvertMemoryBytes: 64 << 20}.WithDefaults())
		defer end()
		_, err := Convert(ctx, bytes.NewReader(data), nil, Options{MaxPages: 4})
		if err != nil && !errors.Is(err, office.ErrMalformed) && !errors.Is(err, limits.ErrInputTooLarge) &&
			!errors.Is(err, limits.ErrTooManyPages) && !errors.Is(err, typeset.ErrInvalidImage) {
			t.Fatalf("undocumented error: %v", err)
		}
	})
}
