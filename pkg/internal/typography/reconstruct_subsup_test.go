package typography

import (
	"bytes"
	"context"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/semantics"
	"github.com/gotenberg/litho/pkg/models"
)

// subsupPDF builds a one-page document whose page shows body, with /F1
// naming Helvetica and fonts adding further font resources, each a name
// and the dictionary it names. objs are further indirect objects, from
// object 10 on.
func subsupPDF(body, fonts string, objs ...string) []byte {
	var b strings.Builder
	b.WriteString("%PDF-1.7\n")
	b.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	b.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	b.WriteString("4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")
	b.WriteString("5 0 obj\n<< /Font << /F1 4 0 R " + fonts + " >> >>\nendobj\n")
	b.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n")
	b.WriteString("6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n")
	for i, o := range objs {
		b.WriteString(strconv.Itoa(10+i) + " 0 obj\n" + o + "\nendobj\n")
	}
	b.WriteString("trailer\n<< /Size " + strconv.Itoa(10+len(objs)) + " /Root 1 0 R >>\n%%EOF\n")
	return []byte(b.String())
}

// subsupExtract returns the text litho extracts from the page of data.
func subsupExtract(t *testing.T, data []byte) string {
	t.Helper()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	ops, err := content.ParsePageContent(ctx, doc, pages[0].Dict)
	if err != nil {
		t.Fatalf("ParsePageContent: %v", err)
	}
	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	got, err := ExtractText(ctx, doc, machine, pages[0], ops, ExtractionConfig{})
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	return got
}

