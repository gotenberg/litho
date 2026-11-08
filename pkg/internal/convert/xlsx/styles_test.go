package xlsx

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// testTheme is a theme whose dk1 is black, lt1 white and accent1
// 4472C4.
const testTheme = `<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:themeElements><a:clrScheme name="t">` +
	`<a:dk1><a:sysClr val="windowText" lastClr="000000"/></a:dk1><a:lt1><a:sysClr val="window" lastClr="FFFFFF"/></a:lt1>` +
	`<a:dk2><a:srgbClr val="44546A"/></a:dk2><a:lt2><a:srgbClr val="E7E6E6"/></a:lt2><a:accent1><a:srgbClr val="4472C4"/></a:accent1>` +
	`</a:clrScheme></a:themeElements></a:theme>`

// styledBook returns a workbook whose styles are fonts, fills, borders
// and cellXfs (each the content of its table, the cell formats after the
// Normal one), with one cell A1, B1... per cell format past the first.
func styledBook(t *testing.T, numFmts, fonts, fills, borders, styleXfs, cellXfs string, n int) *sheet.Document {
	t.Helper()
	styles := `<numFmts>` + numFmts + `</numFmts><fonts><font><sz val="11"/><name val="Test Sans"/></font>` + fonts + `</fonts>` +
		`<fills><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill>` + fills + `</fills>` +
		`<borders><border/>` + borders + `</borders>` +
		`<cellStyleXfs><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/>` + styleXfs + `</cellStyleXfs>` +
		`<cellXfs><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` + cellXfs + `</cellXfs>` +
		`<cellStyles><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>`
	var row strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&row, `<c r="%s1" s="%d"><v>1</v></c>`, string(rune('A'+i-1)), i)
	}
	files := workbookFiles([]testSheet{{name: "S", xml: `<sheetData><row r="1">` + row.String() + `</row></sheetData>`}}, styles, "", "", map[string]string{"xl/theme/theme1.xml": testTheme})
	doc, _ := readBytes(t, zipFiles(t, files))
	return doc
}

// styleOf returns the style of the cell of sh at column col of row 0.
func styleOf(t *testing.T, doc *sheet.Document, col int) *sheet.Style {
	t.Helper()
	c, ok := cellAt(doc.Sheets[0], 0, col)
	if !ok || c.Style == nil {
		t.Fatalf("no styled cell at column %d", col)
	}
	return c.Style
}

// near reports whether two colours agree to a 255th.
func near(a, b css.Color) bool {
	return math.Abs(a.R-b.R) < 1.0/255 && math.Abs(a.G-b.G) < 1.0/255 && math.Abs(a.B-b.B) < 1.0/255 && math.Abs(a.A-b.A) < 1e-9
}

// TestRead_NumberFormats checks the built-in ids as Calc's en-US codes,
// a workbook's own codes and the General fallback.
func TestRead_NumberFormats(t *testing.T) {
	t.Parallel()
	doc := styledBook(t, `<numFmt numFmtId="164" formatCode="0.000&quot;kg&quot;"/><numFmt numFmtId="3" formatCode="00"/>`, "", "", "", "",
		`<xf numFmtId="14"/><xf numFmtId="22"/><xf numFmtId="164"/><xf numFmtId="3"/><xf numFmtId="99"/><xf numFmtId="49"/>`, 6)
	for i, want := range []string{"m/d/yyyy", "m/d/yyyy h:mm", `0.000"kg"`, "00", "General", "@"} {
		if got := styleOf(t, doc, i).Format; got != want {
			t.Errorf("format %d = %q, want %q", i, got, want)
		}
	}
	if doc.Default.Format != "General" {
		t.Errorf("default format %q", doc.Default.Format)
	}
}

// TestRead_Fonts checks the font records: LibreOffice's Cambria 11 for
// what a record leaves out, the attributes, the underline kinds and the
// colours by rgb, theme with tint and palette index.
func TestRead_Fonts(t *testing.T) {
	t.Parallel()
	doc := styledBook(t, "",
		`<font><b/></font>`+
			`<font><i val="1"/><strike/><u val="doubleAccounting"/><vertAlign val="superscript"/><sz val="9"/><name val="Arial"/><color rgb="00FF0000"/></font>`+
			`<font><u/><color theme="4" tint="-0.5"/><name val="Arial"/></font>`+
			`<font><u val="singleAccounting"/><color indexed="17"/><b val="0"/></font>`+
			`<font><color theme="0" tint="0.5"/><color auto="1"/></font>`,
		"", "", "", `<xf fontId="1"/><xf fontId="2"/><xf fontId="3"/><xf fontId="4"/><xf fontId="5"/>`, 5)
	for i, want := range []sheet.Font{
		{Families: []string{"Cambria"}, Size: 11, Bold: true},
		{Families: []string{"Arial"}, Size: 9, Italic: true, Strike: true, Underline: sheet.Double, Script: sheet.Superscript, Color: css.Color{R: 1, A: 1}},
		{Families: []string{"Arial"}, Size: 11, Underline: sheet.Single, Color: tint(rgb(0x4472C4), -0.5)},
		{Families: []string{"Cambria"}, Size: 11, Underline: sheet.Single, Color: rgb(0x008000)},
		{Families: []string{"Cambria"}, Size: 11},
	} {
		got := styleOf(t, doc, i).Font
		if got.Families[0] != want.Families[0] || got.Size != want.Size || got.Bold != want.Bold || got.Italic != want.Italic ||
			got.Strike != want.Strike || got.Underline != want.Underline || got.Script != want.Script || !near(got.Color, want.Color) {
			t.Errorf("font %d = %+v, want %+v", i, got, want)
		}
	}
	if got := styleOf(t, doc, 2).Font.Color; !near(got, rgb(0x203864)) {
		t.Errorf("accent1 darkened by half = %+v, want 203864", got)
	}
	if f := doc.Default.Font; f.Families[0] != "Test Sans" || f.Size != 11 {
		t.Errorf("default font %+v", f)
	}
}

