package compliance

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// supplementCFFFont returns objects 5 to 7 of [glyphPagesObjs]: a symbolic
// Type 1 font with no /Encoding over a CFF program whose built-in encoding
// gives space the code 9 in its format 0 table and the code 32 in a
// supplement, as the font's /Widths entry for code 32 is spaceWidth.
func supplementCFFFont(spaceWidth int) []string {
	program := fontbuild.CFF{
		FontName: "ABCDEF+LithoSans",
		Glyphs: []fontbuild.Glyph{
			{Name: "A", Advance: 722},
			{Name: "y", Advance: 556},
			{Name: "space", Advance: 278},
		},
		NotdefAdvance: 722,
		Encoding:      map[byte]string{0x41: "A", 0x79: "y", 0x09: "space", 0x20: "space"},
	}.Build()
	return []string{
		fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /ABCDEF+LithoSans /FirstChar 32 /LastChar 32"+
			" /Widths [%d] /FontDescriptor 6 0 R >>", spaceWidth),
		"<< /Type /FontDescriptor /FontName /ABCDEF+LithoSans /Flags 4 /Ascent 700 /Descent -200 /CapHeight 700 " +
			"/StemV 80 /FontBBox [0 0 1000 800] /ItalicAngle 0 /FontFile3 7 0 R >>",
		exponentStream("/Subtype /Type1C", string(program)),
	}
}

// TestCFFSupplementCodeWidth pins that a code a CFF encoding's supplement
// gives a glyph is judged at the width of the glyph the supplement names. Per
// Adobe Technical Note #5176, section 12, Table 14, a supplement maps a code
// to a glyph SID, which the charset turns into a GID. The width rule, ISO
// 19005-2, 6.2.11.5, then compares that glyph's advance with /Widths.
//
// veraPDF 1.30.2 stores the SID as a GID index instead
// (CFFType1FontProgram.readSupplements, then getGIDFromCharCode adds 1), so it
// judges such a code at the advance of the glyph at GID SID + 1. litho keeps
// the reading the note states.
//
// # Corpus evidence
//
//   - govdocs thread_300_300228.pdf: QVUSMV+Helvetica-Bold gives space (SID 1)
//     the code 32 through a supplement. fontTools and litho read space, 569
//     units and /Widths 569; veraPDF reads GID 2, y, 1139 units, and reports
//     the one 6.2.11.5 finding the file and its remediated output carry.
func TestCFFSupplementCodeWidth(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		spaceWidth int
		want       glyphRuleCounts
	}{
		{"at the advance of the glyph the SID names", 278, glyphRuleCounts{0, 0, 0}},
		{"at the advance of another glyph", 556, glyphRuleCounts{0, 1, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkGlyphRuleCounts(t, glyphPagesObjs(supplementCFFFont(tc.spaceWidth), glyphText("( ) Tj")), tc.want)
		})
	}
}
