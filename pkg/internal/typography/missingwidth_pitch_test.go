package typography

import (
	"bytes"
	"context"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// missingWidthPitchPDF is a page that sets each glyph by a TD of its own
// in a Courier whose /Widths covers code 0 alone and whose descriptor
// states no /MissingWidth, the shape of pdfjs bug1157493.pdf: "net:", two
// space glyphs, then "host", one glyph every pitch units.
func missingWidthPitchPDF(pitch float64) []byte {
	step := strconv.FormatFloat(pitch, 'f', -1, 64)
	var body strings.Builder
	body.WriteString("BT /F1 19.79 Tf 10 20 TD")
	for i, c := range "net:  host" {
		if i > 0 {
			body.WriteString(" " + step + " 0 TD")
		}
		body.WriteString(" (" + string(c) + ") Tj")
	}
	body.WriteString(" ET")
	s := body.String()
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Courier /FirstChar 0 /LastChar 0 /Widths [600] /FontDescriptor 7 0 R >>\nendobj\n" +
		"7 0 obj\n<< /Type /FontDescriptor /FontName /Courier /Flags 33 /FontBBox [-23 -250 715 805] /ItalicAngle 0 /Ascent 629 /Descent -157 /CapHeight 562 /StemV 51 >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 50] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(s)) + " >>\nstream\n" + s + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 8 /Root 1 0 R >>\n%%EOF\n")
}

// TestExtractText_CodesOutsideWidthsAdvanceNothing pins what any reading
// of a code outside /Widths keeps: ISO 32000-1, 9.6.2.1, Table 111 gives
// /Widths the codes /FirstChar to /LastChar only, and Table 122 gives every
// other code the descriptor's /MissingWidth, 0 when it states none. Such a
// glyph advances nothing, so each sits where its own TD sets it, and the
// gap two space glyphs leave between "net:" and "host" still reads as a
// word break. The font here embeds no program, so no gap is measured from a
// program's advance; [TestExtractText_CodesOutsideWidthsMeasuredByProgram]
// pins the glyphs joining when one is embedded.
//
// # Corpus evidence
//
//   - pdfjs bug1157493.pdf: the shape above, at a 10.8688-unit pitch
//     against the 11.874 units Courier's program gives each glyph.
func TestExtractText_CodesOutsideWidthsAdvanceNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const pitch = 10.8688
	data := missingWidthPitchPDF(pitch)
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

	elems, err := ExtractElements(ctx, doc, graphics.NewMachine([4]float64{0, 0, 200, 50}), pages[0], ops)
	if err != nil {
		t.Fatalf("ExtractElements: %v", err)
	}
	if len(elems) != 10 {
		t.Fatalf("got %d elements, want 10", len(elems))
	}
	for i, el := range elems {
		if el.Width != 0 {
			t.Errorf("element %d (%q): width %v, want 0 (no /MissingWidth)", i, el.Text, el.Width)
		}
		if want := 10 + float64(i)*pitch; math.Abs(el.X-want) > 1e-6 {
			t.Errorf("element %d (%q): x %v, want %v (its own TD, no advance)", i, el.Text, el.X, want)
		}
	}

	got, err := ExtractText(ctx, doc, graphics.NewMachine([4]float64{0, 0, 200, 50}), pages[0], ops, ExtractionConfig{})
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !regexp.MustCompile(`:\s+h`).MatchString(got) {
		t.Errorf("got %q, want a word break between \"net:\" and \"host\"", got)
	}
	if stripped := strings.Join(strings.Fields(got), ""); stripped != "net:host" {
		t.Errorf("got %q, want the characters net:host", got)
	}
}
