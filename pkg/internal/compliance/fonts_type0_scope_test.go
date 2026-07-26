package compliance

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// firesRuleProfile reports whether validating data under profile raises the
// clause/test rule. Unlike firesRule and firesRule2b it is profile-parameterized
// so one fixture can be checked across PDF/A parts, which is what the per-part
// spellings of the embedding and glyph-presence rules require.
func firesRuleProfile(t *testing.T, data []byte, profile models.PDFAProfile, clause string, test int) bool {
	t.Helper()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule.Clause == clause && v.Rule.TestNumber == test {
			return true
		}
	}
	return false
}

// type0FontPDF builds a one-page PDF whose only font is a Type0/CIDFontType2
// with an Identity-H encoding. fontFile is the descendant /FontDescriptor's
// /FontFile2 entry (empty for an unembedded font), extraObj is an optional
// trailing object holding the font program, and content is the page content
// stream.
func type0FontPDF(content, fontFile, extraObj string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] " +
			"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type0 /BaseFont /Arial /Encoding /Identity-H " +
			"/DescendantFonts [6 0 R] >>",
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Arial /DW 1000 " +
			"/CIDToGIDMap /Identity /FontDescriptor 7 0 R " +
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> >>",
		"<< /Type /FontDescriptor /FontName /Arial /Flags 32 " +
			"/Ascent 700 /Descent -200 /CapHeight 700 /StemV 80 " +
			"/FontBBox [0 0 500 700] /ItalicAngle 0 " + fontFile + ">>",
	}
	if extraObj != "" {
		objs = append(objs, extraObj)
	}
	return cyclePDF(objs...)
}

// simpleFontPDF builds the simple-font counterpart of [type0FontPDF]: a one-page
// PDF whose only font is an unembedded TrueType.
func simpleFontPDF(content string) []byte {
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
			"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /TrueType /BaseFont /Arial /Encoding /WinAnsiEncoding "+
			"/FirstChar 65 /LastChar 65 /Widths [500] /FontDescriptor 6 0 R >>",
		"<< /Type /FontDescriptor /FontName /Arial /Flags 32 "+
			"/Ascent 700 /Descent -200 /CapHeight 700 /StemV 80 "+
			"/FontBBox [0 0 500 700] /ItalicAngle 0 >>",
	)
}

// TestFontEmbedding_RenderingMode3ExemptsCompositeFonts pins that the
// rendering-mode-3 escape in the embedding rule applies to composite fonts, not
// only simple ones. veraPDF's test is
// `Subtype == "Type3" || Subtype == "Type0" || renderingMode == 3 ||
// containsFontFile == true` on object PDFont for every part (ISO 19005-1 §6.3.4
// t1, ISO 19005-2/3 §6.2.11.4.1 t1, ISO 19005-4 §6.2.10.4.1 t1) and PDCIDFont is
// a PDFont, so a descendant CIDFont drawn only with `3 Tr` is exempt exactly as
// the simple font is. litho checked the descendant unconditionally and reported
// an embedding violation veraPDF never raises.
func TestFontEmbedding_RenderingMode3ExemptsCompositeFonts(t *testing.T) {
	t.Parallel()

	parts := []struct {
		profile models.PDFAProfile
		clause  string
	}{
		{models.PDFA1b, "6.3.4"},
		{models.PDFA2b, "6.2.11.4.1"},
		{models.PDFA4, "6.2.10.4.1"},
	}

	// Identity-H two-byte codes for CIDs 0x2B 0x48 0x4F.
	invisible := type0FontPDF("BT /F1 12 Tf 3 Tr 10 50 Td <002B0048004F> Tj ET", "", "")
	visible := type0FontPDF("BT /F1 12 Tf 0 Tr 10 50 Td <002B0048004F> Tj ET", "", "")
	simpleInvisible := simpleFontPDF("BT /F1 12 Tf 3 Tr 10 50 Td (A) Tj ET")
	simpleVisible := simpleFontPDF("BT /F1 12 Tf 0 Tr 10 50 Td (A) Tj ET")

	for _, p := range parts {
		t.Run(string(p.profile), func(t *testing.T) {
			t.Parallel()
			if firesRuleProfile(t, invisible, p.profile, p.clause, 1) {
				t.Errorf("unembedded Type0 font drawn only with 3 Tr must not fire §%s t1", p.clause)
			}
			if firesRuleProfile(t, simpleInvisible, p.profile, p.clause, 1) {
				t.Errorf("unembedded simple font drawn only with 3 Tr must not fire §%s t1", p.clause)
			}
			// Controls: the same fonts drawn visibly still fire, so the fix
			// narrows the rule to the exemption and nothing more.
			if !firesRuleProfile(t, visible, p.profile, p.clause, 1) {
				t.Errorf("unembedded Type0 font drawn with 0 Tr must fire §%s t1", p.clause)
			}
			if !firesRuleProfile(t, simpleVisible, p.profile, p.clause, 1) {
				t.Errorf("unembedded simple font drawn with 0 Tr must fire §%s t1", p.clause)
			}
		})
	}
}

// TestGlyphPresence_RequiresEmbeddedProgram pins that the glyph-presence rule
// needs an embedded font program to be missing a glyph from. Every part spells
// the test as `renderingMode == 3 || isGlyphPresent == null || isGlyphPresent ==
// true` on object Glyph (ISO 19005-1 §6.3.5 t1, ISO 19005-2/3 §6.2.11.4.1 t2,
// ISO 19005-4 §6.2.10.4.1 t2), and veraPDF leaves isGlyphPresent null when no
// program is embedded, so the rule passes. The requirement itself is scoped to
// embedded programs: "Embedded font programs shall define all font glyphs
// referenced for rendering". litho fired it from the decode-level .notdef signal
// (an odd-length Identity-H code tail) even when the font embedded nothing,
// double-reporting what the embedding rule already covers.
func TestGlyphPresence_RequiresEmbeddedProgram(t *testing.T) {
	t.Parallel()

	ttf, err := os.ReadFile("testdata/cidfont_notdef_min.ttf")
	if err != nil {
		t.Fatalf("read TTF fixture: %v", err)
	}

	// An odd-length string under Identity-H leaves a trailing byte that cannot
	// form a CID, so the renderer draws .notdef and the code resolves to no
	// glyph. The two fixtures differ only in whether a program is embedded.
	const content = "BT /F1 12 Tf 0 Tr 10 50 Td <002B48> Tj ET"
	unembedded := type0FontPDF(content, "", "")
	embedded := type0FontPDF(content, "/FontFile2 8 0 R ",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(ttf), string(ttf)))

	parts := []struct {
		profile models.PDFAProfile
		clause  string
		test    int
	}{
		{models.PDFA1b, "6.3.5", 1},
		{models.PDFA2b, "6.2.11.4.1", 2},
		{models.PDFA4, "6.2.10.4.1", 2},
	}
	for _, p := range parts {
		t.Run(string(p.profile), func(t *testing.T) {
			t.Parallel()
			if firesRuleProfile(t, unembedded, p.profile, p.clause, p.test) {
				t.Errorf("a composite font with no embedded program must not fire §%s t%d", p.clause, p.test)
			}
			// Control: the identical defect over an embedded program still
			// fires, so the fix removes only the no-program case.
			if !firesRuleProfile(t, embedded, p.profile, p.clause, p.test) {
				t.Errorf("a rendered code resolving to no glyph in an embedded program must fire §%s t%d", p.clause, p.test)
			}
		})
	}
}
