package compliance

import (
	"strings"
	"testing"
)

// nameKeyedCFF builds a name-keyed CFF program, one whose Top DICT carries no
// ROS operator, whose glyph 0 advances notdefWidth and glyph i+1 widths[i],
// over a Private DICT whose defaultWidthX is defaultWidthX. A CIDFontType0
// takes the CID as the glyph index into such a program (ISO 32000-1, 9.7.4.2),
// so a CID at or past len(widths)+1 has no glyph.
func nameKeyedCFF(widths []int, notdefWidth, defaultWidthX int) []byte {
	header := []byte{1, 0, 4, 4}
	name := cffIndex([][]byte{[]byte("LithoT1")})
	// A width operand (28 is a two-byte integer) then endchar.
	charString := func(w int) []byte { return []byte{28, byte(w >> 8), byte(w), 14} }
	charStrings := [][]byte{charString(notdefWidth)}
	for _, w := range widths {
		charStrings = append(charStrings, charString(w))
	}
	charStringsIndex := cffIndex(charStrings)
	// Charset format 0 naming the glyphs A, B, ... by their standard strings.
	charset := []byte{0}
	for i := range widths {
		sid := 34 + i
		charset = append(charset, byte(sid>>8), byte(sid))
	}
	private := cffDictInt(defaultWidthX)
	private = append(private, 20) // defaultWidthX
	private = append(private, 0x8b, 21)
	topDict := func(charsetOff, charStringsOff, privateOff int) []byte {
		d := cffDictInt(charsetOff)
		d = append(d, 15) // charset
		d = append(d, cffDictInt(charStringsOff)...)
		d = append(d, 17) // CharStrings
		d = append(d, cffDictInt(len(private))...)
		d = append(d, cffDictInt(privateOff)...)
		return append(d, 18) // Private
	}
	empty := []byte{0, 0} // String INDEX and Global Subr INDEX
	charsetOff := len(header) + len(name) + len(cffIndex([][]byte{topDict(0, 0, 0)})) + 2*len(empty)
	charStringsOff := charsetOff + len(charset)
	privateOff := charStringsOff + len(charStringsIndex)

	var out []byte
	out = append(out, header...)
	out = append(out, name...)
	out = append(out, cffIndex([][]byte{topDict(charsetOff, charStringsOff, privateOff)})...)
	out = append(out, empty...)
	out = append(out, empty...)
	out = append(out, charset...)
	out = append(out, charStringsIndex...)
	return append(out, private...)
}

// nameKeyedType0Font returns objects 5 to 8 of [glyphPagesObjs]: an
// Identity-H font over a CIDFontType0 named baseFont, whose dictionary adds
// cidFont and whose program is [nameKeyedCFF] with three glyphs, advancing
// 250, 500 and 600, and the given defaultWidthX.
func nameKeyedType0Font(baseFont, cidFont string, defaultWidthX int) []string {
	program := nameKeyedCFF([]int{500, 600}, 250, defaultWidthX)
	return []string{
		"<< /Type /Font /Subtype /Type0 /BaseFont /" + baseFont + " /Encoding /Identity-H /DescendantFonts [6 0 R] >>",
		"<< /Type /Font /Subtype /CIDFontType0 /BaseFont /" + baseFont + " " + cidFont +
			" /FontDescriptor 7 0 R /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> >>",
		"<< /Type /FontDescriptor /FontName /" + baseFont + " /Flags 4 /Ascent 700 /Descent -200 /CapHeight 700 " +
			"/StemV 80 /FontBBox [0 0 500 700] /ItalicAngle 0 /FontFile3 8 0 R >>",
		exponentStream("/Subtype /CIDFontType0C", string(program)),
	}
}

