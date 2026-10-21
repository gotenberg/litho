package page

import (
	"context"
	"fmt"
	"image"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// The text fixtures embed a fontbuild TrueType program whose glyphs are
// rectangles as wide as their advance and 500 units tall, so at 20 points A
// paints a 12 by 10 pixel box with its lower-left corner at the text origin
// and a space paints nothing. At 72 dpi user point (x, y) lands on device
// (x, 100 - y).

// textGlyphs are the program's glyphs: A 600 units wide, B 700, space empty.
var textGlyphs = []fontbuild.Glyph{
	{Name: "space", Unicode: ' ', Advance: 250, Empty: true},
	{Name: "A", Unicode: 'A', Advance: 600},
	{Name: "B", Unicode: 'B', Advance: 700},
}

// textProgram is the embedded TrueType program.
var textProgram = fontbuild.TrueType{PostScriptName: "Rect", Family: "Rect", Subfamily: "Regular", Glyphs: textGlyphs}.Build()

// trueTypeObjs returns objects 5 (a TrueType font, /F in textPage), 6 (its
// descriptor) and 7 (its program), with the widths of textGlyphs.
func trueTypeObjs() []string {
	widths := make([]string, 0, 'B'-' '+1)
	for c := ' '; c <= 'B'; c++ {
		w := 0
		switch c {
		case ' ':
			w = 250
		case 'A':
			w = 600
		case 'B':
			w = 700
		}
		widths = append(widths, fmt.Sprint(w))
	}
	return []string{
		"<< /Type /Font /Subtype /TrueType /BaseFont /Rect /FirstChar 32 /LastChar 66 /Widths [" +
			strings.Join(widths, " ") + "] /Encoding /WinAnsiEncoding /FontDescriptor 6 0 R >>",
		"<< /Type /FontDescriptor /FontName /Rect /Flags 32 /FontBBox [0 0 1000 500] /ItalicAngle 0 " +
			"/Ascent 500 /Descent 0 /CapHeight 500 /StemV 80 /FontFile2 7 0 R >>",
		stream(fmt.Sprintf("/Length1 %d", len(textProgram)), string(textProgram)),
	}
}

// textPage is the resources of a page showing text in /F.
const textPage = "/Resources << /Font << /F 5 0 R >> >>"

// drawText draws content over the TrueType font.
func drawText(t *testing.T, content string) (*rgbImage, Report) {
	t.Helper()
	img, report := draw(t, fixture{page: textPage, content: content, objs: trueTypeObjs()})
	return &rgbImage{t: t, img: img}, report
}

func TestTextFillAndAdvance(t *testing.T) {
	t.Parallel()
	// A at x 10..22, then B at 22..36, rows 40..49.
	img, report := drawText(t, "BT /F 20 Tf 10 50 Td (AB) Tj ET")
	img.expect(map[[2]int]rgb{
		{10, 40}: black, {21, 49}: black, {22, 45}: black, {35, 45}: black,
		{9, 45}: paper, {36, 45}: paper, {15, 39}: paper, {15, 50}: paper,
	})
	if len(report.Unsupported) != 0 {
		t.Errorf("report %v, want nothing left out", report.Unsupported)
	}
}

func TestTextPositionAdjustments(t *testing.T) {
	t.Parallel()
	// TJ -500 moves the second A right by 500/1000 x 20 = 10: it starts at
	// 10 + 12 + 10 = 32.
	img, _ := drawText(t, "BT /F 20 Tf 10 50 Td [(A) -500 (A)] TJ ET")
	img.expect(map[[2]int]rgb{{21, 45}: black, {25, 45}: paper, {31, 45}: paper, {32, 45}: black, {43, 45}: black, {44, 45}: paper})
	// Tc adds 3 after each glyph; Tz 50 halves every horizontal distance:
	// A is 6 wide and the spacing 1.5.
	img, _ = drawText(t, "BT /F 20 Tf 3 Tc 10 50 Td (AA) Tj ET")
	img.expect(map[[2]int]rgb{{21, 45}: black, {23, 45}: paper, {25, 45}: black, {36, 45}: black, {37, 45}: paper})
	img, _ = drawText(t, "BT /F 20 Tf 50 Tz 10 50 Td (AA) Tj ET")
	img.expect(map[[2]int]rgb{{15, 45}: black, {16, 45}: black, {21, 45}: black, {22, 45}: paper})
	// Ts raises the baseline by 10: rows 30..39.
	img, _ = drawText(t, "BT /F 20 Tf 10 Ts 10 50 Td (A) Tj ET")
	img.expect(map[[2]int]rgb{{15, 30}: black, {15, 39}: black, {15, 40}: paper, {15, 29}: paper})
}

func TestTextWordSpacing(t *testing.T) {
	t.Parallel()
	// The space advances 250/1000 x 20 = 5, plus Tw 10: the second A starts
	// at 10 + 12 + 5 + 10 = 37 (ISO 32000-1, 9.3.3).
	img, _ := drawText(t, "BT /F 20 Tf 10 Tw 10 50 Td (A A) Tj ET")
	img.expect(map[[2]int]rgb{{21, 45}: black, {30, 45}: paper, {36, 45}: paper, {37, 45}: black, {48, 45}: black})
	// " sets Tw and Tc, then moves down the leading and shows.
	img, _ = drawText(t, "BT /F 20 Tf 20 TL 10 70 Td 10 0 (A A) \" ET")
	img.expect(map[[2]int]rgb{{15, 45}: black, {37, 45}: black, {36, 45}: paper, {15, 25}: paper})
	// ' moves down the leading.
	img, _ = drawText(t, "BT /F 20 Tf 20 TL 10 70 Td (A) ' ET")
	img.expect(map[[2]int]rgb{{15, 45}: black, {15, 25}: paper})
}

func TestTextRenderingModes(t *testing.T) {
	t.Parallel()
	const show = "BT /F 50 Tf 10 30 Td (A) Tj ET"
	// At 50 points A spans x 10..40 and rows 45..69.
	cases := []struct {
		mode string
		want map[[2]int]rgb
	}{
		{"0", map[[2]int]rgb{{25, 57}: red, {10, 60}: red}},
		// Stroke only: a 2-unit pen on the outline, the inside unpainted.
		{"1", map[[2]int]rgb{{25, 57}: paper, {10, 57}: blue, {39, 57}: blue}},
		{"2", map[[2]int]rgb{{25, 57}: red, {10, 57}: blue}},
		{"3", map[[2]int]rgb{{25, 57}: paper, {10, 57}: paper}},
	}
	for _, c := range cases {
		img, _ := drawText(t, "1 0 0 rg 0 0 1 RG 2 w "+c.mode+" Tr "+show)
		img.expect(c.want)
	}
	// Mode 7 paints nothing and clips what follows to the glyph; ET applies
	// the clip (ISO 32000-1, 9.3.6).
	img, _ := drawText(t, "7 Tr "+show+" 0 g 0 0 100 100 re f")
	img.expect(map[[2]int]rgb{{25, 57}: black, {5, 57}: paper, {45, 57}: paper, {25, 40}: paper})
	// Mode 4 fills and clips; q/Q around the text object restores the clip.
	img, _ = drawText(t, "q 1 0 0 rg 4 Tr "+show+" 0 0 1 rg 0 0 100 100 re f Q 0 g 80 80 10 10 re f")
	img.expect(map[[2]int]rgb{{25, 57}: blue, {5, 57}: paper, {85, 15}: black})
	// A clipping mode with nothing shown leaves the clip alone.
	img, _ = drawText(t, "BT 7 Tr ET 0 g 0 0 100 100 re f")
	img.expect(map[[2]int]rgb{{5, 5}: black})
}

func TestTextHiddenAdvances(t *testing.T) {
	t.Parallel()
	// Invisible text still moves the text position (9.3.6, Table 106).
	img, _ := drawText(t, "BT /F 20 Tf 10 50 Td 3 Tr (A) Tj 0 Tr (A) Tj ET")
	img.expect(map[[2]int]rgb{{15, 45}: paper, {25, 45}: black})
}

// type3Objs returns a Type 3 font (object 5) whose glyph a paints the square
// [0 0 500 500] in glyph space with the given description prefix.
func type3Objs(proc string) []string {
	return []string{
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0] " +
			"/CharProcs << /a 6 0 R >> /Encoding << /Type /Encoding /Differences [97 /a] >> " +
			"/FirstChar 97 /LastChar 97 /Widths [1000] >>",
		stream("", proc),
	}
}

