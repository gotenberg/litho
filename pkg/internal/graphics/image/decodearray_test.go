package image

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestAppliesDecodeArray pins which images have their /Decode array folded into
// the pixels DecodeImage returns.
//
// A caller that re-encodes those pixels drops /Decode from the new dictionary,
// so a wrong answer here does not fail loudly: it silently renders the image
// with the mapping applied twice, or not at all.
func TestAppliesDecodeArray(t *testing.T) {
	t.Parallel()

	ctx, doc, _ := imagePDF(t, jpxImagePDF("/FlateDecode", "/BitsPerComponent 8"))

	for _, tc := range []struct {
		name string
		dict ast.PdfDict
		want bool
	}{
		{
			"a direct colour space under Flate applies it",
			ast.PdfDict{"ColorSpace": ast.PdfName("DeviceGray"), "Filter": ast.PdfName("FlateDecode")},
			true,
		},
		{
			"no filter at all still applies it",
			ast.PdfDict{"ColorSpace": ast.PdfName("DeviceRGB")},
			true,
		},
		{
			"a JPEG does not: image/jpeg never sees the array",
			ast.PdfDict{"ColorSpace": ast.PdfName("DeviceRGB"), "Filter": ast.PdfName("DCTDecode")},
			false,
		},
		{
			"an indexed space does not: the array remaps indices",
			ast.PdfDict{
				"ColorSpace": ast.PdfArray{
					ast.PdfName("Indexed"), ast.PdfName("DeviceRGB"),
					ast.PdfInt(1), ast.PdfString("\xff\x00\x00\x00\xff\x00"),
				},
			},
			false,
		},
		{
			// Mirrors DecodeImage, which tests only the first filter for
			// DCTDecode. The limitation is inherited on purpose: a predicate
			// that disagreed with the decoder would be worse than one that
			// shares its blind spot.
			"a chain whose first filter is not DCT reports the same as the decoder",
			ast.PdfDict{
				"ColorSpace": ast.PdfName("DeviceRGB"),
				"Filter":     ast.PdfArray{ast.PdfName("ASCII85Decode"), ast.PdfName("DCTDecode")},
			},
			true,
		},
	} {
		if got := AppliesDecodeArray(ctx, doc, tc.dict); got != tc.want {
			t.Errorf("%s: AppliesDecodeArray = %v, want %v", tc.name, got, tc.want)
		}
	}
}
