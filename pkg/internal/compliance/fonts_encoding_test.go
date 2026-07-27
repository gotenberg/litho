package compliance

import (
	"context"
	"fmt"
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

// TestNonSymbolicCmapViolation pins the per-part cmap requirement for a
// non-symbolic TrueType font program. PDF/A-1 states none. PDF/A-2, PDF/A-3 and
// PDF/UA-1 require at least one subtable, and more than one when a Microsoft
// Symbol (3,0) is present. PDF/A-4 and PDF/UA-2 instead require a Microsoft
// Unicode (3,1) or a Macintosh Roman (1,0), whatever the count.
func TestNonSymbolicCmapViolation(t *testing.T) {
	sub := func(p, e uint16) truetype.CmapSubtable {
		return truetype.CmapSubtable{PlatformID: p, EncodingID: e}
	}
	symbolOnly := []truetype.CmapSubtable{sub(3, 0)}
	symbolAndUnicode := []truetype.CmapSubtable{sub(3, 0), sub(3, 1)}
	unicodeOnly := []truetype.CmapSubtable{sub(3, 1)}
	macOnly := []truetype.CmapSubtable{sub(1, 0)}
	twoExotic := []truetype.CmapSubtable{sub(3, 2), sub(0, 3)}
	none := []truetype.CmapSubtable{}

	cases := []struct {
		name    string
		profile string
		subs    []truetype.CmapSubtable
		want    bool
	}{
		// A lone Microsoft Symbol carries no non-symbolic lookup, so the
		// count-based levels reject it; PDF/A-4 and PDF/UA-2 reject it because
		// it is neither (3,1) nor (1,0).
		{"A2 symbol-only", "PDF/A-2b", symbolOnly, true},
		{"A3 symbol-only", "PDF/A-3b", symbolOnly, true},
		{"UA1 symbol-only", "PDF/UA-1", symbolOnly, true},
		{"A4 symbol-only", "PDF/A-4", symbolOnly, true},
		{"UA2 symbol-only", "PDF/UA-2", symbolOnly, true},

		// Adding a Unicode subtable beside the Symbol satisfies every level.
		{"A2 symbol+unicode", "PDF/A-2b", symbolAndUnicode, false},
		{"UA1 symbol+unicode", "PDF/UA-1", symbolAndUnicode, false},
		{"A4 symbol+unicode", "PDF/A-4", symbolAndUnicode, false},
		{"UA2 symbol+unicode", "PDF/UA-2", symbolAndUnicode, false},

		// A single Unicode or Mac subtable is fine everywhere.
		{"A2 unicode-only", "PDF/A-2b", unicodeOnly, false},
		{"A4 unicode-only", "PDF/A-4", unicodeOnly, false},
		{"A2 mac-only", "PDF/A-2b", macOnly, false},
		{"A4 mac-only", "PDF/A-4", macOnly, false},
		{"UA2 mac-only", "PDF/UA-2", macOnly, false},

		// Two subtables, neither Symbol nor Unicode nor Mac: the count-based
		// levels accept them, PDF/A-4 and PDF/UA-2 do not. This is the case
		// that discriminates the two predicates.
		{"A2 two-exotic", "PDF/A-2b", twoExotic, false},
		{"UA1 two-exotic", "PDF/UA-1", twoExotic, false},
		{"A4 two-exotic", "PDF/A-4", twoExotic, true},
		{"UA2 two-exotic", "PDF/UA-2", twoExotic, true},

		// No cmap at all is a violation at every level that states the rule.
		{"A2 none", "PDF/A-2b", none, true},
		{"A4 none", "PDF/A-4", none, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nonSymbolicCmapViolation(tc.profile, tc.subs); got != tc.want {
				t.Errorf("nonSymbolicCmapViolation(%q) = %v, want %v", tc.profile, got, tc.want)
			}
		})
	}
}

// TestNonSymbolicCmapRuleIsAbsentUnderPart1 pins that PDF/A-1 states no
// non-symbolic cmap requirement, so the caller skips the check entirely.
func TestNonSymbolicCmapRuleIsAbsentUnderPart1(t *testing.T) {
	if rule := trueTypeNonSymbolicCmapRule("PDF/A-1b"); rule.Specification != "" {
		t.Errorf("PDF/A-1b: got rule %s %s, want none", rule.Specification, rule.Clause)
	}
	for _, profile := range []string{"PDF/A-2b", "PDF/A-3b", "PDF/A-4", "PDF/UA-1", "PDF/UA-2"} {
		if rule := trueTypeNonSymbolicCmapRule(profile); rule.Specification == "" {
			t.Errorf("%s: got no rule, want one", profile)
		}
	}
}

