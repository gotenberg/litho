package compliance

import (
	"context"
	"fmt"
	"os"
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
		return checkType0NotdefGlyph(ctx, doc, type0, "F1", 1, "PDF/A-2b", usage)
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
	// so firing 6.2.11.8 here would be a false positive.
	if v := check(100); len(v) != 0 {
		t.Errorf("CID 100 (beyond the glyph table) must not fire the .notdef rule, got %v", v)
	}
}