// TestRead_Fills checks solid fills, a pattern mixed into its background
// in its share and a gradient as its first two stops mixed.
func TestRead_Fills(t *testing.T) {
	t.Parallel()
	doc := styledBook(t, "", "",
		`<fill><patternFill patternType="solid"><fgColor rgb="FFDDEEFF"/><bgColor indexed="64"/></patternFill></fill>`+
			`<fill><patternFill patternType="lightGray"><fgColor rgb="FF000000"/><bgColor rgb="FFFFFFFF"/></patternFill></fill>`+
			`<fill><patternFill><fgColor rgb="FF00FF00"/></patternFill></fill>`+
			`<fill><gradientFill degree="90"><stop position="0"><color rgb="FFFF0000"/></stop><stop position="1"><color rgb="FF0000FF"/></stop></gradientFill></fill>`,
		"", "", `<xf fillId="2"/><xf fillId="3"/><xf fillId="4"/><xf fillId="5"/><xf fillId="1"/>`, 5)
	for i, want := range []css.Color{
		rgb(0xDDEEFF),
		rgb(0xBFBFBF),
		{},
		rgb(0x80007F),
		rgb(0xDFDFDF),
	} {
		if got := styleOf(t, doc, i).Fill.Color; !near(got, want) {
			t.Errorf("fill %d = %+v, want %+v", i, got, want)
		}
	}
}

// TestRead_Borders checks the named border styles resolved to Calc's
// widths, the colours and the diagonals.
func TestRead_Borders(t *testing.T) {
	t.Parallel()
	doc := styledBook(t, "", "", "",
		`<border diagonalUp="1"><left style="thin"><color rgb="FFFF0000"/></left><right style="medium"/><top style="double"/><bottom style="hair"/><diagonal style="dashed"/></border>`+
			`<border><start style="thick"/><end style="dotted"/><top style="mediumDashDot"/><bottom style="slantDashDot"/><diagonal style="thin"/></border>`,
		"", `<xf borderId="1"/><xf borderId="2"/>`, 2)
	a := styleOf(t, doc, 0)
	for side, want := range map[css.Side]sheet.Border{
		css.Left:   {Line: sheet.LineSolid, Width: 0.75, Color: css.Color{R: 1, A: 1}},
		css.Right:  {Line: sheet.LineSolid, Width: 1.75, Color: css.Color{A: 1}},
		css.Top:    {Line: sheet.LineDouble, Width: 1.75, Gap: 0.75, Color: css.Color{A: 1}},
		css.Bottom: {Line: sheet.LineSolid, Width: 0.1, Color: css.Color{A: 1}},
	} {
		if a.Border[side] != want {
			t.Errorf("side %d = %+v, want %+v", side, a.Border[side], want)
		}
	}
	if !a.DiagonalUp || a.DiagonalDown || a.Diagonal.Line != sheet.LineDashed || a.Diagonal.Width != 0.737 {
		t.Errorf("diagonal %+v up %v down %v", a.Diagonal, a.DiagonalUp, a.DiagonalDown)
	}
	b := styleOf(t, doc, 1)
	if b.Border[css.Left].Width != 2.5 || b.Border[css.Right].Line != sheet.LineDotted || b.Border[css.Top] != (sheet.Border{Line: sheet.LineDashDot, Width: 1.757, Color: css.Color{A: 1}}) || b.Border[css.Bottom].Line != sheet.LineDashed {
		t.Errorf("borders %+v", b.Border)
	}
	if b.DiagonalUp || b.DiagonalDown {
		t.Error("a diagonal without direction is drawn")
	}
}