// TestToUnicodeTrueTypeStandardBaseDifferences pins that a nonsymbolic TrueType
// font with a standard base encoding and fully AGL-resolvable /Differences is
// exempt from the /ToUnicode requirement (§6.2.11.7.2): every code maps to an
// AGL glyph name, hence to Unicode, without /ToUnicode. veraPDF resolves the
// per-glyph Unicode for TrueType the same as Type1, so litho must not fire.
// A non-resolvable override still requires /ToUnicode, but only where the
// content stream renders the code it sits on: §6.2.11.7.2 asks for "the map of
// all used character codes", and veraPDF builds its Glyph object per shown code.
func TestToUnicodeTrueTypeStandardBaseDifferences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	build := func(diffs string, usage *fontUsageInfo) []Violation {
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
		return checkToUnicode(ctx, doc, fontDict, "TT0", 1, "PDF/A-2u", usage)
	}
	used := func(codes ...byte) *fontUsageInfo {
		u := &fontUsageInfo{visible: true, charCodes: map[byte]bool{}}
		for _, c := range codes {
			u.charCodes[c] = true
		}
		return u
	}

	if got := build("[ 65 /A 219 /Euro ]", used(65, 219)); len(got) != 0 {
		t.Errorf("TrueType with standard base + AGL-resolvable Differences should be exempt, got %v", got)
	}
	if got := build("[ 65 /customGlyphNotInAGL ]", used(65)); len(got) == 0 {
		t.Error("TrueType Differences with a rendered non-AGL-resolvable name should still require /ToUnicode")
	}
	if got := build("[ 65 /customGlyphNotInAGL ]", used(66)); len(got) != 0 {
		t.Errorf("TrueType Differences whose non-AGL name is never rendered should stay exempt, got %v", got)
	}
}

// TestToUnicodeSymbolicNonResolvableDifferences pins that a symbolic font whose
// used codes are remapped by /Differences to glyph names with no Unicode value
// (neither AGL- nor Symbol-encoding-resolvable, e.g. MathematicalPi's "H20850")
// is NOT exempt from /ToUnicode: those codes have no Unicode mapping, which
// veraPDF reports per glyph. A symbolic font whose used codes resolve (Symbol
// encoding names, or the built-in encoding litho cannot parse) stays exempt.
func TestToUnicodeSymbolicNonResolvableDifferences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	build := func(diffs string, used byte) []Violation {
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog >>",
			"<< /Type /Font /Subtype /Type1 /Name /F1 /FirstChar 0 /LastChar 255 /Encoding 3 0 R /FontDescriptor 4 0 R >>",
			"<< /Type /Encoding /Differences "+diffs+" >>",
			"<< /Type /FontDescriptor /FontName /GNDPAG+MathematicalPi-Three /Flags 4 >>", // 4 = Symbolic
		)
		fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil {
			t.Fatalf("resolve font: %v", err)
		}
		usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{used: true}}
		return checkToUnicode(ctx, doc, fontDict, "F1", 1, "PDF/A-2u", usage)
	}

	if got := build("[ 65 /H20850 ]", 65); len(got) == 0 {
		t.Error("symbolic font with a used non-resolvable Differences name should require /ToUnicode")
	}
	if got := build("[ 65 /alpha ]", 65); len(got) != 0 {
		t.Errorf("symbolic font whose used Differences name is a Symbol-encoding name should be exempt, got %v", got)
	}
	if got := build("[ 65 /H20850 ]", 66); len(got) != 0 {
		t.Errorf("symbolic font whose UNUSED code is non-resolvable should stay exempt, got %v", got)
	}
}

