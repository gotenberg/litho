package image

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestDecodeImage_ReadsShortDataFromTheBytesThereAre pins the reading of an
// image whose data stops short (see [padShortSamples]): the samples the data
// holds keep their values, a partial last row's among them, and every sample
// it lacks is zero, as PDFium 4.30.0 and MuPDF 1.26.4 read it. Before, the
// rows from the first one the data did not complete were painted black, as
// PDF.js paints them, and a dark line crossed an image poppler, PDFium and
// MuPDF draw whole (govdocs thread_200_200172.pdf).
func TestDecodeImage_ReadsShortDataFromTheBytesThereAre(t *testing.T) {
	t.Parallel()

	// An 8x8 DeviceRGB image of mid grey, with one row and with half a row
	// of its samples missing.
	const dict = "/ColorSpace /DeviceRGB /BitsPerComponent 8"
	full := bytes.Repeat([]byte{128}, 8*8*3)
	for name, tc := range map[string]struct {
		data []byte
		// keptInLast is how many pixels of the last row the data holds.
		keptInLast int
	}{
		"one row short":  {full[:7*8*3], 0},
		"half row short": {full[:7*8*3+4*3], 4},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, doc, page := imagePDF(t, filteredImagePDF(dict, tc.data))
			img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
			if err != nil {
				t.Fatalf("DecodeImage: %v", err)
			}
			for y := range 8 {
				for x := range 8 {
					want := uint32(128)
					if y == 7 && x >= tc.keptInLast {
						want = 0
					}
					r, g, b, _ := img.At(x, y).RGBA()
					if r>>8 != want || g>>8 != want || b>>8 != want {
						t.Fatalf("pixel (%d,%d) = (%d,%d,%d), want %d", x, y, r>>8, g>>8, b>>8, want)
					}
				}
			}
		})
	}
}

// TestDecodeImage_ShortDataReadsZeroThroughDecode pins that a sample the data
// lacks is zero before /Decode, not black after it: an inverted /Decode reads
// it as white, as PDFium and MuPDF draw it.
func TestDecodeImage_ShortDataReadsZeroThroughDecode(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t, filteredImagePDF("/ColorSpace /DeviceGray /BitsPerComponent 8 /Decode [1 0]",
		bytes.Repeat([]byte{100}, 7*8)))
	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	if r, _, _, _ := img.At(0, 7).RGBA(); r>>8 != 255 {
		t.Errorf("missing sample = %d, want 255, a zero sample under /Decode [1 0]", r>>8)
	}
}

// TestDecodeImage_ShortStencilRowsPaintNothing pins the one exception:
// whole rows a stencil mask's data lacks paint nothing, whatever its
// /Decode, as PDFium and poppler leave them.
func TestDecodeImage_ShortStencilRowsPaintNothing(t *testing.T) {
	t.Parallel()

	for _, decode := range []string{"", " /Decode [1 0]"} {
		ctx, doc, page := imagePDF(t, filteredImagePDF("/ImageMask true"+decode, []byte{0x0F, 0x0F, 0x0F}))
		img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
		if err != nil {
			t.Fatalf("DecodeImage%s: %v", decode, err)
		}
		for y := 3; y < 8; y++ {
			if r, _, _, _ := img.At(0, y).RGBA(); r>>8 != 255 {
				t.Errorf("/Decode%q: row %d = %d, want 255, unpainted", decode, y, r>>8)
			}
		}
	}
}

// TestDecodeImage_EmptyDataReportsNoSample pins the refusal that stays: data
// that holds no byte has no sample to read.
func TestDecodeImage_EmptyDataReportsNoSample(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t, filteredImagePDF("/ColorSpace /DeviceRGB /BitsPerComponent 8", nil))
	_, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err == nil || !strings.Contains(err.Error(), "no sample") {
		t.Errorf("DecodeImage = %v, want the refusal of data holding no sample", err)
	}
}

// TestPadShortSamples_PadsAnyShortData pins that data short of one row is
// padded too, while empty data comes back as it is for the caller to refuse.
func TestPadShortSamples_PadsAnyShortData(t *testing.T) {
	t.Parallel()

	got, rows := padShortSamples(nil, 8, 8, 3, 8)
	if rows != 8 || len(got) != 0 {
		t.Errorf("empty: %d bytes, %d rows; want 0 bytes, 8 rows", len(got), rows)
	}
	got, rows = padShortSamples([]byte{1, 2, 3}, 8, 8, 3, 8)
	if rows != 0 || len(got) != 8*8*3 || got[2] != 3 || got[3] != 0 {
		t.Errorf("short of a row: %d bytes, %d rows", len(got), rows)
	}
	got, rows = padShortSamples(make([]byte, 30), 8, 8, 3, 8)
	if rows != 1 || len(got) != 8*8*3 {
		t.Errorf("one row: %d bytes, %d rows; want %d bytes, 1 row", len(got), rows, 8*8*3)
	}
}
