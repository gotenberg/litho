package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestType0ToUnicodeCJKExemptionNeedsAPDF14CMap pins that the CJK
// character-collection exemption from /ToUnicode lapses under part 1 when the
// font reaches its collection through a CMap the profile does not define.
//
// ISO 19005-1 §6.3.8 exempts "Type 0 fonts whose descendant CIDFont uses the
// Adobe-GB1, Adobe-CNS1, Adobe-Japan1 or Adobe-Korea1 character collections",
// and the Unicode value it counts on comes from the CMap that reaches the
// collection. Part 1 is written against PDF 1.4, which predefines no UTF-8,
// UTF-16 or UTF-32 CMap (see [cmapNameIsUnicodeTransformFormat]), so with one of
// those names no Unicode value follows and the exemption cannot hold.
//
// Measured against veraPDF at PDF/A-1a over an Adobe-GB1 CIDFont carrying no
// /ToUnicode: GBK-EUC-H and UniGB-UCS2-H report nothing, UniGB-UTF16-H reports
// the glyph. pdfjs issue18466.pdf is the corpus case (16 glyphs of a
// FZSSK--GBK1-0 encoded UniGB-UTF16-H); seven pdfjs files share the shape.
func TestType0ToUnicodeCJKExemptionNeedsAPDF14CMap(t *testing.T) {
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
		type0, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil {
			t.Fatalf("resolve Type 0 font: %v", err)
		}
		cidFont, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
		if err != nil {
			t.Fatalf("resolve CIDFont: %v", err)
		}
		return doc, type0, cidFont
	}

	cases := []struct {
		encoding string
		ordering string
		profile  string
		fires    bool
	}{
		{"UniGB-UTF16-H", "GB1", "PDF/A-1a", true},
		{"UniJIS-UTF16-H", "Japan1", "PDF/A-1a", true},
		{"UniKS-UTF8-H", "Korea1", "PDF/A-1a", true},

		// The same collections through a CMap PDF 1.4 predefines keep it.
		{"GBK-EUC-H", "GB1", "PDF/A-1a", false},
		{"UniGB-UCS2-H", "GB1", "PDF/A-1a", false},
		{"90ms-RKSJ-H", "Japan1", "PDF/A-1a", false},

		// Parts 2 and later predefine the whole family, so the exemption holds.
		{"UniGB-UTF16-H", "GB1", "PDF/A-2u", false},
		{"UniGB-UTF16-H", "GB1", "PDF/A-3u", false},
	}
	for _, tc := range cases {
		t.Run(tc.profile+"/"+tc.encoding, func(t *testing.T) {
			t.Parallel()

			doc, type0, cidFont := build(t, tc.encoding, tc.ordering)
			usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{32: true}}
			got := checkType0ToUnicode(ctx, doc, type0, cidFont, "F1", 1, tc.profile, usage)
			if fired := len(got) != 0; fired != tc.fires {
				t.Errorf("%s with %s over Adobe-%s: fired = %v (%v), want %v",
					tc.profile, tc.encoding, tc.ordering, fired, got, tc.fires)
			}
		})
	}
}
