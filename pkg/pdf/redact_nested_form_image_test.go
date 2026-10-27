package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/filter/dct"
	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// TestWriter_Redact_BlanksANestedFormImageInPlace pins that an image drawn
// through nested forms, every placement of it under the regions, is blanked
// in place: no form is copied, and strict redaction accepts it.
//
// The shared-image check followed one form level, so an image a nested form
// named counted as used where it could not look: it was blanked in a copy of
// every form of the chain, and strict redaction refused it.
func TestWriter_Redact_BlanksANestedFormImageInPlace(t *testing.T) {
	t.Parallel()

	for _, depth := range []int{2, 3, 5} {
		for _, tc := range []struct {
			name        string
			contents    []string
			regions     []RedactRegion
			top, bottom uint8
		}{
			{"whole placement", []string{placeFormLeft}, []RedactRegion{{Page: 1, Rect: wholeLeft}}, 0, 0},
			{"top half", []string{placeFormLeft}, []RedactRegion{{Page: 1, Rect: topLeft}}, 0, 0x40},
			{
				"same samples on every page",
				[]string{placeFormLeft, placeFormLeft},
				[]RedactRegion{{Page: 1, Rect: topLeft}, {Page: 2, Rect: topLeft}},
				0, 0x40,
			},
		} {
			t.Run(strconv.Itoa(depth)+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				src := formChainDoc(depth, tc.contents)
				for _, opts := range [][]RedactOption{nil, {WithStrictRedaction()}} {
					out, report, err := redactWith(t, src, tc.regions, opts...)
					if err != nil {
						t.Fatalf("Redact (%d options): %v", len(opts), err)
					}
					if len(report.ImagesBlankedInCopy) != 0 || len(report.FormsCopied) != 0 {
						t.Errorf("ImagesBlankedInCopy = %v, FormsCopied = %v, want none",
							report.ImagesBlankedInCopy, report.FormsCopied)
					}
					for page := 1; page <= len(tc.contents); page++ {
						if got := renderedGray(t, out, page, copyLeftTop); !nearGray(got, tc.top) {
							t.Errorf("page %d top row = %d, want %d", page, got, tc.top)
						}
						if got := renderedGray(t, out, page, copyLeftBottom); !nearGray(got, tc.bottom) {
							t.Errorf("page %d bottom row = %d, want %d", page, got, tc.bottom)
						}
					}
				}
			})
		}
	}
}

// TestWriter_Redact_CopiesANestedFormImageDrawnUncovered pins that an image
// drawn through nested forms, and also drawn through them where no region
// reaches, is still blanked in a copy, and refused under strict redaction.
func TestWriter_Redact_CopiesANestedFormImageDrawnUncovered(t *testing.T) {
	t.Parallel()

	for _, depth := range []int{2, 3} {
		t.Run(strconv.Itoa(depth), func(t *testing.T) {
			t.Parallel()
			src := formChainDoc(depth, []string{placeFormLeft, placeFormLeft})
			regions := []RedactRegion{{Page: 1, Rect: wholeLeft}}
			out, report, err := redactWith(t, src, regions)
			if err != nil {
				t.Fatalf("Redact: %v", err)
			}
			if got := report.FormsCopied[1]; got != depth {
				t.Errorf("FormsCopied[1] = %d, want %d", got, depth)
			}
			if got := renderedGray(t, out, 1, copyLeftTop); !nearGray(got, 0) {
				t.Errorf("covered placement = %d, want 0", got)
			}
			if got := renderedGray(t, out, 2, copyLeftTop); !nearGray(got, 0x40) {
				t.Errorf("page 2 = %d, want 0x40", got)
			}
			if _, _, err := redactWith(t, src, regions, WithStrictRedaction()); !errors.Is(err, mutations.ErrImageShared) {
				t.Errorf("strict Redact = %v, want ErrImageShared", err)
			}
		})
	}
}

// TestWriter_Redact_CopiesAnImageDrawnPastTheFormDepthBudget pins that an
// image a form drawn past the document's form depth budget also paints is
// blanked in a copy. The redaction reads no deeper than the budget, so that
// placement goes unmeasured, while a viewer reading deeper draws it.
func TestWriter_Redact_CopiesAnImageDrawnPastTheFormDepthBudget(t *testing.T) {
	t.Parallel()

	// Page 2 draws the chain of page 1 through one more form, object 30, so
	// its placement of the image sits at depth 3.
	const deeper = "/Fm1 Do"
	src := formChainDoc(2, []string{placeFormLeft, "q 1 0 0 1 100 500 cm /Fm2 Do Q"})
	src = bytes.Replace(src, []byte("/Fm1 20 0 R >>"), []byte("/Fm1 20 0 R /Fm2 30 0 R >>"), 1)
	src = bytes.Replace(src, []byte("trailer\n"), []byte("30 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] "+
		"/Resources << /XObject << /Fm1 20 0 R >> >> /Length "+strconv.Itoa(len(deeper))+" >>\nstream\n"+deeper+
		"\nendstream\nendobj\ntrailer\n"), 1)

	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)), WithLimits(Limits{MaxFormDepth: 2}))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	report, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: wholeLeft}})
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if got := report.FormsCopied[1]; got != 2 {
		t.Errorf("FormsCopied = %v, want two forms on page 1", report.FormsCopied)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if got := renderedGray(t, out.Bytes(), 2, copyLeftTop); !nearGray(got, 0x40) {
		t.Errorf("page 2, drawn past the budget = %d, want 0x40", got)
	}
}