// TestExtractText_MarkedContentEndsRun pins that a marked-content operator
// ends the run of the glyph before it, whether or not the sequence carries
// an MCID, as it ends PDF.js's text item. The subscript "X" is shown by the
// Tf of "NO" through a smaller text matrix, 2.4 units below the baseline:
// less than a quarter of the size of "NO", so only the BDC before it
// starts a run, and the run breaks the line past the 2-unit tolerance.
// Without the BDC the subscript continues the run of "NO".
//
// # Corpus evidence
//
//   - govdocs thread_000_000282.pdf: "NOX" and "SO2" in Touch-Up_Line
//     sequences, read "NOX" where PDF.js reads "NO X".
func TestExtractText_MarkedContentEndsRun(t *testing.T) {
	t.Parallel()
	// Helvetica sets "NO" 722 + 778 units wide, 18 units at 12 points.
	const noRun = "BT /F1 1 Tf 12 0 0 12 72 700 Tm (NO) Tj ET "
	for _, tc := range []struct {
		name, body, want string
	}{
		{"marked content", noRun + "/Span << >> BDC BT /F1 1 Tf 5 0 0 5.76 90 697.6 Tm (X) Tj ET EMC", "NO\nX"},
		{"no marked content", noRun + "BT /F1 1 Tf 5 0 0 5.76 90 697.6 Tm (X) Tj ET", "NOX"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := subsupExtract(t, subsupPDF(tc.body, "")); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestReconstructText_RunGapAtItemScale pins that the move back that
// starts a run is measured at the scale the text matrix gives the glyph
// after it. "1" is shown in the Tf of "f" through a smaller text matrix,
// 3.24 units back: more than a fifth of its own size, so it starts a run,
// and the run breaks the line 3.67 units below. Against the size of "f"
// the move would be too small to start one.
func TestReconstructText_RunGapAtItemScale(t *testing.T) {
	t.Parallel()
	f := &Font{}
	in := []models.TextElement{
		{Text: "f", X: 150.48, Y: 100.3, Width: 9.9, FontSize: 18},
		{Text: "1", X: 157.14, Y: 96.63, Width: 7.61, FontSize: 13.14},
	}
	fonts := []runFont{
		{name: "C2_0", font: f, size: 1, height: 18},
		{name: "C2_0", font: f, size: 1, height: 13.14},
	}
	if got := reconstructText(context.Background(), in, fonts, nil); got != "f\n1" {
		t.Errorf("got %q, want %q", got, "f\n1")
	}
}

// TestReconstructText_RotatedLinePastSpaceGlyph pins that a line set a
// quarter turn round reads past the space glyphs shown between its
// glyphs, wherever those sit: a space set upright at the top of the page
// between "(" and "A" neither breaks the line nor anchors the next glyph.
//
// # Corpus evidence
//
//   - govdocs thread_300_300493.pdf pages 6 and 8: "(A)" in rotated figure
//     captions read "( A )".
func TestReconstructText_RotatedLinePastSpaceGlyph(t *testing.T) {
	t.Parallel()
	quarter := math.Pi / 2
	in := []models.TextElement{
		{Text: "(", X: 539.69, Y: 381.53, Width: 2.5, FontSize: 9, Angle: quarter},
		{Text: " ", X: 203, Y: 761, Width: 2, FontSize: 9},
		{Text: "A", X: 539.69, Y: 383.99, Width: 5.5, FontSize: 9, Angle: quarter},
		{Text: " ", X: 205, Y: 761, Width: 2, FontSize: 9},
		{Text: ")", X: 539.69, Y: 389.47, Width: 2.5, FontSize: 9, Angle: quarter},
	}
	if got := ReconstructText(context.Background(), in, nil); got != "(A)" {
		t.Errorf("got %q, want %q", got, "(A)")
	}
}

// type3PDF shows body on a page whose /T3 is a Type 3 font with an empty
// /FontBBox and an identity /FontMatrix, as a drawing program writes one:
// four glyphs 21 units wide whose paths run 32 units high.
func type3PDF(body string) []byte {
	proc := "21 0 d0 0 0 m 10 32 l S"
	stream := "<< /Length " + strconv.Itoa(len(proc)) + " >>\nstream\n" + proc + "\nendstream"
	font := "<< /Type /Font /Subtype /Type3 /FontBBox [0 0 0 0] /FontMatrix [1 0 0 1 0 0] " +
		"/CharProcs << /A 11 0 R /B 11 0 R /C 11 0 R /D 11 0 R >> " +
		"/Encoding << /Type /Encoding /Differences [65 /A /B /C /D] >> " +
		"/FirstChar 65 /LastChar 68 /Widths [21 21 21 21] >>"
	return subsupPDF(body, "/T3 10 0 R", font, stream)
}

// TestExtractText_Type3HeightFromGlyphBounds pins that a Type 3 font with
// an empty /FontBBox is sized across its line by the bounds of its glyphs'
// paths, mapped by its /FontMatrix, as PDF.js sizes it. Shown at 0.43 units
// on the page, "CD" jumps back to the left of "AB" 0.36 units lower: more
// than half of 0.43, which would end the line, but well within the 13.8
// units the 32-unit glyphs stand on the page.
//
// # Corpus evidence
//
//   - pdfjs issue19624.pdf page 2: "AMINESERVICE" read "AMINE SERVICE".
func TestExtractText_Type3HeightFromGlyphBounds(t *testing.T) {
	t.Parallel()
	body := "BT /T3 1.29 Tf 0.33 0 0 0.33 100 500 Tm (AB) Tj 0.33 0 0 0.33 50 500.36 Tm (CD) Tj ET"
	if got := subsupExtract(t, type3PDF(body)); got != "ABCD" {
		t.Errorf("got %q, want %q", got, "ABCD")
	}
}

// TestType3PathBounds pins that the bounds of a glyph's paths take a Bezier
// segment by its extent, not by its control points, and that a description
// that constructs no path has none.
func TestType3PathBounds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		data string
		want *[4]float64
	}{
		{"curve", "0 0 m 0 10 10 10 10 0 c f", &[4]float64{0, 0, 10, 7.5}},
		{"lines and rectangle", "5 0 m 6 1 l S -1 -2 3 4 re f", &[4]float64{-1, -2, 6, 2}},
		{"no path", "1 0 0 rg", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ops, err := content.ParseStreamData(ctx, []byte(tc.data), content.SplitGluedOperators())
			if err != nil {
				t.Fatalf("ParseStreamData: %v", err)
			}
			got := type3PathBounds(ops)
			if (got == nil) != (tc.want == nil) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			if got == nil {
				return
			}
			for i := range got {
				if math.Abs(got[i]-tc.want[i]) > 1e-9 {
					t.Fatalf("got %v, want %v", *got, *tc.want)
				}
			}
		})
	}
}

