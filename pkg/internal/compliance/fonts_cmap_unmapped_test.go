package compliance

import (
	"fmt"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// type0CMapPDF builds a one-page PDF whose only font is a composite font with an
// embedded CIDFontType2 program. encoding names the /Encoding entry ("/Identity-H"
// or "8 0 R" for the CMap stream this builds), and cmapBody supplies that stream's
// mapping sections, so a caller can express a CMap that maps codes to CIDs, one
// that maps nothing, or one that defers to a base CMap.
//
// [type0FontPDF] cannot express this: it hardcodes /Identity-H, and the shape
// under test is an embedded /Encoding CMap stream.
func type0CMapPDF(t *testing.T, content, encoding, cmapBody string, embed bool) []byte {
	t.Helper()

	cmapStream := "/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
		"/CIDSystemInfo 3 dict dup begin /Registry (Adobe) def /Ordering (Identity) def " +
		"/Supplement 0 def end def\n/CMapName /Custom def\n/CMapType 2 def\n" +
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n" + cmapBody +
		"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n"

	descriptor := "<< /Type /FontDescriptor /FontName /Arial /Flags 32 /Ascent 700 " +
		"/Descent -200 /CapHeight 700 /StemV 80 /FontBBox [0 0 500 700] /ItalicAngle 0 "
	if embed {
		descriptor += "/FontFile2 9 0 R "
	}
	descriptor += ">>"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] " +
			"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type0 /BaseFont /Arial /Encoding " + encoding +
			" /DescendantFonts [6 0 R] >>",
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Arial /DW 1000 " +
			"/CIDToGIDMap /Identity /FontDescriptor 7 0 R " +
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> >>",
		descriptor,
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmapStream), cmapStream),
	}
	if embed {
		ttf, err := os.ReadFile("testdata/cidfont_notdef_min.ttf")
		if err != nil {
			t.Fatalf("read TTF fixture: %v", err)
		}
		objs = append(objs, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(ttf), string(ttf)))
	}

	return cyclePDF(objs...)
}

