package image

import (
	"bytes"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestDecodeImage_AllocationsDoNotGrowWithPixels pins that writing a decoded
// pixel allocates nothing. The writes went through image.Image's Set, which
// boxes each colour in a color.Color interface the pixel's colour model then
// converts, one heap allocation per pixel for an RGBA colour: a 2560x3300
// image paid 8.4 million of them on top of the conversion.
//
// # Corpus evidence
//
//   - govdocs thread_200_200293.pdf: 23 page-sized 1-bit scans took 18
//     seconds to decode and score, 5 of them in Set and the colour model
//     behind it, so the gate's 20-second budget ran out and none of its
//     images was scored.
func TestDecodeImage_AllocationsDoNotGrowWithPixels(t *testing.T) {
	cases := []struct {
		name, cs string
		comps    int
	}{
		{"DeviceGray", "/DeviceGray", 1},
		{"DeviceRGB", "/DeviceRGB", 3},
		{"DeviceCMYK", "/DeviceCMYK", 4},
		{"Indexed", "[/Indexed /DeviceRGB 1 <FF000000FF00>]", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allocs := func(side int) float64 {
				data := bytes.Repeat([]byte{1}, side*side*tc.comps)
				ctx, doc, page := imagePDF(t, packedImagePDF(side, side, 8, tc.cs, "", data))
				return testing.AllocsPerRun(3, func() {
					if _, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0")); err != nil {
						t.Fatalf("DecodeImage: %v", err)
					}
				})
			}
			small, large := allocs(32), allocs(128)
			// The large image holds 15,360 more pixels than the small one; a
			// fixed cost per image stays well under one allocation per 100.
			if grown := large - small; grown > 150 {
				t.Errorf("decoding 128x128 allocates %.0f more times than 32x32 (%.0f vs %.0f): allocations grow with the pixel count", grown, large, small)
			}
		})
	}
}
