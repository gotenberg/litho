package compliance

import (
	"context"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// TestNotdefSymbolicType1ControlCode pins that a control-range charCode absent
// from a symbolic Type1 font's /Differences does not fire the .notdef rule: a
// symbolic font resolves such codes through its built-in encoding (ISO 32000-1
// §9.6.6.2), which litho does not parse, so reporting .notdef would be a false
// positive. A non-symbolic font keeps firing, since standard encodings define
// no glyph for control characters.
func TestNotdefSymbolicType1ControlCode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Object 2 is the font, 3 the /Encoding (Differences omit code 15), 4 the
	// descriptor. Flags is the only difference between the two runs.
	build := func(flags int) []Violation {
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog >>",
			"<< /Type /Font /Subtype /Type1 /Name /F4 /FirstChar 0 /LastChar 255 /Encoding 3 0 R /FontDescriptor 4 0 R >>",
			"<< /Type /Encoding /Differences [ 0 /C0 13 /C13 ] >>",
			"<< /Type /FontDescriptor /FontName /ABCDEF+AdvMT /Flags "+strconv.Itoa(flags)+" >>",
		)
		fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil {
			t.Fatalf("resolve font: %v", err)
		}
		usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{15: true}}
		return checkNotdefGlyph(ctx, doc, fontDict, "F4", 1, "PDF/A-2b", usage)
	}

	if got := build(4); len(got) != 0 { // Flags bit 3 (=4): Symbolic
		t.Errorf("symbolic Type1 control-code should not fire .notdef, got %v", got)
	}
	if got := build(32); len(got) == 0 { // Flags bit 6 (=32): Nonsymbolic
		t.Error("non-symbolic Type1 control-code without an encoding entry should fire .notdef")
	}
}

// TestNotdefTrueTypeControlCode pins that a used C0 control code in a
// nonsymbolic, non-embedded TrueType font fires the .notdef rule: standard
// encodings define no glyph for control characters, and the embedded-program
// path cannot run for a non-embedded font. DEL (0x7F) is excluded (some fonts
// define a glyph there), and a symbolic font is exempt.
func TestNotdefTrueTypeControlCode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	build := func(flags int, code byte) []Violation {
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog >>",
			"<< /Type /Font /Subtype /TrueType /Name /TT0 /FirstChar 0 /LastChar 255 /Encoding /WinAnsiEncoding /FontDescriptor 3 0 R >>",
			"<< /Type /FontDescriptor /FontName /Arial /Flags "+strconv.Itoa(flags)+" >>",
		)
		fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil {
			t.Fatalf("resolve font: %v", err)
		}
		usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{code: true}}
		return checkNotdefGlyph(ctx, doc, fontDict, "TT0", 1, "PDF/A-2b", usage)
	}

	if got := build(32, 31); len(got) == 0 { // Nonsymbolic, C0 control code 31
		t.Error("non-symbolic TrueType control code 31 should fire .notdef")
	}
	if got := build(4, 31); len(got) != 0 { // Symbolic: exempt
		t.Errorf("symbolic TrueType control code should not fire .notdef, got %v", got)
	}
	if got := build(32, 0x7F); len(got) != 0 { // DEL is excluded
		t.Errorf("DEL (0x7F) should not fire .notdef, got %v", got)
	}
}

