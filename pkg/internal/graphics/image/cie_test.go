package image

import (
	"context"
	"image"
	"log/slog"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// calArray builds a [/family dict] colour space from numbers.
func calArray(family string, white []float64, gamma ast.PdfObject, matrix []float64) ast.PdfArray {
	nums := func(v []float64) ast.PdfArray {
		out := make(ast.PdfArray, len(v))
		for i, f := range v {
			out[i] = ast.PdfReal(f)
		}
		return out
	}
	dict := ast.PdfDict{}
	if white != nil {
		dict["WhitePoint"] = nums(white)
	}
	if gamma != nil {
		dict["Gamma"] = gamma
	}
	if matrix != nil {
		dict["Matrix"] = nums(matrix)
	}
	return ast.PdfArray{ast.PdfName(family), dict}
}

// srgbD65 returns a CalRGB space real images use: the sRGB primaries and D65
// white with a pure 2.22221 gamma.
//
// # Corpus evidence
//
//   - govdocs: 17,348 images use this space.
func srgbD65() ast.PdfArray {
	g := ast.PdfReal(2.22221)
	return calArray("CalRGB", []float64{0.9505, 1, 1.089}, ast.PdfArray{g, g, g},
		[]float64{0.4124, 0.2126, 0.0193, 0.3576, 0.71519, 0.1192, 0.1805, 0.0722, 0.9505})
}

// decodeOne decodes a one-row 8-bit image in cs holding samples.
func decodeOne(t *testing.T, cs ast.PdfObject, width int, samples []byte) image.Image {
	t.Helper()
	stream := ast.NewStream(ast.PdfDict{
		"Subtype":          ast.PdfName("Image"),
		"Width":            ast.PdfInt(width),
		"Height":           ast.PdfInt(1),
		"ColorSpace":       cs,
		"BitsPerComponent": ast.PdfInt(8),
	}, samples)
	doc := &document.Document{Logger: slog.New(slog.DiscardHandler)}
	page := &document.Page{Resources: ast.PdfDict{"XObject": ast.PdfDict{"Im0": stream}}}
	img, err := DecodeImage(context.Background(), doc, page, "Im0")
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	return img
}

// assertPixels checks each pixel's red, green and blue within one level.
func assertPixels(t *testing.T, img image.Image, want [][3]int) {
	t.Helper()
	for x, w := range want {
		r, g, b, _ := img.At(x, 0).RGBA()
		got := [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
		for c := range 3 {
			if d := got[c] - w[c]; d < -1 || d > 1 {
				t.Errorf("pixel %d = %v, want %v", x, got, w)
				break
			}
		}
	}
}

// TestCalRGB_ToSRGB converts CalRGB samples as PDF.js does for the same
// space, whose pure gamma departs from the sRGB curve near black: the green
// and blue of (255, 2, 2) fall to 0, where reading the samples as DeviceRGB
// kept them.
func TestCalRGB_ToSRGB(t *testing.T) {
	t.Parallel()

	img := decodeOne(t, srgbD65(), 4, []byte{255, 2, 2, 128, 128, 128, 10, 200, 60, 250, 250, 250})
	assertPixels(t, img, [][3]int{{255, 0, 0}, {128, 128, 128}, {2, 201, 56}, {250, 250, 250}})
}

// TestCalRGB_AdaptsWhite takes a D50 white to D65, so a grey, whose tristimulus
// values are a multiple of the white's, stays grey. PDF.js scales the cone
// responses by the white's tristimulus values instead of its cone responses,
// and tints these greys red, (155, 145, 146) for the first.
func TestCalRGB_AdaptsWhite(t *testing.T) {
	t.Parallel()

	g := ast.PdfReal(1.8)
	cs := calArray("CalRGB", []float64{0.9642, 1, 0.8249}, ast.PdfArray{g, g, g},
		[]float64{0.4756, 0.2552, 0.0185, 0.3397, 0.6726, 0.1133, 0.1489, 0.0722, 0.6931})
	img := decodeOne(t, cs, 2, []byte{128, 128, 128, 250, 250, 250})
	assertPixels(t, img, [][3]int{{146, 146, 146}, {251, 251, 251}})
}

// TestCalGray_ToSRGB encodes the luminance a CalGray sample stands for with
// the sRGB transfer function.
func TestCalGray_ToSRGB(t *testing.T) {
	t.Parallel()

	cs := calArray("CalGray", []float64{0.9505, 1, 1.089}, ast.PdfReal(2.2), nil)
	img := decodeOne(t, cs, 4, []byte{0, 64, 128, 255})
	assertPixels(t, img, [][3]int{{0, 0, 0}, {62, 62, 62}, {129, 129, 129}, {255, 255, 255}})
}

// TestCalRGB_IndexedBase converts the colour table of an indexed space over
// CalRGB.
func TestCalRGB_IndexedBase(t *testing.T) {
	t.Parallel()

	cs := ast.PdfArray{ast.PdfName("Indexed"), srgbD65(), ast.PdfInt(1), ast.PdfString([]byte{255, 2, 2, 10, 200, 60})}
	img := decodeOne(t, cs, 2, []byte{0, 1})
	assertPixels(t, img, [][3]int{{255, 0, 0}, {2, 201, 56}})
}

// TestCalRGB_NoWhitePoint reads the samples as device colour when the space
// lacks the /WhitePoint ISO 32000-1, Table 64 requires.
func TestCalRGB_NoWhitePoint(t *testing.T) {
	t.Parallel()

	img := decodeOne(t, calArray("CalRGB", nil, nil, nil), 1, []byte{255, 2, 2})
	assertPixels(t, img, [][3]int{{255, 2, 2}})
}
