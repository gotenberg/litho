package image

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestDecodeImage_DirectLab converts the L*, a* and b* of a direct Lab image
// to sRGB: full lightness with neutral a* and b* is white, zero lightness
// black. Read as RGB samples, the white came out pink.
func TestDecodeImage_DirectLab(t *testing.T) {
	t.Parallel()

	cs := ast.PdfArray{ast.PdfName("Lab"), ast.PdfDict{
		"WhitePoint": ast.PdfArray{ast.PdfReal(0.9642), ast.PdfInt(1), ast.PdfReal(0.8249)},
		"Range":      ast.PdfArray{ast.PdfInt(-128), ast.PdfInt(127), ast.PdfInt(-128), ast.PdfInt(127)},
	}}
	img := decodeOne(t, cs, 2, []byte{255, 128, 128, 0, 128, 128})
	assertPixels(t, img, [][3]int{{255, 255, 255}, {0, 0, 0}})
}
