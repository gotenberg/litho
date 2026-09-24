package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// cffIndex assembles a CFF INDEX from its entries. Offsets use the four-byte
// form so an entry's encoded size does not depend on the values written into
// it, which keeps the two-pass layout in [cidKeyedCFF] exact.
func cffIndex(entries [][]byte) []byte {
	if len(entries) == 0 {
		return []byte{0, 0}
	}
	count := len(entries)
	out := []byte{byte(count >> 8), byte(count), 4}
	put := func(v int) {
		out = append(out, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
	}
	off := 1 // CFF INDEX offsets are 1-based.
	put(off)
	for _, e := range entries {
		off += len(e)
		put(off)
	}
	for _, e := range entries {
		out = append(out, e...)
	}
	return out
}

// cidKeyedCFF builds a minimal CID-keyed CFF program whose charset holds
// exactly the given CIDs. GID 0 is .notdef and carries CID 0 implicitly, so a
// CID outside the list has no glyph in the program.
func cidKeyedCFF(cids []uint16) []byte {
	header := []byte{1, 0, 4, 4}
	name := cffIndex([][]byte{[]byte("LithoCID")})

	charStrings := make([][]byte, len(cids)+1)
	for i := range charStrings {
		charStrings[i] = []byte{14} // endchar
	}
	strings := cffIndex(charStrings)

	// Charset format 0: GID i (i >= 1) takes the CID at position i-1.
	charset := []byte{0}
	for _, c := range cids {
		charset = append(charset, byte(c>>8), byte(c))
	}

	topDict := func(charsetOff, charStringsOff int) []byte {
		num := func(v int) []byte {
			return []byte{29, byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
		}
		// ROS (12 30) with three zero operands marks the font CID-keyed.
		d := []byte{0x8b, 0x8b, 0x8b, 12, 30}
		d = append(d, num(charsetOff)...)
		d = append(d, 15) // charset
		d = append(d, num(charStringsOff)...)
		d = append(d, 17) // CharStrings
		return d
	}

	empty := []byte{0, 0} // String INDEX and Global Subr INDEX
	topIdxLen := len(cffIndex([][]byte{topDict(0, 0)}))
	charsetOff := len(header) + len(name) + topIdxLen + 2*len(empty)
	charStringsOff := charsetOff + len(charset)

	out := make([]byte, 0, charStringsOff+len(strings))
	out = append(out, header...)
	out = append(out, name...)
	out = append(out, cffIndex([][]byte{topDict(charsetOff, charStringsOff)})...)
	out = append(out, empty...)
	out = append(out, empty...)
	out = append(out, charset...)
	out = append(out, strings...)
	return out
}

// TestCheckType0NotdefGlyph_CIDFontType0RenderModeScope pins the rendering-mode
// scope of the two rules a CID absent from a CID-keyed CFF charset triggers.
//
// ISO 19005-2 §6.2.11.4.1 asks that embedded fonts define the glyphs referenced
// "for rendering", and text shown in rendering mode 3 is not rendered, so a
// font every one of whose uses is invisible has no glyph-presence answer to
// give. The .notdef rule (§6.2.11.8) carries no such exemption and keeps
// firing. The CIDFontType2 branch already scoped presence this way.
//
// # Corpus evidence
//
//   - govdocs thread_200_200565.pdf: all 411 show operators are mode 3, and
//     veraPDF reports 6.2.11.8 and not 6.2.11.4.1.
func TestCheckType0NotdefGlyph_CIDFontType0RenderModeScope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cff := cidKeyedCFF([]uint16{1, 2})

	check := func(visible bool, cid uint16) []Violation {
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Type /Font /Subtype /Type0 /BaseFont /X /Encoding /Identity-H /DescendantFonts [5 0 R] >>",
			"<< /Type /Font /Subtype /CIDFontType0 /BaseFont /X /FontDescriptor 6 0 R"+
				" /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> >>",
			"<< /Type /FontDescriptor /FontName /X /Flags 4 /FontFile3 7 0 R >>",
			fmt.Sprintf("<< /Length %d /Subtype /CIDFontType0C >>\nstream\n%s\nendstream", len(cff), string(cff)),
		)
		type0, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
		if err != nil {
			t.Fatalf("resolve Type0 dict: %v", err)
		}
		usage := &fontUsageInfo{
			visible:    visible,
			rawStrings: []ast.PdfString{ast.PdfString([]byte{byte(cid >> 8), byte(cid)})},
		}
		return checkType0NotdefGlyph(ctx, doc, type0, "F1", 1, "PDF/A-2b", usage, withMalformedCodes)
	}

	// A CID the charset holds answers both rules, visible or not.
	if v := check(true, 1); len(v) != 0 {
		t.Errorf("CID 1 is in the charset, nothing may fire; got %v", v)
	}

	// Visible use of an absent CID fires both rules.
	visible := check(true, 47)
	if !hasRule(visible, notdefRule("PDF/A-2b")) {
		t.Errorf("visible CID 47 must fire the .notdef rule, got %v", visible)
	}
	if !hasRule(visible, glyphPresenceRule("PDF/A-2b")) {
		t.Errorf("visible CID 47 must fire the glyph-presence rule, got %v", visible)
	}

	// Mode-3-only use keeps the .notdef rule and drops glyph presence.
	invisible := check(false, 47)
	if !hasRule(invisible, notdefRule("PDF/A-2b")) {
		t.Errorf("invisible CID 47 must still fire the .notdef rule, got %v", invisible)
	}
	if hasRule(invisible, glyphPresenceRule("PDF/A-2b")) {
		t.Errorf("invisible CID 47 is not rendered, so glyph presence is exempt; got %v", invisible)
	}
}
