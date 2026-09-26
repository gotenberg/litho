package compliance

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestTilingCellsShareOneGraphicsState pins that every tiling cell a content
// stream paints begins in one graphics state, the one the stream began in,
// which an uncoloured pattern the stream paints turns the colour operators off
// in and the underlying space of a Pattern space replaces the fill or stroke
// of (see [colorLinkWalk.cellEntry]). Every count was measured with veraPDF
// 1.30 at PDF/A-1b and PDF/A-2b on the same fixture.
func TestTilingCellsShareOneGraphicsState(t *testing.T) {
	t.Parallel()

	const (
		fill       = "0 0 10 10 re f"
		coloured   = "/Pattern cs /P1 scn " + fill
		uncoloured = "/CSp cs 0.5 /P2 scn " + fill
		patternCS  = "/CSp [/Pattern /DeviceGray]"
		rgbCell    = "1 0 0 rg 0 0 5 5 re f"
	)
	tiling := func(paintType int, content, res string) string {
		return linkStreamObj(fmt.Sprintf("/Type /Pattern /PatternType 1 /PaintType %d /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Resources %s", paintType, res), content)
	}
	form := func(content, res string) string {
		return linkStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources "+res, content)
	}
	mask := linkStreamObj("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ImageMask true /BitsPerComponent 1", "\x00")
	type3 := func(glyph int) string {
		return fmt.Sprintf("<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [0.001 0 0 0.001 0 0] "+
			"/CharProcs << /a %d 0 R >> /Encoding << /Type /Encoding /Differences [97 /a] >> /FirstChar 97 /LastChar 97 /Widths [1000] /Resources << >> >>", glyph)
	}
	bothRes := "<< /ColorSpace << " + patternCS + " >> /Pattern << /P1 5 0 R /P2 6 0 R >> >>"
	underRGB := "<< /ColorSpace << /CSp [/Pattern /DeviceRGB] >> /Pattern << /P2 5 0 R >> >>"
	shading := "<< /Shading << /Sh0 << /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 10 0] " +
		"/Function << /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /C1 [1 0 0] /N 1 >> >> >> >>"

	tests := []struct {
		name      string
		data      []byte
		rgb, gray int
	}{
		{"a coloured cell filling after rg", linkPagePDF("<< /Pattern << /P1 5 0 R >> >>", coloured, tiling(1, rgbCell, "<< >>")), 2, 0},
		{"a coloured cell before an uncoloured one", linkPagePDF(bothRes, coloured+" "+uncoloured,
			tiling(1, rgbCell, "<< >>"), tiling(2, "0 0 5 5 re f", "<< >>")), 0, 0},
		{"a coloured cell after an uncoloured one", linkPagePDF(bothRes, uncoloured+" "+coloured,
			tiling(1, rgbCell, "<< >>"), tiling(2, "0 0 5 5 re f", "<< >>")), 0, 0},
		{"an uncoloured pattern a form paints", linkPagePDF("<< /Pattern << /P1 5 0 R >> /XObject << /Fm0 6 0 R >> >>", coloured+" /Fm0 Do",
			tiling(1, rgbCell, "<< >>"), form(uncoloured, "<< /ColorSpace << "+patternCS+" >> /Pattern << /P2 7 0 R >> >>"),
			tiling(2, "0 0 5 5 re f", "<< >>")), 2, 0},
		{"a form a switched-off cell paints", linkPagePDF(bothRes, coloured+" "+uncoloured,
			tiling(1, "/Fm0 Do", "<< /XObject << /Fm0 7 0 R >> >>"), tiling(2, "0 0 5 5 re f", "<< >>"), form(rgbCell, "<< >>")), 0, 0},
		{"a form a coloured cell paints", linkPagePDF("<< /Pattern << /P1 5 0 R >> >>", coloured,
			tiling(1, "/Fm0 Do", "<< /XObject << /Fm0 6 0 R >> >>"), form(rgbCell, "<< >>")), 2, 0},
		{"a glyph a switched-off cell shows", linkPagePDF(bothRes, coloured+" "+uncoloured,
			tiling(1, "BT /T3 12 Tf (a) Tj ET", "<< /Font << /T3 7 0 R >> >>"), tiling(2, "0 0 5 5 re f", "<< >>"),
			type3(8), linkStreamObj("", "1000 0 d0 1 0 0 rg 0 0 1 1 re f")), 0, 0},
		{"a glyph a coloured cell shows", linkPagePDF("<< /Pattern << /P1 5 0 R >> >>", coloured,
			tiling(1, "BT /T3 12 Tf (a) Tj ET", "<< /Font << /T3 6 0 R >> >>"),
			type3(7), linkStreamObj("", "1000 0 d0 1 0 0 rg 0 0 1 1 re f")), 2, 1},
		{"an image mask in the cell after a fill's underlying space", linkPagePDF(underRGB, "/CSp cs 1 0 0 /P2 scn "+fill,
			tiling(2, "/Im0 Do", "<< /XObject << /Im0 6 0 R >> >>"), mask), 1, 0},
		{"an image mask in the cell after a stroke's underlying space", linkPagePDF(underRGB, "/CSp CS 1 0 0 /P2 SCN 0 0 10 10 re S",
			tiling(2, "/Im0 Do", "<< /XObject << /Im0 6 0 R >> >>"), mask), 0, 1},
		{"an image mask in a coloured cell after the underlying space", linkPagePDF(
			"<< /ColorSpace << /CSp [/Pattern /DeviceRGB] >> /Pattern << /P1 5 0 R /P2 6 0 R >> >>", "/CSp cs 1 0 0 /P2 scn "+fill+" "+coloured,
			tiling(1, "/Im0 Do", "<< /XObject << /Im0 7 0 R >> >>"), tiling(2, "0 0 5 5 re f", "<< >>"), mask), 1, 0},
		{"an inline image mask in an uncoloured cell", linkPagePDF(underRGB, "/CSp cs 1 0 0 /P2 scn "+fill,
			tiling(2, "BI /W 1 /H 1 /IM true ID \x00 EI", "<< >>")), 1, 0},
		{"an image in an uncoloured cell", linkPagePDF("<< /ColorSpace << "+patternCS+" >> /Pattern << /P2 5 0 R >> >>", uncoloured,
			tiling(2, "/Im0 Do", "<< /XObject << /Im0 6 0 R >> >>"),
			linkStreamObj("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8", "abc")), 1, 0},
		{"a shading in an uncoloured cell", linkPagePDF("<< /ColorSpace << "+patternCS+" >> /Pattern << /P2 5 0 R >> >>", uncoloured,
			tiling(2, "/Sh0 sh", shading)), 0, 0},
		{"a shading past a d1", linkPagePDF("<< /Font << /T3 5 0 R >> >>", "BT /T3 12 Tf (a) Tj ET",
			"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [0.001 0 0 0.001 0 0] /CharProcs << /a 6 0 R >> "+
				"/Encoding << /Type /Encoding /Differences [97 /a] >> /FirstChar 97 /LastChar 97 /Widths [1000] /Resources "+shading+" >>",
			linkStreamObj("", "1000 0 0 0 1 1 d1 /Sh0 sh")), 0, 1},
		{"scn naming no pattern", linkPagePDF("<< >>", "/Pattern cs /Pundef scn "+fill), 0, 2},
		{"scn with no name in a Pattern space", linkPagePDF("<< >>", "/Pattern cs 0.5 scn "+fill), 0, 2},
	}
	profiles := []struct {
		profile   models.PDFAProfile
		clause    string
		rgb, gray int
	}{
		{models.PDFA1b, "6.2.3.3", 1, 3},
		{models.PDFA2b, "6.2.4.3", 2, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, p := range profiles {
				if got := ruleContexts(t, tt.data, p.profile, p.clause, p.rgb); len(got) != tt.rgb {
					t.Errorf("%s: DeviceRGB %s t%d fired %d times, want %d", p.profile, p.clause, p.rgb, len(got), tt.rgb)
				}
				if got := ruleContexts(t, tt.data, p.profile, p.clause, p.gray); len(got) != tt.gray {
					t.Errorf("%s: DeviceGray %s t%d fired %d times, want %d", p.profile, p.clause, p.gray, len(got), tt.gray)
				}
			}
		})
	}
}