// TestNameKeyedCFFMissingGlyphs pins how the glyph rules judge a CID past the
// last glyph of a name-keyed CFF program under a CIDFontType0: it has no
// glyph, so it fails the presence and .notdef rules, and the width rule
// weighs it at the CIDFont's default width for a subset and at the program's
// defaultWidthX otherwise; see [cidFontType0Glyphs] and
// [checkCFFMissingGlyphWidths].
//
// Every count was measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b.
func TestNameKeyedCFFMissingGlyphs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		baseFont      string
		cidFont       string
		defaultWidthX int
		shows         string
		want          glyphRuleCounts
	}{
		{"a CID the program holds, at its advance", "ABCDEF+LithoT1", "/DW 1000 /W [1 [500]]", 0, "<0001> Tj", glyphRuleCounts{0, 0, 0}},
		{"a CID the program holds, off its advance", "ABCDEF+LithoT1", "/DW 1000 /W [1 [700]]", 0, "<0001> Tj", glyphRuleCounts{0, 1, 0}},
		{"the last glyph", "ABCDEF+LithoT1", "/DW 1000 /W [2 [600]]", 0, "<0002> Tj", glyphRuleCounts{0, 0, 0}},
		{"a subset: /W 500 against /DW 1000", "ABCDEF+LithoT1", "/DW 1000 /W [3 [500]]", 0, "<0003> Tj", glyphRuleCounts{1, 1, 1}},
		{"a subset: /W 1000 against the default 1000", "ABCDEF+LithoT1", "/W [3 [1000]]", 0, "<0003> Tj", glyphRuleCounts{1, 0, 1}},
		{"a subset: /DW 500 alone", "ABCDEF+LithoT1", "/DW 500", 0, "<0003> Tj", glyphRuleCounts{1, 0, 1}},
		{"a subset: CID 256", "ABCDEF+LithoT1", "/DW 1000", 0, "<0100> Tj", glyphRuleCounts{1, 0, 1}},
		{"a subset: two missing CIDs", "ABCDEF+LithoT1", "/DW 1000 /W [3 [500] 4 [400]]", 0, "<00030004> Tj", glyphRuleCounts{2, 2, 2}},
		{"a subset: only in rendering mode 3", "ABCDEF+LithoT1", "/DW 1000 /W [3 [500]]", 0, "3 Tr <0003> Tj", glyphRuleCounts{0, 0, 1}},
		{"a six-character name reads as a subset", "Lith01", "/DW 1000", 0, "<0003> Tj", glyphRuleCounts{1, 0, 1}},
		{"a six-character name: /W 0", "Lith01", "/DW 1000 /W [3 [0]]", 0, "<0003> Tj", glyphRuleCounts{1, 1, 1}},
		{"a full font: /DW 1000 against defaultWidthX 0", "LithoT1", "/DW 1000", 0, "<0003> Tj", glyphRuleCounts{1, 1, 1}},
		{"a full font: /W 0 against defaultWidthX 0", "LithoT1", "/DW 1000 /W [3 [0]]", 0, "<0003> Tj", glyphRuleCounts{1, 0, 1}},
		{"a full font: /W 700 against defaultWidthX 700", "LithoT1", "/DW 1000 /W [3 [700]]", 700, "<0003> Tj", glyphRuleCounts{1, 0, 1}},
		{"a full font: /W 500 against defaultWidthX 0", "LithoT1", "/DW 1000 /W [3 [500]]", 0, "<0003> Tj", glyphRuleCounts{1, 1, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkGlyphRuleCounts(t, glyphPagesObjs(nameKeyedType0Font(tc.baseFont, tc.cidFont, tc.defaultWidthX), glyphText(tc.shows)), tc.want)
		})
	}
}

// TestNameKeyedCFFSubsetNameIsTheCIDFonts pins that the width a missing glyph
// is weighed at follows the /BaseFont of the CIDFont, not of the Type0 font.
// Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on CID 3 of the program
// of [TestNameKeyedCFFMissingGlyphs] under /DW 1000.
func TestNameKeyedCFFSubsetNameIsTheCIDFonts(t *testing.T) {
	t.Parallel()

	full := nameKeyedType0Font("LithoT1", "/DW 1000", 0)
	full[0] = strings.Replace(full[0], "/BaseFont /LithoT1", "/BaseFont /ABCDEF+LithoT1", 1)
	subset := nameKeyedType0Font("ABCDEF+LithoT1", "/DW 1000", 0)
	subset[0] = strings.Replace(subset[0], "/BaseFont /ABCDEF+LithoT1", "/BaseFont /LithoT1", 1)
	checkGlyphRuleCounts(t, glyphPagesObjs(full, glyphText("<0003> Tj")), glyphRuleCounts{1, 1, 1})
	checkGlyphRuleCounts(t, glyphPagesObjs(subset, glyphText("<0003> Tj")), glyphRuleCounts{1, 0, 1})
}
