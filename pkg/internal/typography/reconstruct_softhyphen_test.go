package typography

import (
	"bytes"
	"context"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// softHyphenPDF shows body on one page with two fonts: /F1 Helvetica, whose
// /ToUnicode maps code 0xAD to U+00AD SOFT HYPHEN, and /F2 Times-Roman.
func softHyphenPDF(body string) []byte {
	cmap := "/CIDInit /ProcSet findresource begin 12 dict begin begincmap " +
		"/CMapName /SoftHyphen def 1 begincodespacerange <00> <FF> endcodespacerange " +
		"1 beginbfrange <20> <7E> <0020> endbfrange 1 beginbfchar <AD> <00AD> endbfchar " +
		"endcmap CMapName currentdict /CMap defineresource pop end end"
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding /ToUnicode 7 0 R >>\nendobj\n" +
		"8 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Times-Roman /Encoding /WinAnsiEncoding >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R /F2 8 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Length " + strconv.Itoa(len(cmap)) + " >>\nstream\n" + cmap + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 9 /Root 1 0 R >>\n%%EOF\n")
}

func softHyphenText(t *testing.T, body string) string {
	t.Helper()
	ctx := context.Background()
	data := softHyphenPDF(body)
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
	got, err := ExtractText(ctx, doc, graphics.NewMachine([4]float64{0, 0, 612, 792}), pages[0], ops, ExtractionConfig{})
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	return got
}

// TestExtractText_SoftHyphenAcrossFontChange pins that the glyphs after a
// soft hyphen sit its advance earlier until a text-positioning operator sets
// the position again, a Tf between them included, as PDF.js reads them: its
// text builder skips the glyph without advancing its text matrix, and Tf
// leaves that matrix alone (src/core/evaluator.js, buildTextContentItem,
// isInvisibleFormatMark, and getTextContent, OPS.setFont). Each case matches
// the text PDF.js at the pinned revision reads from the same page. The
// widths are the Standard 14 ones: Helvetica a 556 and hyphen 333,
// Times-Roman p 500.
func TestExtractText_SoftHyphenAcrossFontChange(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			// The soft hyphen's advance lies between "s" and "r": PDF.js,
			// which does not advance past it, reads no gap there.
			name: "no gap after a soft hyphen before a Tf",
			body: "BT /F1 10 Tf 72 700 Td (Cross\255) Tj /F2 10 Tf (reactivity) Tj ET",
			want: "Crossreactivity",
		},
		{
			// "res" is set by a Td where litho's position after "p" lies,
			// 3.33 units past where PDF.js reads "p" to end.
			name: "gap before a Td after a soft hyphen and a Tf",
			body: "BT /F1 10 Tf 72 700 Td (a\255) Tj /F2 10 Tf (p) Tj 13.89 0 Td (res) Tj ET",
			want: "ap res",
		},
		{
			name: "no soft hyphen, no shift",
			body: "BT /F1 10 Tf 72 700 Td (a-) Tj /F2 10 Tf (p) Tj 13.89 0 Td (res) Tj ET",
			want: "a-pres",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := softHyphenText(t, tc.body); got != tc.want {
				t.Errorf("ExtractText = %q, want %q", got, tc.want)
			}
		})
	}
}