func TestType3ColouredAndUncoloured(t *testing.T) {
	t.Parallel()
	// At 20 points the glyph's square is 10 units: x 10..20, rows 40..49.
	const show = "0 0 1 rg BT /F 20 Tf 10 50 Td (aa) Tj ET"
	img, _ := draw(t, fixture{page: textPage, content: show, objs: type3Objs("1000 0 d0 1 0 0 rg 0 0 500 500 re f")})
	// d0: the description's own red; the second glyph 20 further.
	expect(t, img, 0, map[[2]int]rgb{{15, 45}: red, {35, 45}: red, {25, 45}: paper, {15, 52}: paper})
	img, _ = draw(t, fixture{page: textPage, content: show, objs: type3Objs("1000 0 0 0 500 500 d1 1 0 0 rg 0 0 500 500 re f")})
	// d1: the shape alone, in the fill colour current when it is shown.
	expect(t, img, 0, map[[2]int]rgb{{15, 45}: blue, {35, 45}: blue})
	// The description's colour stays inside it.
	img, _ = draw(t, fixture{page: textPage, content: show + " 0 0 50 10 re f", objs: type3Objs("1000 0 d0 1 0 0 rg 0 0 500 500 re f")})
	expect(t, img, 0, map[[2]int]rgb{{25, 95}: blue})
}