// TestRead_Alignment checks the alignment as LibreOffice imports it: a
// rotated or stacked cell without a horizontal alignment aligns left when
// turned up to 89 degrees and right past them, stacked included.
func TestRead_Alignment(t *testing.T) {
	t.Parallel()
	doc := styledBook(t, "", "", "", "", "",
		`<xf><alignment horizontal="centerContinuous" vertical="center" wrapText="1" indent="2"/></xf>`+
			`<xf><alignment textRotation="45"/></xf>`+
			`<xf><alignment textRotation="135"/></xf>`+
			`<xf><alignment textRotation="255" vertical="justify" shrinkToFit="1" readingOrder="2"/></xf>`+
			`<xf><alignment horizontal="distributed" vertical="distributed"/></xf>`, 5)
	for i, want := range []sheet.Alignment{
		{Horizontal: sheet.HCenter, Vertical: sheet.VMiddle, Wrap: true, Indent: 2 * 3 * 5.5},
		{Horizontal: sheet.HLeft, Rotation: 45},
		{Horizontal: sheet.HRight, Rotation: -45},
		{Horizontal: sheet.HRight, Stacked: true, Vertical: sheet.VJustify, Wrap: true, Shrink: true, RightToLeft: true},
		{Horizontal: sheet.HDistributed, Vertical: sheet.VDistributed, Wrap: true},
	} {
		if got := styleOf(t, doc, i).Align; got != want {
			t.Errorf("alignment %d = %+v, want %+v", i, got, want)
		}
	}
	if p := styleOf(t, doc, 0).Padding; p != [4]float64{1, 1, 1, 1} {
		t.Errorf("padding %v, want a point a side", p)
	}
}

// TestRead_StyleRecords checks that a cell format takes what it leaves
// out from its cell style record, and its own alignment whatever its
// apply flags say.
func TestRead_StyleRecords(t *testing.T) {
	t.Parallel()
	doc := styledBook(t, "", `<font><b/><name val="Arial"/></font>`, `<fill><patternFill patternType="solid"><fgColor rgb="FF123456"/></patternFill></fill>`, "",
		`<xf numFmtId="10" fontId="1" fillId="2"><alignment horizontal="right"/></xf>`,
		`<xf xfId="1"/><xf xfId="1" fontId="0" applyFont="0"/>`, 2)
	a, b := styleOf(t, doc, 0), styleOf(t, doc, 1)
	if a.Format != "0.00%" || !a.Font.Bold || !near(a.Fill.Color, rgb(0x123456)) || a.Align.Horizontal != sheet.HGeneral {
		t.Errorf("inherited style %+v", a)
	}
	if b.Font.Bold || b.Format != "0.00%" {
		t.Errorf("own font %+v format %q, want the cell format's font", b.Font, b.Format)
	}
}

// TestRead_SystemThemeColors checks that a theme colour naming a system
// colour reads as the system's, black window text and white window,
// whatever its lastClr, with its tint.
func TestRead_SystemThemeColors(t *testing.T) {
	t.Parallel()
	theme := strings.NewReplacer(`lastClr="000000"`, `lastClr="00FF00"`, `lastClr="FFFFFF"`, `lastClr="FF0000"`).Replace(testTheme)
	styles := `<fonts><font><sz val="11"/><name val="Test Sans"/></font><font><color theme="1"/></font><font><color theme="0" tint="-0.5"/></font><font><color theme="4"/></font></fonts>` +
		`<cellStyleXfs><xf numFmtId="0" fontId="0"/></cellStyleXfs><cellXfs><xf fontId="0"/><xf fontId="1"/><xf fontId="2"/><xf fontId="3"/></cellXfs>`
	files := workbookFiles([]testSheet{{name: "S", xml: `<sheetData><row r="1"><c r="A1" s="1"><v>1</v></c><c r="B1" s="2"><v>1</v></c><c r="C1" s="3"><v>1</v></c></row></sheetData>`}},
		styles, "", "", map[string]string{"xl/theme/theme1.xml": theme})
	doc, _ := readBytes(t, zipFiles(t, files))
	for i, want := range []css.Color{{A: 1}, tint(css.Color{R: 1, G: 1, B: 1, A: 1}, -0.5), rgb(0x4472C4)} {
		if got := styleOf(t, doc, i).Font.Color; !near(got, want) {
			t.Errorf("column %d colour %v, want %v", i, got, want)
		}
	}
}

// TestRead_SubstituteFamily checks that a Normal font no installed face
// serves measures its columns in Noto Sans, LibreOffice's substitute.
func TestRead_SubstituteFamily(t *testing.T) {
	t.Parallel()
	fonts := pinnedFonts(t)
	width := func(name string) float64 {
		styles := strings.Replace(minimalStyles, "Test Sans", name, 1)
		data := zipFiles(t, workbookFiles([]testSheet{{name: "S", xml: `<sheetData/>`}}, styles, "", "", nil))
		doc, _, err := load(context.Background(), bytes.NewReader(data), fonts, office.Options{})
		if err != nil {
			t.Fatal(err)
		}
		return doc.Sheets[0].DefaultColumnWidth
	}
	if unknown, noto := width("Aptos Narrow"), width("Noto Sans"); unknown != noto {
		t.Errorf("an unknown family's columns are %v points, Noto Sans's %v", unknown, noto)
	}
}