// TestWriterRedactBlanksALossyTileInPlace pins that a second redaction over
// a photograph a lossy blanking redrew blanks the tile it reaches in place.
// Each JPEG tile is drawn by a form of its own inside the form that replaced
// the picture, two levels down, where the shared-image check did not look,
// so the tile was blanked in a copy of both forms.
func TestWriterRedactBlanksALossyTileInPlace(t *testing.T) {
	t.Parallel()
	first, _ := redactBytes(t, jpegPhotoPDF(t, ""), lossyBlankRegion, WithRedactionJPEGQuality(75))
	_, before := drawnPicture(t, first)
	corner := [4]float64{10, 10, 30, 30}
	reached := false
	for _, j := range before {
		// The JPEG tiles, page space: the picture fills the 200pt page.
		minX, maxX := 200*float64(j.Min.X)/lossyBlankSide, 200*float64(j.Max.X)/lossyBlankSide
		minY, maxY := 200*(1-float64(j.Max.Y)/lossyBlankSide), 200*(1-float64(j.Min.Y)/lossyBlankSide)
		if minX <= corner[0] && maxX >= corner[2] && minY <= corner[1] && maxY >= corner[3] {
			reached = true
		}
	}
	if !reached {
		t.Fatalf("no JPEG tile under %v among %v", corner, before)
	}

	second, report := redactBytes(t, first, corner)
	if len(report.ImagesBlankedInCopy) != 0 || len(report.FormsCopied) != 0 {
		t.Errorf("ImagesBlankedInCopy = %v, FormsCopied = %v, want none", report.ImagesBlankedInCopy, report.FormsCopied)
	}
	picture, _ := drawnPicture(t, second)
	covered := 0
	for y := range lossyBlankSide {
		for x := range lossyBlankSide {
			ux := 200 * (float64(x) + 0.5) / lossyBlankSide
			uy := 200 * (1 - (float64(y)+0.5)/lossyBlankSide)
			if ux < corner[0] || ux > corner[2] || uy < corner[1] || uy > corner[3] {
				continue
			}
			covered++
			if i := 3 * (y*lossyBlankSide + x); !bytes.Equal(picture[i:i+3], []byte{0, 0, 0}) {
				t.Fatalf("covered sample (%d, %d) draws %v, want the blank", x, y, picture[i:i+3])
			}
		}
	}
	if covered == 0 {
		t.Fatal("the region covers no sample")
	}
}

// TestWriterRedactCopiesASharedPhotographNextToALossyTile pins that a second
// redaction over a lossily blanked placement still blanks the source's own
// photograph in a copy when the photograph is painted elsewhere, uncovered.
// The JPEG tiles the first redaction wrote are blanked in place; the
// photograph is not one of them: the page still draws it twice past the
// first region, and blanking it in place would blank the placement no region
// covers.
//
// # Corpus evidence
//
//   - pdfjs: issue269_2 draws each of its four photographs 24 times through
//     the forms of its layers. A second redaction meeting a tile of the
//     first's lossy form and one of those placements blanks the tiles in
//     place and copies the four photographs, as the same region does on the
//     source.
func TestWriterRedactCopiesASharedPhotographNextToALossyTile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data, err := dct.Encode(ctx, photoSamples(lossyBlankSide), lossyBlankSide, lossyBlankSide, 3, dct.EncodeOptions{Quality: 90})
	if err != nil {
		t.Fatal(err)
	}
	const page = "q 200 0 0 200 0 0 cm /Im Do Q q 200 0 0 200 200 0 cm /Im Do Q q 200 0 0 200 400 0 cm /Im Do Q"
	src := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 600 200] /Contents 4 0 R /Resources << /XObject << /Im 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream",
			lossyBlankSide, lossyBlankSide, len(data), data),
	})

	first, report := redactBytes(t, src, lossyBlankRegion, WithRedactionJPEGQuality(75))
	if report.LossyBlankedImages == 0 {
		t.Fatal("the first redaction drew no JPEG tile")
	}
	// Under the first placement's lower-left tile, and the second
	// placement's lower-left corner.
	region := [4]float64{10, 10, 230, 30}
	second, report := redactBytes(t, first, region)
	if got := report.ImagesBlankedInCopy[1]; !slices.Equal(got, []string{"Im"}) {
		t.Errorf("ImagesBlankedInCopy[1] = %v, want [Im]", got)
	}
	if n := len(report.BlankedImages); n < 2 {
		t.Errorf("BlankedImages = %v, want the tiles and the photograph", report.BlankedImages)
	}

	for _, at := range [][2]float64{{20, 20}, {220, 20}} {
		if got := renderedGray(t, second, 1, at); got > 8 {
			t.Errorf("covered point %v = %d, want the blank", at, got)
		}
	}
	// The third placement, which no region meets, keeps the photograph.
	at := [2]float64{420, 20}
	if got, want := renderedGray(t, second, 1, at), renderedGray(t, src, 1, at); got != want {
		t.Errorf("uncovered point %v = %d, want %d", at, got, want)
	}

	if _, _, err := redactWith(t, first, []RedactRegion{{Page: 1, Rect: region}}, WithStrictRedaction()); !errors.Is(err, mutations.ErrImageShared) {
		t.Errorf("strict Redact = %v, want ErrImageShared", err)
	}
}
