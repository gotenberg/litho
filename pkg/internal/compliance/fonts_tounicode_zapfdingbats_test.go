package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestToUnicodeZapfDingbatsPart1 pins that a ZapfDingbats glyph earns no
// name-based exemption from the PDF/A-1 /ToUnicode requirement, and that parts 2
// and later keep theirs.
//
// Its glyphs are named a1 to a191. ISO 32000-1 §9.10.2 derives a Unicode value
// from "the Adobe standard Latin character set or the set of named characters in
// the Symbol font" and names no third set, so those names carry none. The Adobe
// Glyph List does map them, which is why [glyphNameHasUnicode] answers true and
// every name survey in [checkToUnicode] passed such a font.
//
// The part split is measured, not assumed: veraPDF runs the same test
// (`toUnicode != null` on a Glyph) in both profiles, yet reports such a glyph
// at PDF/A-1a and passes it at PDF/A-2u.
//
// # Corpus evidence
//
//   - govdocs thread_100_100601.pdf: veraPDF reports the /a73 glyph at
//     PDF/A-1a and passes it at PDF/A-2u.
//   - All corpora: extending the carve-out past part 1 reported four files
//     veraPDF passes.
func TestToUnicodeZapfDingbatsPart1(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	build := func(t *testing.T, baseFont string) (*document.Document, ast.PdfDict) {
		t.Helper()

		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog >>",
			"<< /Type /Font /Subtype /Type1 /BaseFont /"+baseFont+
				" /FirstChar 1 /LastChar 1 /Widths [788] /Encoding 3 0 R /FontDescriptor 4 0 R >>",
			"<< /Type /Encoding /Differences [1 /a73] >>",
			// Flags bit 3 (value 4) marks the font symbolic.
			"<< /Type /FontDescriptor /FontName /"+baseFont+" /Flags 4 >>",
		)
		fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil {
			t.Fatalf("resolve font: %v", err)
		}
		return doc, fontDict
	}
	usage := func() *fontUsageInfo {
		return &fontUsageInfo{visible: true, charCodes: map[byte]bool{1: true}}
	}

	cases := []struct {
		name     string
		baseFont string
		profile  string
		fires    bool
	}{
		{"an embedded subset under part 1", "MHHIBN+ZapfDingbats", "PDF/A-1a", true},
		{"the plain spelling under part 1", "ZapfDingbats", "PDF/A-1a", true},
		{"part 2 keeps the exemption", "MHHIBN+ZapfDingbats", "PDF/A-2u", false},
		{"part 3 keeps the exemption", "MHHIBN+ZapfDingbats", "PDF/A-3u", false},
		// a73 is no name of the Symbol font either, and veraPDF reports it
		// there too, measured at PDF/A-1a on this font, which embeds no
		// program; at PDF/A-2u it passes.
		{"another symbolic font naming a73 under part 1", "MHHIBN+Symbol", "PDF/A-1a", true},
		{"another symbolic font naming a73 under part 2", "MHHIBN+Symbol", "PDF/A-2u", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, fontDict := build(t, tc.baseFont)
			got := checkToUnicode(ctx, doc, fontDict, "F1", 1, tc.profile, usage())
			if fired := len(got) != 0; fired != tc.fires {
				t.Errorf("%s %s: fired = %v (%v), want %v",
					tc.baseFont, tc.profile, fired, got, tc.fires)
			}
		})
	}
}