// TestToUnicodeStandard14Differences pins that a standard 14 /BaseFont name does
// not excuse an /Encoding /Differences override with no Unicode value. The
// standard 14 built-in encodings are AGL-based, which settles the base encoding
// only: a rendered code remapped to a name outside the AGL and the Symbol set
// maps to nothing, and ISO 19005-2 §6.2.11.7.2 (with ISO 14289-1 §7.21.7 and
// ISO 14289-2 §8.4.5.8) demands a Unicode value for every used character code.
// veraPDF 1.30.2 fires on such a font whatever its /BaseFont says. Scoping is by
// rendered code, matching veraPDF's Glyph object: an override nothing shows maps
// nothing, so it stays exempt.
func TestToUnicodeStandard14Differences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	build := func(baseFont, diffs string, usage *fontUsageInfo) []Violation {
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog >>",
			"<< /Type /Font /Subtype /Type1 /Name /F1 /BaseFont /"+baseFont+" /Encoding 3 0 R >>",
			"<< /Type /Encoding /Differences "+diffs+" >>",
		)
		fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil {
			t.Fatalf("resolve font: %v", err)
		}
		return checkToUnicode(ctx, doc, fontDict, "F1", 1, "PDF/A-2u", usage)
	}
	used := func(codes ...byte) *fontUsageInfo {
		u := &fontUsageInfo{visible: true, charCodes: map[byte]bool{}}
		for _, c := range codes {
			u.charCodes[c] = true
		}
		return u
	}
	bad := "[ 65 /lithoglyphone /lithoglyphtwo ]"

	if got := build("Helvetica", bad, used(65, 66)); len(got) == 0 {
		t.Error("standard 14 font with a rendered non-resolvable Differences name should require /ToUnicode")
	}
	if got := build("LithoCustom", bad, used(65, 66)); len(got) == 0 {
		t.Error("non-standard font with a rendered non-resolvable Differences name should require /ToUnicode")
	}
	if got := build("Helvetica", bad, used(67)); len(got) != 0 {
		t.Errorf("standard 14 font whose non-resolvable override is never rendered should stay exempt, got %v", got)
	}
	if got := build("Helvetica", bad, nil); len(got) != 0 {
		t.Errorf("standard 14 font with no recorded usage should stay exempt, got %v", got)
	}
	if got := build("Helvetica", "[ 65 /eacute /Euro ]", used(65, 66)); len(got) != 0 {
		t.Errorf("standard 14 font with AGL-resolvable Differences should stay exempt, got %v", got)
	}
	if got := build("Symbol", "[ 65 /summation /integral ]", used(65, 66)); len(got) != 0 {
		t.Errorf("Symbol font with Symbol-set Differences should stay exempt, got %v", got)
	}
	if got := build("Helvetica", "[ 65 /.notdef ]", used(65)); len(got) != 0 {
		t.Errorf("a /.notdef override is the .notdef rule's business, not this one, got %v", got)
	}
}

// TestToUnicodeStandardBaseEncodingScopedToRenderedCodes pins that a standard
// /BaseEncoding exempts a font from /ToUnicode as long as the /Differences
// overrides behind RENDERED codes resolve to Unicode. ISO 19005-2 §6.2.11.7.2
// asks for "the map of all used character codes", and veraPDF builds its Glyph
// object only for a code a text-showing operator draws, so an override the
// content stream never reaches cannot leave a used code unmapped. veraPDF 1.30.2
// passes such a file; litho validated every entry regardless of usage and
// reported a false positive on it.
func TestToUnicodeStandardBaseEncodingScopedToRenderedCodes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// /BaseFont is deliberately outside the standard 14, so the standard 14
	// exemption cannot mask the /BaseEncoding path under test.
	build := func(diffs string, usage *fontUsageInfo) []Violation {
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog >>",
			"<< /Type /Font /Subtype /Type1 /Name /F1 /BaseFont /LithoCustom /Encoding 3 0 R >>",
			"<< /Type /Encoding /BaseEncoding /WinAnsiEncoding /Differences "+diffs+" >>",
		)
		fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil {
			t.Fatalf("resolve font: %v", err)
		}
		return checkToUnicode(ctx, doc, fontDict, "F1", 1, "PDF/A-2u", usage)
	}
	used := func(codes ...byte) *fontUsageInfo {
		u := &fontUsageInfo{visible: true, charCodes: map[byte]bool{}}
		for _, c := range codes {
			u.charCodes[c] = true
		}
		return u
	}
	bad := "[ 200 /lithoglyphone /lithoglyphtwo ]"

	if got := build(bad, used(65, 66)); len(got) != 0 {
		t.Errorf("standard /BaseEncoding whose bad override is never rendered should stay exempt, got %v", got)
	}
	if got := build(bad, used(200)); len(got) == 0 {
		t.Error("standard /BaseEncoding with a RENDERED non-resolvable override should require /ToUnicode")
	}
	if got := build(bad, nil); len(got) != 0 {
		t.Errorf("standard /BaseEncoding with no recorded usage should stay exempt, got %v", got)
	}
	if got := build("[ 200 /eacute /Euro ]", used(200, 201)); len(got) != 0 {
		t.Errorf("standard /BaseEncoding with AGL-resolvable overrides should stay exempt, got %v", got)
	}
}

