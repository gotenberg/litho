package recolor

import (
	"context"
	"io"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// FuzzConvertImage converts an image whose samples, dimensions, depth,
// colour space and filter come from the input, and asserts it never panics
// and that a converted image holds the samples its dictionary states.
func FuzzConvertImage(f *testing.F) {
	f.Add([]byte{0, 51, 102, 255, 255, 255}, uint8(2), uint8(1), uint8(8), uint8(1), false)
	f.Add([]byte{0, 51}, uint8(2), uint8(2), uint8(8), uint8(1), false)
	f.Add([]byte{0x0f, 0x50, 0xa5}, uint8(3), uint8(2), uint8(4), uint8(0), true)
	f.Add([]byte{0, 1, 1, 0}, uint8(4), uint8(1), uint8(8), uint8(3), false)
	spaces := []ast.PdfObject{
		familyGray, familyRGB, familyCMYK,
		ast.PdfArray{familyIndexed, familyRGB, ast.PdfInt(1), ast.PdfString("\xff\x00\x00\x00\x00\xff")},
		ast.PdfArray{familyLab, ast.PdfDict{"WhitePoint": ast.PdfArray{ast.PdfReal(0.9505), ast.PdfInt(1), ast.PdfReal(1.089)}}},
	}
	f.Fuzz(func(t *testing.T, data []byte, w, h, bpc, space uint8, flate bool) {
		extra := ast.PdfDict{}
		if flate {
			extra["Filter"] = ast.PdfName("FlateDecode")
		}
		if bpc%3 == 0 {
			extra["Mask"] = ast.PdfArray{ast.PdfInt(0), ast.PdfInt(int64(bpc))}
		}
		img := imageStream(int(w), int(h), spaces[int(space)%len(spaces)], int(bpc%17), data, extra)
		p := newTestPage(1, 1, img)
		alloc, _, outcome, err := p.convert(t, deviceTarget(familyCMYK), fixedColors(deviceColors{tc: 4}))
		if err != nil || outcome != ImageConverted {
			return
		}
		out := pending(t, alloc, imageRef.ObjectNum)
		doc, _ := p.open(t)
		rc, err := doc.NewStreamReaderChecked(context.Background(), out)
		if err != nil {
			t.Fatalf("the converted image does not decode: %v", err)
		}
		defer rc.Close()
		samples, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("reading the converted image: %v", err)
		}
		if _, indexed := out.Dict["ColorSpace"].(ast.PdfArray); indexed {
			// The samples were kept as they were, short or not.
			return
		}
		depth, _ := out.Dict["BitsPerComponent"].(ast.PdfInt)
		if len(samples) != int(w)*int(h)*4*int(depth)/8 {
			t.Fatalf("%d bytes for a %dx%d CMYK image of %d bits", len(samples), w, h, depth)
		}
	})
}
