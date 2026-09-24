package remediation

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
)

// toUnicodeRangesPDF builds a page using a Type0 font (object 4) whose
// /ToUnicode (object 5) holds the given bfrange groups.
func toUnicodeRangesPDF(groups string) []byte {
	cmapData := "/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
		"/CMapName /Test def\n/CMapType 2 def\n" +
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n" +
		groups +
		"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n"
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /MyriadArabic-Regular /Encoding /Identity-H" +
			" /ToUnicode 5 0 R /DescendantFonts [6 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmapData), cmapData),
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /MyriadArabic-Regular /DW 1000 >>",
	})
}

// runToUnicodeRanges runs the registered repair twice over the document and
// returns the allocator with both passes' fixes.
func runToUnicodeRanges(t *testing.T, data []byte) (*writer.ObjAllocator, []string, []string) {
	t.Helper()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	first, err := fixToUnicodeRangeIssues(ctx, alloc, doc, Options{}, nil, "")
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	second, err := fixToUnicodeRangeIssues(ctx, alloc, doc, Options{}, nil, "")
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	return alloc, first, second
}

// TestFixToUnicodeRangesSplitsACrossingRange pins the repair of a /ToUnicode
// that maps several glyphs through a single entry crossing a first-byte
// boundary (Adobe Technical Note #5411, 1.4.1), so veraPDF credits none of them
// (ISO 14289-2, 8.4.5.8). The CMap is rewritten in place and still maps every
// code it mapped.
//
// # Corpus evidence
//
//   - pdfjs issue5751.pdf: its /ToUnicode maps c, e, n, p and s through the
//     single entry <01ED> <0206> <0061>.
func TestFixToUnicodeRangesSplitsACrossingRange(t *testing.T) {
	t.Parallel()

	alloc, fixes, again := runToUnicodeRanges(t, toUnicodeRangesPDF("1 beginbfrange\n<01ED> <0206> <0061>\nendbfrange\n"))
	if len(fixes) != 1 {
		t.Fatalf("fixes = %v, want one", fixes)
	}
	if len(again) != 0 {
		t.Errorf("a second pass over the same CMap reported %v, want nothing", again)
	}

	stream, ok := alloc.Pending()[5].(ast.PdfStream)
	if !ok {
		t.Fatalf("object 5 is %T, want the rewritten stream in place", alloc.Pending()[5])
	}
	raw, err := stream.Bytes(context.Background())
	if err != nil {
		t.Fatalf("reading the CMap: %v", err)
	}
	parsed, err := cmap.Parse(context.Background(), raw)
	if err != nil {
		t.Fatalf("parsing the rewrite: %v", err)
	}
	strict := parsed.StrictMapping()
	if !maps.Equal(strict, parsed.Mapping) {
		t.Errorf("the rewrite still holds a range a strict reader drops:\n%s", raw)
	}
	for code, want := range map[string]string{"\x01\xEF": "c", "\x01\xFF": "s", "\x02\x06": "z"} {
		if got := strict[code]; got != want {
			t.Errorf("code %X maps to %q, want %q", code, got, want)
		}
	}
}

// TestFixToUnicodeRangesLeavesAConformingCMap guards the repair against
// rewriting a CMap a strict reader already reads whole.
func TestFixToUnicodeRangesLeavesAConformingCMap(t *testing.T) {
	t.Parallel()

	alloc, fixes, _ := runToUnicodeRanges(t, toUnicodeRangesPDF(
		"2 beginbfrange\n<01ED> <01FF> <0061>\n<0200> <0206> <0074>\nendbfrange\n"))
	if len(fixes) != 0 {
		t.Errorf("fixes = %v, want none", fixes)
	}
	if _, ok := alloc.Pending()[5]; ok {
		t.Error("a conforming CMap was rewritten")
	}
}

// TestMatchToUnicodeRangesClaimsThePerGlyphRules pins that the repair is
// offered for every profile's per-glyph Unicode rule, and only for those.
func TestMatchToUnicodeRangesClaimsThePerGlyphRules(t *testing.T) {
	t.Parallel()

	for _, clause := range []string{"6.3.8", "6.2.11.7.2", "7.21.7", "8.4.5.8"} {
		v := compliance.Violation{Rule: compliance.Rule{Clause: clause, TestNumber: 1}}
		if !matchToUnicodeRanges(v) {
			t.Errorf("clause %s is not claimed by the ToUnicode range repair", clause)
		}
	}
	for _, clause := range []string{"6.2.11.7.3", "8.4.5.6", "6.2.11.5"} {
		v := compliance.Violation{Rule: compliance.Rule{Clause: clause, TestNumber: 1}}
		if matchToUnicodeRanges(v) {
			t.Errorf("clause %s is claimed by the ToUnicode range repair", clause)
		}
	}
}