// TestSymbolicCmapViolation pins the part-aware symbolic TrueType cmap rule
// against the veraPDF test expressions: PDF/A-1 requires exactly one subtable;
// PDF/A-2/3 and PDF/UA-1 also accept a Microsoft Symbol (3,0); PDF/A-4 and
// PDF/UA-2 accept a (3,0) or a Mac (1,0) regardless of count.
func TestSymbolicCmapViolation(t *testing.T) {
	sub := func(p, e uint16) truetype.CmapSubtable {
		return truetype.CmapSubtable{PlatformID: p, EncodingID: e}
	}
	macAndSymbol := []truetype.CmapSubtable{sub(1, 0), sub(3, 0)} // Wingdings shape
	symbolOnly := []truetype.CmapSubtable{sub(3, 0)}
	macOnly := []truetype.CmapSubtable{sub(1, 0)}
	unicodeOnly := []truetype.CmapSubtable{sub(3, 1)}
	twoNonSymbol := []truetype.CmapSubtable{sub(3, 1), sub(0, 3)}

	cases := []struct {
		name    string
		profile string
		subs    []truetype.CmapSubtable
		want    bool
	}{
		// Wingdings (Mac + Symbol): only PDF/A-1 rejects it (count != 1).
		{"A1 mac+symbol", "PDF/A-1b", macAndSymbol, true},
		{"A2 mac+symbol", "PDF/A-2b", macAndSymbol, false},
		{"A3 mac+symbol", "PDF/A-3b", macAndSymbol, false},
		{"A4 mac+symbol", "PDF/A-4", macAndSymbol, false},
		{"UA1 mac+symbol", "PDF/UA-1", macAndSymbol, false},
		{"UA2 mac+symbol", "PDF/UA-2", macAndSymbol, false},

		// A single non-symbol Unicode subtable: accepted by the count-based
		// levels, rejected by PDF/A-4 / PDF/UA-2 which demand (3,0) or (1,0).
		{"A1 unicode-only", "PDF/A-1b", unicodeOnly, false},
		{"A2 unicode-only", "PDF/A-2b", unicodeOnly, false},
		{"A4 unicode-only", "PDF/A-4", unicodeOnly, true},
		{"UA2 unicode-only", "PDF/UA-2", unicodeOnly, true},

		// Symbol-only and Mac-only single subtables are always fine.
		{"A2 symbol-only", "PDF/A-2b", symbolOnly, false},
		{"A4 symbol-only", "PDF/A-4", symbolOnly, false},
		{"A4 mac-only", "PDF/A-4", macOnly, false},
		{"UA2 mac-only", "PDF/UA-2", macOnly, false},

		// Two subtables, neither Symbol nor Mac: rejected everywhere.
		{"A1 two-nonsymbol", "PDF/A-1b", twoNonSymbol, true},
		{"A2 two-nonsymbol", "PDF/A-2b", twoNonSymbol, true},
		{"A4 two-nonsymbol", "PDF/A-4", twoNonSymbol, true},
		{"UA1 two-nonsymbol", "PDF/UA-1", twoNonSymbol, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := symbolicCmapViolation(tc.profile, tc.subs); got != tc.want {
				t.Errorf("symbolicCmapViolation(%q) = %v, want %v", tc.profile, got, tc.want)
			}
		})
	}
}

// TestToUnicodeTrueTypeStandardBaseDifferences pins that a nonsymbolic TrueType
// font with a standard base encoding and fully AGL-resolvable /Differences is
// exempt from the /ToUnicode requirement (§6.2.11.7.2): every code maps to an
// AGL glyph name, hence to Unicode, without /ToUnicode. veraPDF resolves the
// per-glyph Unicode for TrueType the same as Type1, so litho must not fire.
func TestToUnicodeTrueTypeStandardBaseDifferences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	build := func(diffs string) []Violation {
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog >>",
			"<< /Type /Font /Subtype /TrueType /Name /TT0 /FirstChar 0 /LastChar 255 /Encoding 3 0 R /FontDescriptor 4 0 R >>",
			"<< /Type /Encoding /BaseEncoding /MacRomanEncoding /Differences "+diffs+" >>",
			"<< /Type /FontDescriptor /FontName /ABCDEF+CenturySchoolbook /Flags 34 >>",
		)
		fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil {
			t.Fatalf("resolve font: %v", err)
		}
		return checkToUnicode(ctx, doc, fontDict, "TT0", 1, "PDF/A-2u", nil)
	}

	if got := build("[ 65 /A 219 /Euro ]"); len(got) != 0 {
		t.Errorf("TrueType with standard base + AGL-resolvable Differences should be exempt, got %v", got)
	}
	if got := build("[ 65 /customGlyphNotInAGL ]"); len(got) == 0 {
		t.Error("TrueType Differences with a non-AGL-resolvable name should still require /ToUnicode")
	}
}