func TestType3RecursionIsBounded(t *testing.T) {
	t.Parallel()
	// The glyph shows itself: each level is drawn smaller, and the nesting
	// stops at maxType3Depth.
	objs := type3Objs("1000 0 d0 0 0 500 500 re f BT /F 1000 Tf 500 0 Td (a) Tj ET")
	objs[0] = strings.Replace(objs[0], ">>", "/Resources << /Font << /F 5 0 R >> >> >>", 1)
	img, report := draw(t, fixture{page: textPage, content: "0 g BT /F 20 Tf 10 50 Td (a) Tj ET", objs: objs})
	expect(t, img, 0, map[[2]int]rgb{{15, 45}: black})
	if report.Unsupported[Skipped] == 0 {
		t.Errorf("report %v, want the glyph past the nesting cap skipped", report.Unsupported)
	}
}

// identityVObjs returns a Type 0 font in vertical writing (object 5) over the
// TrueType program as a CIDFontType2 with an Identity /CIDToGIDMap: CID 2 is
// A, 600 wide, drawn with the default vertical metrics, position vector (300,
// 880) and displacement -1000 (ISO 32000-1, 9.7.4.3, /DW2).
func identityVObjs() []string {
	return []string{
		"<< /Type /Font /Subtype /Type0 /BaseFont /Rect /Encoding /Identity-V /DescendantFonts [8 0 R] >>",
		"<< /Type /FontDescriptor /FontName /Rect /Flags 4 /FontBBox [0 0 1000 500] /ItalicAngle 0 " +
			"/Ascent 500 /Descent 0 /CapHeight 500 /StemV 80 /FontFile2 7 0 R >>",
		stream(fmt.Sprintf("/Length1 %d", len(textProgram)), string(textProgram)),
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Rect /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> " +
			"/FontDescriptor 6 0 R /DW 1000 /W [2 [600]] /CIDToGIDMap /Identity >>",
	}
}

func TestVerticalText(t *testing.T) {
	t.Parallel()
	// At 20 points v = (6, 17.6): the first A spans x 44..56 and y
	// 62.4..72.4, rows 27.6..37.6; the next is 20 lower, rows 47.6..57.6.
	img, _ := draw(t, fixture{page: textPage, content: "0 g BT /F 20 Tf 50 80 Td <00020002> Tj ET", objs: identityVObjs()})
	expect(t, img, 0, map[[2]int]rgb{
		{50, 32}: black, {50, 52}: black, {50, 42}: paper,
		{45, 32}: black, {42, 32}: paper, {57, 32}: paper,
	})
	// A TJ adjustment is subtracted from the vertical coordinate in
	// vertical writing (9.4.3, Table 109): 500 at 20 points moves the next
	// glyph 10 further down, rows 57.6..67.6.
	img, _ = draw(t, fixture{page: textPage, content: "0 g BT /F 20 Tf 50 80 Td [<0002> 500 <0002>] TJ ET", objs: identityVObjs()})
	expect(t, img, 0, map[[2]int]rgb{{50, 32}: black, {50, 52}: paper, {50, 62}: black})
}

func TestTextWithoutFont(t *testing.T) {
	t.Parallel()
	// A font resource that does not exist shows nothing and fails nothing.
	img, report := drawText(t, "BT /Missing 20 Tf 10 50 Td (A) Tj ET")
	img.expect(map[[2]int]rgb{{15, 45}: paper})
	if report.Unsupported[Skipped] == 0 {
		t.Errorf("report %v, want the show skipped", report.Unsupported)
	}
}

// BenchmarkRenderText draws a page of 60 lines of 40 glyphs at 150 dpi, the
// glyph loop's hot path: resolution through the memo, rasterisation and
// painting.
func BenchmarkRenderText(b *testing.B) {
	var sb strings.Builder
	sb.WriteString("BT 0 g /F 1.2 Tf 2 98 Td 1.5 TL ")
	for range 60 {
		sb.WriteString("(ABAB ABAB ABAB ABAB ABAB ABAB ABAB ABAB) ' ")
	}
	sb.WriteString("ET")
	f := fixture{page: textPage, content: sb.String(), objs: trueTypeObjs()}
	doc, pg := open(b, f.bytes(), limits.Limits{})
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := Render(context.Background(), doc, pg, Options{DPI: 150, Background: white}); err != nil {
			b.Fatal(err)
		}
	}
}

// rgbImage pairs a rendering with its test for terse pixel checks.
type rgbImage struct {
	t   *testing.T
	img *image.NRGBA
}

// expect checks pixels exactly.
func (i *rgbImage) expect(want map[[2]int]rgb) {
	i.t.Helper()
	expect(i.t, i.img, 0, want)
}
