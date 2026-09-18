package image

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestStatedBitsPerComponent pins which depth an image is read at.
//
// This became load-bearing when packed samples started being unpacked. Before
// that, a JPXDecode image stating 4 was simply refused by the "BitsPerComponent
// must be 8" guard; afterwards, honouring the stated 4 would stride the
// codestream's 8-bit output as though it were packed and mis-read every pixel.
// A wrong answer is worse than the refusal it replaced, so the rule is pinned
// here rather than left to the two call sites.
//
// It is tested on the helper rather than through DecodeImage on purpose: JPEG
// 2000 needs the CGO plugin, so an end-to-end decode fails at the filter stage
// whether or not this rule is applied, and such a test would pass even with the
// rule deleted.
func TestStatedBitsPerComponent(t *testing.T) {
	t.Parallel()

	// Any document will do: the dictionaries below hold direct values, and the
	// document is only what resolveInt chases references through.
	ctx, doc, _ := imagePDF(t, jpxImagePDF("/FlateDecode", "/BitsPerComponent 8"))

	for _, tc := range []struct {
		name string
		dict ast.PdfDict
		want int
	}{
		{
			"JPXDecode ignores a stated depth",
			ast.PdfDict{"Filter": ast.PdfName("JPXDecode"), "BitsPerComponent": ast.PdfInt(4)},
			8,
		},
		{
			"JPXDecode last in a chain still ignores it",
			ast.PdfDict{
				"Filter":           ast.PdfArray{ast.PdfName("FlateDecode"), ast.PdfName("JPXDecode")},
				"BitsPerComponent": ast.PdfInt(4),
			},
			8,
		},
		{
			"a non-JPX filter keeps the stated depth",
			ast.PdfDict{"Filter": ast.PdfName("FlateDecode"), "BitsPerComponent": ast.PdfInt(4)},
			4,
		},
		{
			"one bit survives, since sub-byte depths are now decodable",
			ast.PdfDict{"Filter": ast.PdfName("CCITTFaxDecode"), "BitsPerComponent": ast.PdfInt(1)},
			1,
		},
		{
			"an absent entry defaults to 8",
			ast.PdfDict{"Filter": ast.PdfName("FlateDecode")},
			8,
		},
	} {
		if got := statedBitsPerComponent(ctx, doc, tc.dict); got != tc.want {
			t.Errorf("%s: bitsPerComponent = %d, want %d", tc.name, got, tc.want)
		}
	}
}
