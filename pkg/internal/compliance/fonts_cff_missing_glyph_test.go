package compliance

import (
	"testing"
)

// cffDictInt encodes a CFF DICT integer operand in its five-byte form, so the
// size of a DICT does not depend on the offsets written into it.
func cffDictInt(v int) []byte {
	return []byte{29, byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
}

// cidKeyedCFFWithWidths builds a CID-keyed CFF program with an FDArray and a
// Private DICT whose charset holds cids, glyph i+1 carrying cids[i] and
// advancing widths[i] units of a 1000-unit em, and whose glyph 0, .notdef,
// advances notdefWidth. A CID outside cids has no glyph in the program.
func cidKeyedCFFWithWidths(cids []uint16, widths []int, notdefWidth int) []byte {
	header := []byte{1, 0, 4, 4}
	name := cffIndex([][]byte{[]byte("LithoCID")})
	// A width operand (28 is a two-byte integer) then endchar.
	charString := func(w int) []byte { return []byte{28, byte(w >> 8), byte(w), 14} }
	charStrings := [][]byte{charString(notdefWidth)}
	for _, w := range widths {
		charStrings = append(charStrings, charString(w))
	}
	charStringsIndex := cffIndex(charStrings)
	charset := []byte{0}
	for _, c := range cids {
		charset = append(charset, byte(c>>8), byte(c))
	}
	// FDSelect format 3: one range, every glyph in font DICT 0.
	glyphs := len(charStrings)
	fdSelect := []byte{3, 0, 1, 0, 0, 0, byte(glyphs >> 8), byte(glyphs)}
	private := []byte{0x8b, 20, 0x8b, 21} // defaultWidthX 0, nominalWidthX 0
	fontDict := func(privateOff int) []byte {
		d := cffDictInt(len(private))
		d = append(d, cffDictInt(privateOff)...)
		return append(d, 18) // Private
	}
	topDict := func(charsetOff, fdSelectOff, charStringsOff, fdArrayOff int) []byte {
		d := []byte{0x8b, 0x8b, 0x8b, 12, 30} // ROS: the font is CID-keyed
		d = append(d, cffDictInt(charsetOff)...)
		d = append(d, 15) // charset
		d = append(d, cffDictInt(charStringsOff)...)
		d = append(d, 17) // CharStrings
		d = append(d, cffDictInt(fdArrayOff)...)
		d = append(d, 12, 36) // FDArray
		d = append(d, cffDictInt(fdSelectOff)...)
		return append(d, 12, 37) // FDSelect
	}
	empty := []byte{0, 0} // String INDEX and Global Subr INDEX
	charsetOff := len(header) + len(name) + len(cffIndex([][]byte{topDict(0, 0, 0, 0)})) + 2*len(empty)
	fdSelectOff := charsetOff + len(charset)
	charStringsOff := fdSelectOff + len(fdSelect)
	fdArrayOff := charStringsOff + len(charStringsIndex)
	privateOff := fdArrayOff + len(cffIndex([][]byte{fontDict(0)}))

	var out []byte
	out = append(out, header...)
	out = append(out, name...)
	out = append(out, cffIndex([][]byte{topDict(charsetOff, fdSelectOff, charStringsOff, fdArrayOff)})...)
	out = append(out, empty...)
	out = append(out, empty...)
	out = append(out, charset...)
	out = append(out, fdSelect...)
	out = append(out, charStringsIndex...)
	out = append(out, cffIndex([][]byte{fontDict(privateOff)})...)
	return append(out, private...)
}

// cffType0Font returns objects 5 to 8 of [glyphPagesObjs]: an Identity-H font
// over a CIDFontType0 whose dictionary adds cidFont and whose program is
// [cidKeyedCFFWithWidths] with CIDs 1 and 2, advancing 500 and 600.
func cffType0Font(cidFont string) []string {
	program := cidKeyedCFFWithWidths([]uint16{1, 2}, []int{500, 600}, 250)
	return []string{
		"<< /Type /Font /Subtype /Type0 /BaseFont /ABCDEF+LithoCID /Encoding /Identity-H /DescendantFonts [6 0 R] >>",
		"<< /Type /Font /Subtype /CIDFontType0 /BaseFont /ABCDEF+LithoCID " + cidFont +
			" /FontDescriptor 7 0 R /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> >>",
		"<< /Type /FontDescriptor /FontName /ABCDEF+LithoCID /Flags 4 /Ascent 700 /Descent -200 /CapHeight 700 " +
			"/StemV 80 /FontBBox [0 0 500 700] /ItalicAngle 0 /FontFile3 8 0 R >>",
		exponentStream("/Subtype /CIDFontType0C", string(program)),
	}
}

// TestCFFMissingGlyphWidths pins how the width rule judges a CID a CID-keyed
// CFF program's charset does not hold: at the CIDFont's default width, /DW or
// 1000, against the /W or /DW width the dictionary declares; see
// [checkCFFMissingGlyphWidths]. The glyph fails the presence and .notdef rules
// whatever its widths.
//
// Every count was measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b.
func TestCFFMissingGlyphWidths(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		cidFont string
		shows   string
		want    glyphRuleCounts
	}{
		{"a CID the charset holds, at its advance", "/DW 1000 /W [1 [500]]", "<0001> Tj", glyphRuleCounts{0, 0, 0}},
		{"a CID the charset holds, off its advance", "/DW 1000 /W [1 [700]]", "<0001> Tj", glyphRuleCounts{0, 1, 0}},
		{"/W 500 against /DW 1000", "/DW 1000 /W [3 [500]]", "<0003> Tj", glyphRuleCounts{1, 1, 1}},
		{"a /W range of 500 against /DW 1000", "/DW 1000 /W [3 5 500]", "<0003> Tj", glyphRuleCounts{1, 1, 1}},
		{"/W 0 against the default 1000", "/W [3 [0]]", "<0003> Tj", glyphRuleCounts{1, 1, 1}},
		{"/W 1000 against /DW 500", "/DW 500 /W [3 [1000]]", "<0003> Tj", glyphRuleCounts{1, 1, 1}},
		{"/W 1000 against the default 1000", "/W [3 [1000]]", "<0003> Tj", glyphRuleCounts{1, 0, 1}},
		{"/DW 1000 alone", "/DW 1000", "<0003> Tj", glyphRuleCounts{1, 0, 1}},
		{"/DW 500 alone", "/DW 500", "<0003> Tj", glyphRuleCounts{1, 0, 1}},
		{"two missing CIDs", "/DW 1000 /W [3 [500] 4 [400]]", "<00030004> Tj", glyphRuleCounts{2, 2, 2}},
		{"only in rendering mode 3", "/DW 1000 /W [3 [500]]", "3 Tr <0003> Tj", glyphRuleCounts{0, 0, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkGlyphRuleCounts(t, glyphPagesObjs(cffType0Font(tc.cidFont), glyphText(tc.shows)), tc.want)
		})
	}
}
