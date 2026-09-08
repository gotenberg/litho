package compliance

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestCheckType0NotdefGlyph_CIDFontType2 guards the .notdef-rendering check for
// a TrueType CIDFont (ISO 19005-2 §6.2.11.8, veraPDF object Glyph, name !=
// ".notdef"). The fixture is a Roboto subset with GID 0 = the real .notdef box,
// GID 1 = "uni0000" (an empty, zero-advance control-character glyph), and 38
// glyphs total. A used CID that resolves to an in-range non-zero GID must not
// fire even when that glyph is empty and zero-width; only a CID resolving to
// GID 0 or to a GID outside the program fires.
func TestCheckType0NotdefGlyph_CIDFontType2(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	ttf, err := os.ReadFile("testdata/cidfont_notdef_min.ttf")
	if err != nil {
		t.Fatalf("read TTF fixture: %v", err)
	}

	// check runs the .notdef check for a Type0/Identity-H font whose content
	// shows the given two-byte CIDs (Identity-H maps a two-byte code to that CID).
	check := func(cids ...uint16) []Violation {
		var raw []byte
		for _, c := range cids {
			raw = append(raw, byte(c>>8), byte(c))
		}
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Type /Font /Subtype /Type0 /BaseFont /X /Encoding /Identity-H /DescendantFonts [5 0 R] >>",
			// /W [1 [0]] declares CID 1's width as 0, the Roboto condition that
			// tripped the old "empty outline + width 0 = .notdef" heuristic.
			"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /X /CIDToGIDMap /Identity /W [1 [0]] /FontDescriptor 6 0 R /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> >>",
			"<< /Type /FontDescriptor /FontName /X /Flags 4 /FontFile2 7 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(ttf), string(ttf)),
		)
		type0, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
		if err != nil {
			t.Fatalf("resolve Type0 dict: %v", err)
		}
		usage := &fontUsageInfo{visible: true, rawStrings: []ast.PdfString{ast.PdfString(raw)}}
		return checkType0NotdefGlyph(ctx, doc, type0, "F1", 1, "PDF/A-2b", usage, withMalformedCodes)
	}

	// CID 1 → GID 1 = "uni0000": an in-range, empty, zero-width but real glyph.
	// veraPDF passes it; litho must not treat it as .notdef.
	if v := check(1); len(v) != 0 {
		t.Errorf("CID 1 (empty in-range glyph uni0000) must not fire .notdef, got %v", v)
	}
	// CID 0 → GID 0 = the .notdef glyph: must fire.
	if v := check(0); len(v) == 0 {
		t.Error("CID 0 (.notdef glyph) must fire")
	}
	// CID 100 is beyond the 38-glyph program. Under Identity /CIDToGIDMap it is
	// a missing glyph handled by the glyph-presence rule, not the .notdef rule:
	// veraPDF does not consistently treat an out-of-range CID as a .notdef Glyph,
	// so firing 6.2.11.8 here would be a false positive. It does report the
	// glyph as absent, so the presence rule is what must come back.
	v := check(100)
	if hasRule(v, notdefRule("PDF/A-2b")) {
		t.Errorf("CID 100 (beyond the glyph table) must not fire the .notdef rule, got %v", v)
	}
	if !hasRule(v, glyphPresenceRule("PDF/A-2b")) {
		t.Errorf("CID 100 (beyond the glyph table) must fire the glyph-presence rule, got %v", v)
	}
}

// TestNotdefUnembeddedNamedEncodingGap pins that a code the declared encoding
// leaves undefined references .notdef when no font program is embedded.
//
// veraPDF names a glyph ".notdef" exactly when the font program does not
// contain the code, which its GFGlyph constructor expresses as
// containsCode(code) ? getGlyphName(code) : ".notdef". With nothing embedded
// there is no program to consult, so the declared encoding decides.
// MacExpertEncoding defines no glyph at 'A' or 'B', so showing those codes
// references .notdef under ISO 19005-2, 6.2.11.8 t1.
//
// An embedded program is deliberately not judged this way: per ISO 32000-1,
// 9.6.6.2 it can supply a code the encoding omits, and answering that needs the
// program's own charset.
func TestNotdefUnembeddedNamedEncodingGap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	fires := func(t *testing.T, encoding string, flags int) bool {
		t.Helper()
		enc := ""
		if encoding != "" {
			enc = " /Encoding /" + encoding
		}
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> >>",
			"<< /Type /FontDescriptor /FontName /LithoTT /Flags "+strconv.Itoa(flags)+" /ItalicAngle 0"+
				" /Ascent 718 /Descent -207 /CapHeight 718 /StemV 88 /FontBBox [-166 -225 1000 931] >>",
			"<< /Type /Font /Subtype /TrueType /BaseFont /LithoTT /FirstChar 65 /LastChar 66"+
				" /Widths [722 722] /FontDescriptor 4 0 R"+enc+" >>",
		)
		fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 5})
		if err != nil {
			t.Fatalf("resolve font: %v", err)
		}
		usage := &fontUsageInfo{charCodes: map[byte]bool{65: true, 66: true}}
		got := checkNotdefGlyph(ctx, doc, fontDict, "F1", 1, "PDF/A-2b", usage)
		return hasRule(got, Rule_19005_2_6_2_11_8_t01)
	}

	const nonSymbolic, symbolic = 32, 4

	if !fires(t, "MacExpertEncoding", nonSymbolic) {
		t.Error("MacExpertEncoding defines no glyph at 'A', so the code references .notdef")
	}
	for _, encoding := range []string{"WinAnsiEncoding", "MacRomanEncoding"} {
		if fires(t, encoding, nonSymbolic) {
			t.Errorf("%s defines 'A', so nothing may fire", encoding)
		}
	}
	// No declared encoding keeps the Annex D standard-encoding default, which
	// covers printable ASCII, so an absent table entry says nothing.
	if fires(t, "", nonSymbolic) {
		t.Error("a font declaring no encoding must keep the standard-encoding default")
	}
	// A symbolic font resolves codes through its built-in encoding.
	if fires(t, "MacExpertEncoding", symbolic) {
		t.Error("a symbolic font is exempt, per ISO 32000-1 9.6.6.2")
	}
}