// TestToUnicodeCMapValuesRenderedNotdef pins that a rendered /Encoding
// /Differences entry naming /.notdef fires the ToUnicode value rule. ISO 32000-1
// §9.6.6.1 gives .notdef the meaning "no glyph", so the code maps to U+0000,
// which ISO 19005-2 §6.2.11.7.2 t2 (and ISO 14289-1 §7.21.7 t2) forbid. veraPDF
// 1.30.2 reports "The glyph has Unicode value 0" on such a code; litho reported
// only the companion reference rule §6.2.11.8. A /ToUnicode entry for the code
// supersedes the name (ISO 32000-1 §9.10.2), and an override nothing renders is
// out of scope, matching veraPDF's per-Glyph scoping.
func TestToUnicodeCMapValuesRenderedNotdef(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	build := func(subtype, diffs string, used byte) []Violation {
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog >>",
			"<< /Type /Font /Subtype /"+subtype+" /Name /F1 /BaseFont /Helvetica /Encoding 3 0 R >>",
			"<< /Type /Encoding /Differences "+diffs+" >>",
		)
		fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil {
			t.Fatalf("resolve font: %v", err)
		}
		usage := &fontUsageInfo{
			visible:    true,
			charCodes:  map[byte]bool{used: true},
			rawStrings: []ast.PdfString{ast.PdfString([]byte{used})},
		}
		return checkToUnicodeCMapValues(ctx, doc, fontDict, "F1", 1, "PDF/A-2u", usage)
	}

	if got := build("Type1", "[ 65 /.notdef ]", 65); len(got) == 0 {
		t.Error("a rendered /.notdef override with no /ToUnicode must fire the value rule")
	}
	if got := build("TrueType", "[ 65 /.notdef ]", 65); len(got) == 0 {
		t.Error("the rendered /.notdef rule is font-type agnostic; TrueType must fire too")
	}
	if got := build("Type3", "[ 65 /.notdef ]", 65); len(got) == 0 {
		t.Error("the rendered /.notdef rule is font-type agnostic; Type3 must fire too")
	}
	if got := build("Type1", "[ 200 /.notdef ]", 65); len(got) != 0 {
		t.Errorf("a /.notdef override nothing renders must stay silent, got %v", got)
	}
	if got := build("Type1", "[ 65 /eacute ]", 65); len(got) != 0 {
		t.Errorf("a rendered AGL-resolvable override must stay silent, got %v", got)
	}
}

// TestToUnicodeCMapValuesNotdefOverriddenByCMap pins the other half of the rule
// above: a /ToUnicode entry for the code supersedes the .notdef glyph name, so
// the font passes, while a /ToUnicode silent on that code leaves the name in
// charge and still fires.
func TestToUnicodeCMapValuesNotdefOverriddenByCMap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	build := func(entries string) []Violation {
		data := "/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
			"/CMapName /Test def\n/CMapType 2 def\n" +
			"1 begincodespacerange\n<00> <FF>\nendcodespacerange\n" + entries +
			"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n"
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog >>",
			"<< /Type /Font /Subtype /Type1 /Name /F1 /BaseFont /Helvetica /Encoding 3 0 R /ToUnicode 4 0 R >>",
			"<< /Type /Encoding /Differences [ 65 /.notdef ] >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(data), data),
		)
		fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil {
			t.Fatalf("resolve font: %v", err)
		}
		usage := &fontUsageInfo{
			visible:    true,
			charCodes:  map[byte]bool{0x41: true},
			rawStrings: []ast.PdfString{"A"},
		}
		return checkToUnicodeCMapValues(ctx, doc, fontDict, "F1", 1, "PDF/A-2u", usage)
	}

	if got := build("1 beginbfchar\n<41> <0041>\nendbfchar\n"); len(got) != 0 {
		t.Errorf("a /ToUnicode entry for the code supersedes the .notdef name, got %v", got)
	}
	if got := build("1 beginbfchar\n<42> <0042>\nendbfchar\n"); len(got) == 0 {
		t.Error("a /ToUnicode silent on the rendered .notdef code must still fire the value rule")
	}
}
