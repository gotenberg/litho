package typography

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/models"
)

// TestReconstructText_BaselineDriftInsideRunKeepsLine pins that a baseline
// shift smaller than a quarter of the font size continues the run, so it
// does not break the line even past the 2-unit tolerance: PDF.js keeps such
// a glyph in the same text item.
func TestReconstructText_BaselineDriftInsideRunKeepsLine(t *testing.T) {
	t.Parallel()
	in := []models.TextElement{
		{Text: "a", X: 100, Y: 500, Width: 10, FontSize: 20},
		{Text: "b", X: 110, Y: 503, Width: 10, FontSize: 20},
	}
	if got := ReconstructText(context.Background(), in, nil); got != "ab" {
		t.Errorf("got %q, want %q", got, "ab")
	}
}

// TestReconstructText_SuperscriptAgainstRunStart pins that a superscript in
// a smaller size starts a run, and breaks the line only when its baseline
// sits more than the tolerance from the start of the run before it.
func TestReconstructText_SuperscriptAgainstRunStart(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		y    float64
		want string
	}{
		{"within tolerance", 501.5, "x2"},
		{"beyond tolerance", 504, "x\n2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := []models.TextElement{
				{Text: "x", X: 100, Y: 500, Width: 5, FontSize: 10},
				{Text: "2", X: 105, Y: tc.y, Width: 3, FontSize: 6},
			}
			if got := ReconstructText(context.Background(), in, nil); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestReconstructText_FontChangeStartsRun pins that a change of font
// starts a run even at the same size. The bullet sits a third of a unit
// above the text that follows it in another font, so the run of that text
// starts at its own baseline, and the superscript 2.0001 units above it
// breaks the line, as PDF.js breaks it.
//
// # Corpus evidence
//
//   - pdfjs issue20668.pdf: "1200" then the footnote mark "11", after a
//     bullet set in a third font.
func TestReconstructText_FontChangeStartsRun(t *testing.T) {
	t.Parallel()
	bullet, body, mark := &Font{}, &Font{}, &Font{}
	in := []models.TextElement{
		{Text: "•", X: 382, Y: 428.567, Width: 5, FontSize: 8},
		{Text: "G", X: 387, Y: 428.243, Width: 6, FontSize: 8},
		{Text: "0", X: 393, Y: 428.243, Width: 4, FontSize: 8},
		{Text: "1", X: 397, Y: 430.2431, Width: 2, FontSize: 4},
	}
	fonts := []runFont{
		{name: "F7", font: bullet, size: 8},
		{name: "F6", font: body, size: 8},
		{name: "F6", font: body, size: 8},
		{name: "F5", font: mark, size: 4},
	}
	if got := reconstructText(context.Background(), in, fonts, nil); got != "•G0\n1" {
		t.Errorf("got %q, want %q", got, "•G0\n1")
	}
}

// TestReconstructText_SizeIsTheTfOperand pins that the size whose change
// starts a run is the one Tf selected. Two glyphs at the same Tf size whose
// text matrix scales them differently continue one run, as in PDF.js, so a
// small baseline shift between them does not break the line. Without the
// fonts, the size on the page is all reconstruction can compare.
func TestReconstructText_SizeIsTheTfOperand(t *testing.T) {
	t.Parallel()
	in := []models.TextElement{
		{Text: "a", X: 100, Y: 500, Width: 5, FontSize: 10},
		{Text: "b", X: 105, Y: 502.4, Width: 5, FontSize: 12.5},
	}
	f := &Font{}
	fonts := []runFont{{name: "F1", font: f, size: 1}, {name: "F1", font: f, size: 1}}
	if got := reconstructText(context.Background(), in, fonts, nil); got != "ab" {
		t.Errorf("with fonts: got %q, want %q", got, "ab")
	}
	if got := ReconstructText(context.Background(), in, nil); got != "a\nb" {
		t.Errorf("without fonts: got %q, want %q", got, "a\nb")
	}
}

// runsPDF shows text on the page in two fonts, then a form that shows text
// in a font of its own resources. Ts lifts a superscript on the page.
func runsPDF() []byte {
	form := "BT /F1 12 Tf 72 600 Td (Z) Tj ET"
	body := "BT /F1 10 Tf 72 700 Td (E=mc) Tj /F1 6 Tf 4 Ts (2) Tj 0 Ts /F2 10 Tf ( holds) Tj ET /Fm1 Do"
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"9 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Times-Roman >>\nendobj\n" +
		"10 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R /F2 9 0 R >> /XObject << /Fm1 7 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 612 792] " +
		"/Resources << /Font << /F1 10 0 R >> >> /Length " + strconv.Itoa(len(form)) +
		" >>\nstream\n" + form + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 11 /Root 1 0 R >>\n%%EOF\n")
}

func runsPage(t *testing.T) (*document.Document, *document.Page, []content.Operation) {
	t.Helper()
	ctx := context.Background()
	data := runsPDF()
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
	return doc, pages[0], ops
}

// TestExtractElements_RunFontsPairWithElements pins that extraction names
// one run font per element, a form's glyphs included, and that a form's
// glyph is named by the font of the form's own resources.
func TestExtractElements_RunFontsPairWithElements(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, page, ops := runsPage(t)

	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	ex, err := extractElements(ctx, doc, machine, page, ops, extractionRecord{runFonts: true})
	if err != nil {
		t.Fatalf("extractElements: %v", err)
	}
	elements, fonts := ex.elements, ex.runFonts
	if len(fonts) != len(elements) || len(elements) == 0 {
		t.Fatalf("got %d run fonts for %d elements, want one each", len(fonts), len(elements))
	}
	first, last := fonts[0], fonts[len(fonts)-1]
	if elements[len(elements)-1].Text != "Z" {
		t.Fatalf("last element %q, want the form's Z", elements[len(elements)-1].Text)
	}
	if first.name != "F1" || first.size != 10 {
		t.Errorf("first glyph font = %q at %v, want F1 at 10", first.name, first.size)
	}
	if last.name != "F1" || last.size != 12 || last.font == first.font {
		t.Errorf("form glyph font = %q at %v (same font as the page's F1: %v), want the form's own F1 at 12",
			last.name, last.size, last.font == first.font)
	}
}

// TestExtractText_TextRiseSuperscript pins the two halves end to end: Ts
// lifts the superscript 4 units, past the line tolerance, so it reads as a
// word of its own, as PDF.js reads it.
//
// # Corpus evidence
//
//   - govdocs thread_000_000578.pdf: footnote marks set with 4.9 Ts read
//     glued to the word before them.
func TestExtractText_TextRiseSuperscript(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, page, ops := runsPage(t)

	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	got, err := ExtractText(ctx, doc, machine, page, ops, ExtractionConfig{})
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if words := strings.Fields(got); len(words) < 3 || words[0] != "E=mc" || words[1] != "2" || words[2] != "holds" {
		t.Errorf("got %q, want the words E=mc, 2, holds", got)
	}
}