// TestToUnicodeDingbatsNamePart1Embedded pins that at PDF/A-1 a glyph of an
// embedded Type 1 font fails the /ToUnicode requirement by its name, not by
// the font's: ISO 19005-1, 6.3.8 admits a glyph name only where ISO 32000-1,
// 9.10.2 derives Unicode from it, the Adobe standard Latin character set or
// the Symbol font's names, and veraPDF's PDType1Font.toUnicodePDFA1 asks just
// that of each Glyph. The check used to key on a /BaseFont of ZapfDingbats, so
// it passed a73 under any other name and failed an AGL name under that one.
// Every expectation is veraPDF 1.30.2's at PDF/A-1a on the same font shape.
func TestToUnicodeDingbatsNamePart1Embedded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const tuCMap = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n" +
		"1 begincodespacerange <00> <ff> endcodespacerange\n" +
		"1 beginbfchar <01> <2714> endbfchar\n" +
		"endcmap CMapName currentdict /CMap defineresource pop end end"

	build := func(t *testing.T, subtype, baseFont, glyph string, differences, toUnicode bool, flags int) (*document.Document, ast.PdfDict) {
		t.Helper()
		enc := ""
		if differences {
			enc = " /Encoding << /Type /Encoding /Differences [1 /" + glyph + "] >>"
		}
		tu := ""
		if toUnicode {
			tu = " /ToUnicode 5 0 R"
		}
		key, program := "FontFile", type1Program("/Encoding 256 array\ndup 1 /"+glyph+" put\nreadonly def", ".notdef", glyph)
		if subtype == "TrueType" {
			key, program = "FontFile2", "not a program litho reads"
		}
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog >>",
			"<< /Type /Font /Subtype /"+subtype+" /BaseFont /"+baseFont+
				" /FirstChar 1 /LastChar 1 /Widths [600]"+enc+tu+" /FontDescriptor 3 0 R >>",
			fmt.Sprintf("<< /Type /FontDescriptor /FontName /%s /Flags %d /%s 4 0 R >>", baseFont, flags, key),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(program), program),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(tuCMap), tuCMap),
		)
		fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil {
			t.Fatalf("resolve font: %v", err)
		}
		return doc, fontDict
	}

	const symbolic, nonsymbolic = 4, 32
	cases := []struct {
		name                     string
		subtype, baseFont, glyph string
		differences, toUnicode   bool
		flags                    int
		profile                  string
		fires                    bool
	}{
		{"a73 under another name", "Type1", "XYZABC+Dingbats", "a73", true, false, symbolic, "PDF/A-1a", true},
		{"a73 under another name, nonsymbolic", "Type1", "XYZABC+Dingbats", "a73", true, false, nonsymbolic, "PDF/A-1a", true},
		{"a73 named by the program alone", "Type1", "XYZABC+Dingbats", "a73", false, false, symbolic, "PDF/A-1a", true},
		{"a73 under ZapfDingbats", "Type1", "MHHIBN+ZapfDingbats", "a73", true, false, symbolic, "PDF/A-1a", true},
		{"an AGL name under ZapfDingbats", "Type1", "MHHIBN+ZapfDingbats", "A", true, false, symbolic, "PDF/A-1a", false},
		{"a Symbol name under ZapfDingbats", "Type1", "MHHIBN+ZapfDingbats", "alpha", true, false, symbolic, "PDF/A-1a", false},
		{"an AGL name under ZapfDingbats, program only", "Type1", "MHHIBN+ZapfDingbats", "A", false, false, symbolic, "PDF/A-1a", false},
		{"a73 mapped by /ToUnicode", "Type1", "XYZABC+Dingbats", "a73", true, true, symbolic, "PDF/A-1a", false},
		{"a73 at part 2", "Type1", "XYZABC+Dingbats", "a73", true, false, symbolic, "PDF/A-2u", false},
		{"a73 through a TrueType", "TrueType", "MHHIBN+ZapfDingbats", "a73", true, false, symbolic, "PDF/A-1a", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, fontDict := build(t, tc.subtype, tc.baseFont, tc.glyph, tc.differences, tc.toUnicode, tc.flags)
			usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{1: true}}
			got := checkToUnicode(ctx, doc, fontDict, "F1", 1, tc.profile, usage)
			if fired := len(got) != 0; fired != tc.fires {
				t.Errorf("%s %s /%s at %s: fired = %v (%v), want %v",
					tc.subtype, tc.baseFont, tc.glyph, tc.profile, fired, got, tc.fires)
			}
		})
	}
}
