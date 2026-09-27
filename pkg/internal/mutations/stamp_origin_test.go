package mutations

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestStampOriginShiftUsesCropBox pins that a stamp is measured from the page's
// visible corner.
//
// TextStamp documents X and Y as "points from the page's bottom-left corner",
// but a content stream draws in user space, where (0, 0) is the coordinate
// origin. Those coincide only when the box starts at (0, 0), which real files
// do not always do.
//
// # Corpus evidence
//
//   - All corpora: 4% of the files measured do not start their box at (0, 0).
//   - govdocs thread_000_000018.pdf: uses [-306 -396 306 396], where an
//     unshifted stamp began at the page's centre and ran off the right edge,
//     losing its last character to the crop.
func TestStampOriginShiftUsesCropBox(t *testing.T) {
	page := &document.Page{
		MediaBox: ast.PdfArray{ast.PdfInt(-306), ast.PdfInt(-396), ast.PdfInt(306), ast.PdfInt(396)},
		CropBox:  ast.PdfArray{ast.PdfInt(-306), ast.PdfInt(-396), ast.PdfInt(306), ast.PdfInt(396)},
	}

	dx, dy, ok := stampOriginShift(page)
	if !ok {
		t.Fatal("a box starting at (-306, -396) needs a shift, got none")
	}
	if dx != -306 || dy != -396 {
		t.Errorf("shift = (%v, %v), want (-306, -396)", dx, dy)
	}
}

// TestStampOriginShiftSkipsOrdinaryPages checks the common page is untouched, so
// the shared stamp stream and its byte-identical output survive.
func TestStampOriginShiftSkipsOrdinaryPages(t *testing.T) {
	page := &document.Page{
		MediaBox: ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(612), ast.PdfInt(792)},
		CropBox:  ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(612), ast.PdfInt(792)},
	}

	if _, _, ok := stampOriginShift(page); ok {
		t.Error("a box starting at (0, 0) needs no shift, but one was reported")
	}
}

// TestStampOriginShiftFallsBackToMediaBox checks the MediaBox is used when no
// crop box is present, which is what ISO 32000-1, 14.11.2 makes the crop box
// default to.
func TestStampOriginShiftFallsBackToMediaBox(t *testing.T) {
	page := &document.Page{
		MediaBox: ast.PdfArray{ast.PdfReal(37), ast.PdfReal(37), ast.PdfReal(649), ast.PdfReal(829)},
	}

	dx, dy, ok := stampOriginShift(page)
	if !ok {
		t.Fatal("a media box starting at (37, 37) needs a shift, got none")
	}
	if dx != 37 || dy != 37 {
		t.Errorf("shift = (%v, %v), want (37, 37)", dx, dy)
	}
}

// TestStampOriginShiftReadsTheViewedBox pins that the stamp is measured from
// the bottom-left corner of the region a viewer shows (see
// [document.Page.ViewedBox]): the lesser of each pair of corners, whichever
// pair a rectangle names (ISO 32000-1, 7.9.5), and US Letter from (0, 0) where
// the page has no box a viewer uses. A media box of three entries, or of no
// width, moved the stamp by its first two entries.
func TestStampOriginShiftReadsTheViewedBox(t *testing.T) {
	box := func(v ...float64) ast.PdfArray {
		arr := make(ast.PdfArray, len(v))
		for i, x := range v {
			arr[i] = ast.PdfReal(x)
		}
		return arr
	}
	for _, tc := range []struct {
		name   string
		media  ast.PdfArray
		crop   ast.PdfArray
		dx, dy float64
	}{
		{"corners swapped", box(0, 0, 700, 900), box(650, 830, 38, 38), 38, 38},
		{"a media box of three entries", box(10, 10, 300), nil, 0, 0},
		{"a media box of no width", box(5, 5, 5, 800), nil, 0, 0},
		{"a crop box of no width", box(20, 20, 620, 820), box(50, 50, 50, 400), 20, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := &document.Page{}
			page.SetBoxes(tc.media, tc.crop, nil, nil, nil)
			dx, dy, _ := stampOriginShift(page)
			if dx != tc.dx || dy != tc.dy {
				t.Errorf("shift = (%v, %v), want (%v, %v)", dx, dy, tc.dx, tc.dy)
			}
		})
	}
}

// TestStampOriginShiftIgnoresMalformedBoxes checks a box litho cannot read as
// numbers is left alone rather than shifting by a guess.
func TestStampOriginShiftIgnoresMalformedBoxes(t *testing.T) {
	for _, tc := range []struct {
		name string
		page *document.Page
	}{
		{"no boxes", &document.Page{}},
		{"too short", &document.Page{MediaBox: ast.PdfArray{ast.PdfInt(0)}}},
		{"non-numeric", &document.Page{MediaBox: ast.PdfArray{ast.PdfName("x"), ast.PdfInt(0)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, ok := stampOriginShift(tc.page); ok {
				t.Error("reported a shift for a box it cannot read")
			}
		})
	}
}
