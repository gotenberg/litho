package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestCMapUnicodeTransformFormatIsNotPDF14 pins that ISO 19005-1 predefines no
// UTF-8, UTF-16 or UTF-32 CMap. Part 1 is written against PDF 1.4 and
// ISO 32000-1, Table 118 marks that whole family a PDF 1.5 addition, so under
// part 1 such a name names no predefined CMap: it supplies no CIDSystemInfo, and
// §6.3.3.1 t1 cannot establish the Registry/Ordering relationship it demands.
//
// Measured against veraPDF at PDF/A-1a, one fixture per character collection
// holding the CIDFont at Adobe-<collection> throughout: UniGB, UniJIS, UniKS and
// UniCNS in -UTF16-, -UTF8- and -UTF32- forms all report, while the UCS2, EUC,
// RKSJ and GBK names of the same collections pass. Parts 2 and later predefine
// the whole family and report none of them. pdfjs issue18466.pdf carries
// UniGB-UTF16-H over Adobe-GB1; eight pdfjs files share the shape.
func TestCMapUnicodeTransformFormatIsNotPDF14(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	build := func(t *testing.T, encoding, ordering string) (*document.Document, ast.PdfDict, ast.PdfDict) {
		t.Helper()

		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog >>",
			"<< /Type /Font /Subtype /Type0 /BaseFont /LithoCJK /Encoding /"+encoding+
				" /DescendantFonts [3 0 R] >>",
			"<< /Type /Font /Subtype /CIDFontType0 /BaseFont /LithoCJK"+
				" /CIDSystemInfo << /Registry (Adobe) /Ordering ("+ordering+") /Supplement 2 >>"+
				" /FontDescriptor 4 0 R >>",
			"<< /Type /FontDescriptor /FontName /LithoCJK /Flags 4 >>",
		)
		fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil {
			t.Fatalf("resolve font: %v", err)
		}
		cidFont, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
		if err != nil {
			t.Fatalf("resolve CIDFont: %v", err)
		}
		return doc, fontDict, cidFont
	}

	cases := []struct {
		encoding string
		ordering string
		profile  string
		fires    bool
	}{
		{"UniGB-UTF16-H", "GB1", "PDF/A-1b", true},
		{"UniJIS-UTF16-H", "Japan1", "PDF/A-1b", true},
		{"UniKS-UTF16-H", "Korea1", "PDF/A-1b", true},
		{"UniCNS-UTF16-H", "CNS1", "PDF/A-1b", true},
		{"UniGB-UTF8-H", "GB1", "PDF/A-1b", true},
		{"UniGB-UTF32-H", "GB1", "PDF/A-1b", true},

		// The same collections through their PDF 1.4 names stay silent.
		{"UniGB-UCS2-H", "GB1", "PDF/A-1b", false},
		{"GBK-EUC-H", "GB1", "PDF/A-1b", false},
		{"UniJIS-UCS2-HW-H", "Japan1", "PDF/A-1b", false},
		{"90ms-RKSJ-H", "Japan1", "PDF/A-1b", false},

		// Parts 2 and later predefine the family.
		{"UniGB-UTF16-H", "GB1", "PDF/A-2b", false},
		{"UniGB-UTF16-H", "GB1", "PDF/A-4", false},
	}
	for _, tc := range cases {
		t.Run(tc.profile+"/"+tc.encoding, func(t *testing.T) {
			t.Parallel()

			doc, fontDict, cidFont := build(t, tc.encoding, tc.ordering)
			got := checkCIDSystemInfo(ctx, doc, fontDict, cidFont, "F1", 1, tc.profile)
			if fired := len(got) != 0; fired != tc.fires {
				t.Errorf("%s with %s: fired = %v (%v), want %v",
					tc.profile, tc.encoding, fired, got, tc.fires)
			}
		})
	}
}
