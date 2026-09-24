package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestCIDToGIDMapEscapeDiffersByPart pins that the escape ISO 19005-1 grants is
// not the one parts 2 to 4 grant, which litho had conflated.
//
// ISO 19005-2/3 §6.2.11.3.2 t1 and ISO 19005-4 §6.2.10.3.2 t1 read
// `Subtype != "CIDFontType2" || CIDToGIDMap != null || containsFontFile == false`:
// a CIDFontType2 with no embedded program has no glyph space to map into.
// ISO 19005-1 §6.3.3.2 t1 reads `... || renderingMode == 3` and grants no
// embedding escape at all.
//
// # Corpus evidence
//
//   - pdfjs issue7835.pdf, issue18466.pdf: each shows an unembedded
//     CIDFontType2 with no /CIDToGIDMap, and veraPDF reports both at PDF/A-1a
//     while litho reported neither.
//   - govdocs thread_400_400765.pdf: shows an unembedded CIDFontType2 with no
//     /CIDToGIDMap, and veraPDF reports it at PDF/A-1a while litho did not.
func TestCIDToGIDMapEscapeDiffersByPart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// build returns the CIDFont dictionary of a Type 0 font whose descendant
	// carries a font descriptor but, when embedded is false, no program.
	build := func(t *testing.T, embedded bool) (*document.Document, ast.PdfDict) {
		t.Helper()

		// A real sfnt: litho reads the program before calling a font embedded,
		// so a stub stream would be indistinguishable from no program at all.
		program := nonSymbolicTTF()
		descriptor := "<< /Type /FontDescriptor /FontName /LithoCID /Flags 4 >>"
		if embedded {
			descriptor = "<< /Type /FontDescriptor /FontName /LithoCID /Flags 4 /FontFile2 5 0 R >>"
		}
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog >>",
			"<< /Type /Font /Subtype /Type0 /BaseFont /LithoCID /Encoding /Identity-H"+
				" /DescendantFonts [3 0 R] >>",
			"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /LithoCID"+
				" /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >>"+
				" /FontDescriptor 4 0 R >>",
			descriptor,
			fmt.Sprintf("<< /Length %d /Length1 %d >>\nstream\n%s\nendstream",
				len(program), len(program), program),
		)
		cidFont, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
		if err != nil {
			t.Fatalf("resolve CIDFont: %v", err)
		}
		return doc, cidFont
	}

	cases := []struct {
		name     string
		profile  string
		embedded bool
		visible  bool
		fires    bool
	}{
		{"part 1 judges an unembedded font", "PDF/A-1b", false, true, true},
		{"part 1 judges an embedded font", "PDF/A-1b", true, true, true},
		{"part 1 exempts an invisible font", "PDF/A-1b", false, false, false},
		{"part 2 exempts an unembedded font", "PDF/A-2b", false, true, false},
		{"part 2 judges an embedded font", "PDF/A-2b", true, true, true},
		{"part 4 exempts an unembedded font", "PDF/A-4", false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, cidFont := build(t, tc.embedded)
			got := checkCIDToGIDMap(ctx, doc, cidFont, "CIDFontType2", "F1", 1, tc.profile, tc.visible)
			if fired := len(got) != 0; fired != tc.fires {
				t.Errorf("%s: fired = %v (%v), want %v", tc.profile, fired, got, tc.fires)
			}
		})
	}
}