// TestReconstructText_BeadReadsContentOrder pins that the text inside one
// bead reads in content-stream order, its lines broken as on a page without
// beads, and that the beads follow one another in thread order. The
// superscript "1" sits 4.8 units above the name before it, so it ends that
// line; the second bead, listed first, comes first.
//
// # Corpus evidence
//
//   - govdocs thread_200_200417.pdf page 1: "Xu 1 ," read "Xu1,".
func TestReconstructText_BeadReadsContentOrder(t *testing.T) {
	t.Parallel()
	in := []models.TextElement{
		{Text: "X", X: 180, Y: 535.84, Width: 8, FontSize: 12},
		{Text: "u", X: 188, Y: 535.84, Width: 6, FontSize: 12},
		{Text: "1", X: 195.6, Y: 540.64, Width: 4.8, FontSize: 9.6},
		{Text: ",", X: 200.4, Y: 535.84, Width: 3, FontSize: 12},
		{Text: "T", X: 72, Y: 100, Width: 7, FontSize: 12},
	}
	beads := []semantics.Bead{
		{Rect: [4]float64{50, 50, 300, 150}},
		{Rect: [4]float64{50, 500, 300, 600}},
	}
	if got, want := ReconstructText(context.Background(), in, beads), "T\n\nXu\n1\n,"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestExtractText_SkewedMatrixEndsLine pins that a glyph whose text
// rendering matrix is not set along the page axes is read across the line as
// PDF.js reads it, each position taken through the inverse of its own
// matrix as if the matrix were a rotation. A subscript "d" set by an oblique
// text matrix 1.56 units below "Re" then ends the line of "Re", and breaks it,
// sitting more than a tenth of the size of "Re" off its baseline. The same
// glyph on the baseline of "Re" continues its line. A line turned 0.57°
// round and scaled across reads a jump back to its margin along its own
// baseline, 0.3 units off it, where the page Y axis reads 2.42.
//
// # Corpus evidence
//
//   - govdocs thread_200_200087.pdf: "Reδ" and "Uδ", each δ an oblique
//     Symbol subscript, where PDF.js reads "Re δ" and "U δ".
//   - pdfjs JBIG2Globals.pdf: "tyrannidemdinavit", a margin note set on the
//     turned baseline of the line before it, as PDF.js reads it.
func TestExtractText_SkewedMatrixEndsLine(t *testing.T) {
	t.Parallel()
	// Helvetica sets "Re" 722 + 556 units wide, 12.73 units at 9.96 points.
	for _, tc := range []struct {
		name, body, want string
	}{
		{"skewed subscript", "BT /F1 1 Tf 9.96 0 0 9.96 72 422.36 Tm (Re) Tj 6.48 0 2.2163 6.48 84.73 420.8 Tm (d) Tj 9.96 0 0 9.96 108 422.36 Tm (Reynolds) Tj ET", "Re\nd Reynolds"},
		{"skewed on the baseline", "BT /F1 1 Tf 9.96 0 0 9.96 72 422.36 Tm (Re) Tj 9.96 0 2.2163 9.96 84.73 422.36 Tm (d) Tj ET", "Red"},
		{"upright subscript", "BT /F1 1 Tf 9.96 0 0 9.96 72 422.36 Tm (Re) Tj 6.48 0 0 6.48 84.73 420.8 Tm (d) Tj ET", "Red"},
		{"turned and scaled", "BT /F1 1 Tf 9.02 0.0902 -0.11 11 239.1 680.11 Tm (tyrannidem) Tj 6.17 0.0617 -0.07 7 26.71 677.69 Tm (dinavit) Tj ET", "tyrannidemdinavit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := subsupExtract(t, subsupPDF(tc.body, "")); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