// TestUnmappedCodeResolvesToNoGlyph pins that a code an embedded /Encoding CMap
// maps nowhere reaches no glyph, not the .notdef glyph.
//
// ISO 32000-1 §9.7.6.3 sends a code no cidrange or cidchar covers to CID 0. Where
// litho holds every mapping the CMap has (no usecmap base, no notdef range), that
// code reaches nothing in the embedded program, so both the .notdef rule and the
// glyph-presence rule apply. A code written as CID 0 is different: it reaches the
// .notdef glyph, which the program does contain, and fires the .notdef rule alone.
// Both readings were measured against veraPDF 1.30.2, which reports the pair for
// an encoding CMap written with bfchar alone and the .notdef rule alone for an
// explicit <0000>.
//
// Rendering mode 3 exempts the glyph-presence half, which asks only about glyphs
// referenced for rendering; the .notdef rule carries no such exemption.
//
// PDF/UA-1 carries the same pair (ISO 14289-1 §7.21.4.1 t2 and §7.21.8 t1) and was
// measured to behave identically; this table covers the PDF/A parts.
//
// # Corpus evidence
//
//   - pdfjs bug920426.pdf: a Type 0 font whose encoding CMap is written with
//     bfchar alone, for which veraPDF 1.30.2 reports both rules.
func TestUnmappedCodeResolvesToNoGlyph(t *testing.T) {
	t.Parallel()

	const bfcharOnly = "1 beginbfchar\n<0043> <0043>\nendbfchar\n"
	const cidRange = "1 begincidrange\n<0000> <00FF> 0\nendcidrange\n"
	const showUnmapped = "BT /F1 12 Tf 10 50 Td <0043> Tj ET"

	for _, tc := range []struct {
		name       string
		content    string
		encoding   string
		cmapBody   string
		noEmbed    bool
		wantGlyph  bool
		wantNotdef bool
		skipNotdef bool
	}{
		{
			name:       "a code the CMap maps nowhere reaches no glyph",
			content:    showUnmapped,
			encoding:   "8 0 R",
			cmapBody:   bfcharOnly,
			wantGlyph:  true,
			wantNotdef: true,
		},
		{
			name:       "the same code in rendering mode 3 is not rendered",
			content:    "BT /F1 12 Tf 3 Tr 10 50 Td <0043> Tj ET",
			encoding:   "8 0 R",
			cmapBody:   bfcharOnly,
			wantGlyph:  false,
			wantNotdef: true,
		},
		{
			name:       "a code written as CID 0 reaches the .notdef glyph",
			content:    "BT /F1 12 Tf 10 50 Td <0000> Tj ET",
			encoding:   "/Identity-H",
			cmapBody:   cidRange,
			wantGlyph:  false,
			wantNotdef: true,
		},
		{
			name:       "a CID the program contains is silent",
			content:    "BT /F1 12 Tf 10 50 Td <0005> Tj ET",
			encoding:   "/Identity-H",
			cmapBody:   cidRange,
			wantGlyph:  false,
			wantNotdef: false,
		},
		{
			name:       "a CMap naming a usecmap base leaves the code unresolved",
			content:    showUnmapped,
			encoding:   "8 0 R",
			cmapBody:   "/Identity-H usecmap\n" + bfcharOnly,
			wantGlyph:  false,
			wantNotdef: false,
		},
		{
			name:       "a CMap with a notdef range leaves the code unresolved",
			content:    showUnmapped,
			encoding:   "8 0 R",
			cmapBody:   bfcharOnly + "1 beginnotdefrange\n<0000> <00FF> 0\nendnotdefrange\n",
			wantGlyph:  false,
			wantNotdef: false,
		},
		{
			// The rule constrains embedded programs: "Embedded font programs
			// shall define all font glyphs referenced for rendering". With
			// nothing embedded there is no program a glyph could be missing
			// from, and veraPDF 1.30.2 reports the embedding rule
			// (§6.2.11.4.1 t1) for this file instead, not glyph presence.
			//
			// The .notdef half is not asserted: litho fires it here and veraPDF
			// does not, a divergence that predates this path and is unrelated to
			// which glyph an unmapped code reaches.
			name:       "an unembedded program has no glyph to be missing",
			content:    showUnmapped,
			encoding:   "8 0 R",
			cmapBody:   bfcharOnly,
			noEmbed:    true,
			wantGlyph:  false,
			skipNotdef: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data := type0CMapPDF(t, tc.content, tc.encoding, tc.cmapBody, !tc.noEmbed)

			for _, p := range []struct {
				profile      models.PDFAProfile
				glyphClause  string
				glyphTest    int
				notdefClause string
				notdefTest   int
			}{
				// ISO 19005-1 states glyph presence (§6.3.5 t1) but no Type0
				// .notdef rule, so only the glyph half is asserted there.
				{models.PDFA1b, "6.3.5", 1, "", 0},
				{models.PDFA2b, "6.2.11.4.1", 2, "6.2.11.8", 1},
				{models.PDFA3b, "6.2.11.4.1", 2, "6.2.11.8", 1},
				{models.PDFA4, "6.2.10.4.1", 2, "6.2.10.9", 1},
			} {
				got := firesRuleProfile(t, data, p.profile, p.glyphClause, p.glyphTest)
				if got != tc.wantGlyph {
					t.Errorf("%s: §%s t%d fired=%v, want %v",
						p.profile, p.glyphClause, p.glyphTest, got, tc.wantGlyph)
				}
				if p.notdefClause == "" || tc.skipNotdef {
					continue
				}
				gotNotdef := firesRuleProfile(t, data, p.profile, p.notdefClause, p.notdefTest)
				if gotNotdef != tc.wantNotdef {
					t.Errorf("%s: §%s t%d fired=%v, want %v",
						p.profile, p.notdefClause, p.notdefTest, gotNotdef, tc.wantNotdef)
				}
			}
		})
	}
}
