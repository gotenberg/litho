package xlsx

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestRead_ColumnsAndRows checks the column widths in digits of the
// Normal font as Excel prints them (Test Sans at 11.04 points, 92 printer
// pixels: digits of 46 pixels, 5.52 points, and of 7 screen pixels), the
// default width and height of the format properties and of a sheet
// without them, the rows' heights scaled as Excel prints them, flags and
// styles. Test Sans rises 0.8 em and falls 0.2: its standard row is 100
// printer pixels (8, 74 and 18) and 17 screen ones (2, 12 and 3).
func TestRead_ColumnsAndRows(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name               string
		xml                string
		defWidth, defHeigh float64
	}{
		// 8 digits of 7 screen pixels and 5 of padding, 64 pixels, 9.140625
		// characters of 46 printer pixels; the standard row of 100 printer
		// pixels for a default within 2 screen pixels of its 17 without
		// customHeight, 13.5 points being 18.
		{"no format properties", `<sheetData/>`, 50.4, 12},
		{"default near the standard", `<sheetFormatPr defaultRowHeight="13.5"/><sheetData/>`, 50.4, 12},
		// 20 screen pixels times 100 over 17, 117.6 printer pixels.
		{"base width", `<sheetFormatPr baseColWidth="10" defaultRowHeight="15"/><sheetData/>`, 63.12, 14.04},
		{"default width", `<sheetFormatPr baseColWidth="10" defaultColWidth="12" defaultRowHeight="20.5"/><sheetData/>`, 66.24, 19.2},
		{"default base", `<sheetFormatPr defaultRowHeight="0"/><sheetData/>`, 50.4, 12},
		// 27.33 screen pixels times 100 over 17, 160.78 printer pixels.
		{"custom default", `<sheetFormatPr defaultRowHeight="20.5" customHeight="1"/><sheetData/>`, 50.4, 19.2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := readBytes(t, oneSheet(t, tc.xml))
			sh := doc.Sheets[0]
			if math.Abs(sh.DefaultColumnWidth-tc.defWidth) > 1e-9 || math.Abs(sh.DefaultRowHeight-tc.defHeigh) > 1e-9 {
				t.Errorf("defaults %v by %v, want %v by %v", sh.DefaultColumnWidth, sh.DefaultRowHeight, tc.defWidth, tc.defHeigh)
			}
		})
	}
	doc, _ := readBytes(t, oneSheet(t, `<cols><col min="3" max="4" width="10" customWidth="1"/><col min="1" max="1" width="2"/>`+
		`<col min="4" max="6" hidden="1"/><col min="8" max="8" style="0"/><col min="9" max="9"/></cols>`+
		`<sheetData><row r="2" ht="30" customHeight="1"><c r="A2"><v>1</v></c></row><row r="3" ht="40"><c r="A3"><v>1</v></c></row><row r="5" hidden="1" s="0" customFormat="1"/>`+
		`<row r="7" s="0"/><row><c><v>1</v></c><c r="1_1"><v>2</v></c><c r="E8"/><c r="E8" t="b"><v>1</v></c></row></sheetData>`))
	sh := doc.Sheets[0]
	want := []sheet.Column{{First: 0, Last: 0, Width: 11.04}, {First: 2, Last: 3, Width: 55.2}, {First: 4, Last: 5, Hidden: true}, {First: 7, Last: 7}}
	if len(sh.Columns) != len(want) {
		t.Fatalf("columns %+v, want %+v", sh.Columns, want)
	}
	for i, c := range sh.Columns {
		if c.First != want[i].First || c.Last != want[i].Last || math.Abs(c.Width-want[i].Width) > 1e-9 || c.Hidden != want[i].Hidden {
			t.Errorf("column %d = %+v, want %+v", i, c, want[i])
		}
	}
	if sh.Columns[3].Style == nil {
		t.Error("a column style is lost")
	}
	var idx []int
	for _, r := range sh.Rows {
		idx = append(idx, r.Index)
	}
	if !slices.Equal(idx, []int{1, 2, 4, 7}) {
		t.Fatalf("rows %v, want 1, 2, 4 and 7", idx)
	}
	// 40 and 53.33 screen pixels times 100 over 17, truncated: 235 and 313
	// printer pixels.
	if r := sh.Rows[0]; math.Abs(r.Height-28.2) > 1e-9 || !r.CustomHeight {
		t.Errorf("row 2 = %+v", r)
	}
	if r := sh.Rows[1]; math.Abs(r.Height-37.56) > 1e-9 || r.CustomHeight {
		t.Errorf("row 3 = %+v, want the stored height of a row without a custom one", r)
	}
	if r := sh.Rows[2]; !r.Hidden || r.Style == nil {
		t.Errorf("row 5 = %+v", r)
	}
	if r := sh.Rows[3]; len(r.Cells) != 3 || r.Cells[0].Column != 0 || r.Cells[1].Column != 1 || r.Cells[2].Value.Kind != sheet.Bool {
		t.Errorf("row 8 cells %+v, want A8, B8 and the boolean E8", r.Cells)
	}
}

// TestRead_CellValues checks every cell type, the cached value of a
// formula, the cells without value but a style, the merges, the right to
// left view and the warnings of what is not applied.
func TestRead_CellValues(t *testing.T) {
	t.Parallel()
	doc, _ := readBytes(t, oneSheet(t, `<sheetViews><sheetView rightToLeft="1" showZeros="0" workbookViewId="0"/></sheetViews><sheetData><row r="1">`+
		`<c r="A1"><v> 12.5 </v></c><c r="B1" t="n"><f>A1*2</f><v>25</v></c><c r="C1"><f>SUM(A1)</f><v></v></c>`+
		`<c r="D1" t="str"><f>"a"</f><v>text_x0021_</v></c><c r="E1" t="b"><v>1</v></c><c r="F1" t="b"><v>0</v></c>`+
		`<c r="G1" t="e"><v>#DIV/0!</v></c><c r="H1" t="d"><v>2025-01-21T13:30:00</v></c><c r="I1" t="inlineStr"><is><t>in</t><t>line</t></is></c>`+
		`<c r="J1" s="0"/><c r="K1"/><c r="L1"><v>x</v></c></row></sheetData>`+
		`<mergeCells><mergeCell ref="A3:C4"/><mergeCell ref="E5"/></mergeCells><conditionalFormatting sqref="A1"><cfRule type="cellIs" priority="1"/></conditionalFormatting>`))
	sh := doc.Sheets[0]
	for _, tc := range []struct {
		col  int
		want sheet.Value
	}{
		{0, sheet.Value{Kind: sheet.Number, Number: 12.5}},
		{1, sheet.Value{Kind: sheet.Number, Number: 25}},
		{3, sheet.Value{Kind: sheet.Text, Text: "text!"}},
		{4, sheet.Value{Kind: sheet.Bool, Number: 1}},
		{5, sheet.Value{Kind: sheet.Bool}},
		{6, sheet.Value{Kind: sheet.Error, Text: "#DIV/0!"}},
		{7, sheet.Value{Kind: sheet.Number, Number: 45678.5625}},
		{8, sheet.Value{Kind: sheet.Text, Text: "inline"}},
		{9, sheet.Value{}},
	} {
		c, ok := cellAt(sh, 0, tc.col)
		if !ok || c.Value != tc.want {
			t.Errorf("column %d = %+v (%v), want %+v", tc.col, c.Value, ok, tc.want)
		}
	}
	for _, col := range []int{10, 11} {
		if c, ok := cellAt(sh, 0, col); ok {
			t.Errorf("column %d kept: %+v", col, c)
		}
	}
	// A formula saved with an empty result is kept for the builder.
	if c, ok := cellAt(sh, 0, 2); !ok || c.Formula != "SUM(A1)" {
		t.Errorf("column 2 = %+v (%v), want the formula", c, ok)
	}
	if !sh.RightToLeft || sh.HideZeros {
		t.Errorf("right to left %v, hide zeros %v: want right to left, zeros printed", sh.RightToLeft, sh.HideZeros)
	}
	if len(sh.Merges) != 1 || sh.Merges[0] != (sheet.Range{FirstRow: 2, FirstColumn: 0, LastRow: 3, LastColumn: 2}) {
		t.Errorf("merges %+v", sh.Merges)
	}
	if len(sh.Conditional) != 1 || len(sh.Conditional[0].Rules) != 1 {
		t.Errorf("conditional formats %+v", sh.Conditional)
	}
}

// TestRead_EmptyStrings keeps a cell that stores an empty string, which
// Excel counts in the range it prints, as a cell without a value marked
// EmptyText; a cell without a value, a formula or a style is not kept.
func TestRead_EmptyStrings(t *testing.T) {
	t.Parallel()
	doc, _ := readBytes(t, oneSheet(t, `<sheetData><row r="1"><c r="A1"><v>1</v></c>`+
		`<c r="B1" t="inlineStr"><is><t></t></is></c><c r="C1" t="str"><v></v></c><c r="D1"/></row></sheetData>`))
	sh := doc.Sheets[0]
	for _, col := range []int{1, 2} {
		if c, ok := cellAt(sh, 0, col); !ok || !c.EmptyText || c.Value.Kind != sheet.Empty {
			t.Errorf("column %d = %+v (%v), want an empty string", col, c, ok)
		}
	}
	if c, ok := cellAt(sh, 0, 3); ok {
		t.Errorf("column 3 kept: %+v", c)
	}
}

// TestRead_Hyperlinks checks external links through their relationships
// on the cells of their range, and the places in the workbook as # and
// the location.
func TestRead_Hyperlinks(t *testing.T) {
	t.Parallel()
	files := workbookFiles([]testSheet{{
		name: "S",
		xml: `<sheetData><row r="1"><c r="A1"><v>1</v></c><c r="B1"><v>2</v></c><c r="C1"><v>3</v></c></row></sheetData>` +
			`<hyperlinks><hyperlink ref="A1:B1" r:id="rIdL" location="top"/><hyperlink ref="C1" location="'S'!A1"/></hyperlinks>`,
		rels: `<Relationship Id="rIdL" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com/a" TargetMode="External"/>`,
	}}, minimalStyles, "", "", nil)
	doc, _ := readBytes(t, zipFiles(t, files))
	sh := doc.Sheets[0]
	for col, want := range []string{"https://example.com/a#top", "https://example.com/a#top", "#'S'!A1"} {
		if c, _ := cellAt(sh, 0, col); c.Href != want {
			t.Errorf("column %d links %q, want %q", col, c.Href, want)
		}
	}
}

// TestRead_HyperlinkFormulas links a cell whose formula calls HYPERLINK
// with a string literal for its link_location (ECMA-376 Part 1,
// 18.17.7.170), and leaves a computed location or another formula
// unlinked.
func TestRead_HyperlinkFormulas(t *testing.T) {
	t.Parallel()
	cell := func(ref, f string) string {
		return `<c r="` + ref + `" t="str"><f>` + f + `</f><v>x</v></c>`
	}
	files := workbookFiles([]testSheet{{
		name: "S",
		xml: `<sheetData><row r="1">` +
			cell("A1", `HYPERLINK(&quot;https://example.com/a&quot;,&quot;Label&quot;)`) +
			cell("B1", `hyperlink( &quot;#S!A1&quot; )`) +
			cell("C1", `HYPERLINK(&quot;https://example.com/&quot;&amp;A2,&quot;Label&quot;)`) +
			cell("D1", `HYPERLINK(A2)`) +
			cell("E1", `UPPER(&quot;https://example.com/&quot;)`) +
			cell("F1", `HYPERLINK(&quot;https://example.com/q?&quot;&quot;x&quot;&quot;&quot;)`) +
			`</row></sheetData>`,
	}}, minimalStyles, "", "", nil)
	doc, _ := readBytes(t, zipFiles(t, files))
	for col, want := range []string{"https://example.com/a", "#S!A1", "", "", "", `https://example.com/q?"x"`} {
		if c, _ := cellAt(doc.Sheets[0], 0, col); c.Href != want {
			t.Errorf("column %d links %q, want %q", col, c.Href, want)
		}
	}
}

// TestRead_HyperlinksWholeColumns checks hyperlinks over whole columns:
// they mark the cells the sheet holds, and the reading follows those
// cells, not the million rows of each link's range.
func TestRead_HyperlinksWholeColumns(t *testing.T) {
	t.Parallel()
	links := strings.Repeat(`<hyperlink ref="A:A" location="top"/>`, 20_000)
	doc, _ := readBytes(t, oneSheet(t, `<sheetData><row r="1048576"><c r="A1048576"><v>1</v></c></row></sheetData><hyperlinks>`+links+`</hyperlinks>`))
	if c, _ := cellAt(doc.Sheets[0], 1048575, 0); c.Href != "#top" {
		t.Errorf("last cell links %q, want #top", c.Href)
	}
}

// TestRead_PageSetup checks the page setup over Excel's defaults.
func TestRead_PageSetup(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		xml   string
		check func(p sheet.PageSetup) bool
	}{
		{"defaults", `<sheetData/>`, func(p sheet.PageSetup) bool {
			// Excel's Normal margins: 0.7 inch at the sides, 0.75 at the top
			// and bottom, 0.3 for the header.
			return p.Width == 612 && p.Height == 792 && p.Scale == 100 && math.Abs(p.Margin[3]-50.4) < 0.01 && math.Abs(p.Margin[0]-54) < 0.01 && math.Abs(p.HeaderTop-21.6) < 0.01
		}},
		{"margins", `<sheetData/><pageMargins left="0.25" right="0.5" top="1" bottom="2" header="0.3" footer="0"/>`, func(p sheet.PageSetup) bool {
			return p.Margin == [4]float64{72, 36, 144, 18} && math.Abs(p.HeaderTop-21.6) < 1e-9 && p.FooterBottom == 0
		}},
		{"A4 landscape scaled", `<sheetData/><pageSetup paperSize="9" orientation="landscape" scale="5" pageOrder="overThenDown" firstPageNumber="7" useFirstPageNumber="1"/>`, func(p sheet.PageSetup) bool {
			return math.Abs(p.Width-841.89) < 0.01 && math.Abs(p.Height-595.28) < 0.01 && p.Scale == 10 && p.Order == sheet.OverThenDown && p.FirstPageNumber == 7
		}},
		{"first page number unused", `<sheetData/><pageSetup firstPageNumber="7"/>`, func(p sheet.PageSetup) bool { return p.FirstPageNumber == 0 }},
		{"fit to pages", `<sheetPr><pageSetUpPr fitToPage="1"/></sheetPr><sheetData/><pageSetup fitToWidth="1" fitToHeight="0" scale="50"/>`, func(p sheet.PageSetup) bool {
			return p.Scale == 0 && p.FitWidth == 1 && p.FitHeight == 0
		}},
		{"fit without setup", `<sheetPr><pageSetUpPr fitToPage="1"/></sheetPr><sheetData/>`, func(p sheet.PageSetup) bool {
			return p.Scale == 0 && p.FitWidth == 1 && p.FitHeight == 1
		}},
		{"custom paper", `<sheetData/><pageSetup paperSize="9" paperWidth="100mm" paperHeight="5in"/>`, func(p sheet.PageSetup) bool {
			return math.Abs(p.Width-283.46) < 0.01 && p.Height == 360
		}},
		{"printer defaults", `<sheetData/><pageSetup paperSize="1" usePrinterDefaults="0"/>`, func(p sheet.PageSetup) bool { return math.Abs(p.Width-595.28) < 0.01 }},
		{"unknown paper", `<sheetData/><pageSetup paperSize="0"/>`, func(p sheet.PageSetup) bool { return math.Abs(p.Width-595.28) < 0.01 }},
		{"letter", `<sheetData/><pageSetup paperSize="1"/>`, func(p sheet.PageSetup) bool { return p.Width == 612 }},
		{"draft", `<sheetData/><pageSetup draft="1"/>`, func(p sheet.PageSetup) bool { return p.Draft }},
		{"not draft", `<sheetData/><pageSetup draft="0"/>`, func(p sheet.PageSetup) bool { return !p.Draft }},
		{"errors displayed", `<sheetData/><pageSetup errors="displayed"/>`, func(p sheet.PageSetup) bool { return p.Errors == sheet.ErrorsDisplayed }},
		{"errors blank", `<sheetData/><pageSetup errors="blank"/>`, func(p sheet.PageSetup) bool { return p.Errors == sheet.ErrorsBlank }},
		{"errors dash", `<sheetData/><pageSetup errors="dash"/>`, func(p sheet.PageSetup) bool { return p.Errors == sheet.ErrorsDash }},
		{"errors NA", `<sheetData/><pageSetup errors="NA"/>`, func(p sheet.PageSetup) bool { return p.Errors == sheet.ErrorsNA }},
		{"print options", `<sheetData/><printOptions gridLines="1" headings="true" horizontalCentered="1" verticalCentered="1"/>`, func(p sheet.PageSetup) bool {
			return p.GridLines && p.Headings && p.CenterHorizontally && p.CenterVertically
		}},
		{"breaks", `<sheetData/><rowBreaks count="3"><brk id="20" man="1"/><brk id="5" man="1"/><brk id="9"/><brk id="0" man="1"/></rowBreaks><colBreaks><brk id="3" max="1048575" man="1"/></colBreaks>`, func(p sheet.PageSetup) bool {
			return slices.Equal(p.RowBreaks, []int{5, 20}) && slices.Equal(p.ColumnBreaks, []int{3})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := readBytes(t, oneSheet(t, tc.xml))
			if p := doc.Sheets[0].Page; !tc.check(p) {
				t.Errorf("page setup %+v", p)
			}
		})
	}
}

// hasWarning reports whether a warning's message holds s.
func hasWarning(warnings []typeset.Warning, s string) bool {
	for _, w := range warnings {
		if strings.Contains(w.Message, s) {
			return true
		}
	}
	return false
}

// TestRead_TableHeaders checks a table's column names written into the
// header cells its sheet leaves empty.
func TestRead_TableHeaders(t *testing.T) {
	t.Parallel()
	files := workbookFiles([]testSheet{{
		name: "S",
		xml:  `<sheetData><row r="2"><c r="B2" t="inlineStr"><is><t>Kept</t></is></c></row></sheetData><tableParts count="1"><tablePart r:id="rIdT"/></tableParts>`,
		rels: `<Relationship Id="rIdT" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/table" Target="../tables/table1.xml"/>`,
	}}, minimalStyles, "", "", map[string]string{"xl/tables/table1.xml": `<table ` + nsDecl + ` id="1" name="T" displayName="T" ref="A2:C5">` +
		`<tableColumns count="3"><tableColumn id="1" name="First"/><tableColumn id="2" name="Second"/><tableColumn id="3" name="Third_x0021_"/></tableColumns></table>`})
	doc, _ := readBytes(t, zipFiles(t, files))
	sh := doc.Sheets[0]
	for col, want := range []string{"First", "Kept", "Third!"} {
		if c, _ := cellAt(sh, 1, col); c.Value.Text != want {
			t.Errorf("column %d = %q, want %q", col, c.Value.Text, want)
		}
	}
}

// TestRead_TableStyles checks a preset table style laid over a table's
// cells, the header row, the banded rows, the total row and the outer and
// inner borders, and a workbook's own table style.
func TestRead_TableStyles(t *testing.T) {
	t.Parallel()
	table := func(style string) string {
		return `<table ` + nsDecl + ` id="1" name="T" displayName="T" ref="A1:B4" totalsRowCount="1"><tableColumns count="2"><tableColumn id="1" name="A"/><tableColumn id="2" name="B"/></tableColumns>` +
			`<tableStyleInfo name="` + style + `" showFirstColumn="0" showLastColumn="0" showRowStripes="1" showColumnStripes="0"/></table>`
	}
	sheetXML := `<sheetData><row r="2"><c r="A2"><v>1</v></c></row></sheetData><tableParts count="1"><tablePart r:id="rIdT"/></tableParts>`
	rels := `<Relationship Id="rIdT" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/table" Target="../tables/table1.xml"/>`
	styles := minimalStyles + `<dxfs><dxf><fill><patternFill patternType="solid"><bgColor rgb="FF00FF00"/></patternFill></fill></dxf></dxfs>` +
		`<tableStyles count="1"><tableStyle name="Mine" pivot="0" count="1"><tableStyleElement type="headerRow" dxfId="0"/></tableStyle></tableStyles>`
	read := func(style string) *sheet.Sheet {
		files := workbookFiles([]testSheet{{name: "S", xml: sheetXML, rels: rels}}, styles, "", "",
			map[string]string{"xl/tables/table1.xml": table(style), "xl/theme/theme1.xml": testTheme})
		doc, _ := readBytes(t, zipFiles(t, files))
		return doc.Sheets[0]
	}
	sh := read("TableStyleMedium2")
	style := func(row, col int) *sheet.Style {
		c, ok := cellAt(sh, row, col)
		if !ok || c.Style == nil {
			t.Fatalf("no styled cell at row %d, column %d", row, col)
		}
		return c.Style
	}
	accent := rgb(0x4472C4)
	band := tint(accent, 0.79998168889431442)
	line := tint(accent, 0.39997558519241921)
	if h := style(0, 1); !near(h.Fill.Color, accent) || !h.Font.Bold || !near(h.Font.Color, rgb(0xFFFFFF)) {
		t.Errorf("header %+v", h)
	}
	if b := style(1, 0); !near(b.Fill.Color, band) || b.Border[css.Left].Line != sheet.LineSolid || !near(b.Border[css.Left].Color, line) || b.Border[css.Right].Line != sheet.LineNone {
		t.Errorf("first band %+v", b)
	}
	if b := style(2, 1); b.Fill.Color.A != 0 || b.Border[css.Top].Line != sheet.LineSolid || b.Border[css.Right].Line != sheet.LineSolid {
		t.Errorf("second band %+v", b)
	}
	if tot := style(3, 0); !tot.Font.Bold || tot.Border[css.Top].Line != sheet.LineDouble || tot.Fill.Color.A != 0 {
		t.Errorf("total row %+v", tot)
	}
	if c, _ := cellAt(sh, 1, 0); c.Value.Number != 1 {
		t.Errorf("A2 value %+v lost", c.Value)
	}
	mine := read("Mine")
	if c, _ := cellAt(mine, 0, 0); c.Style == nil || !near(c.Style.Fill.Color, rgb(0x00FF00)) {
		t.Errorf("workbook style header %+v", c.Style)
	}
	if c, ok := cellAt(mine, 1, 1); ok && c.Style != nil && c.Style.Fill.Color.A != 0 {
		t.Errorf("workbook style body %+v", c.Style)
	}
}

// TestRead_TableStyleWholeSheet checks a table whose area names the whole
// sheet: its style reaches a cell the sheet holds past maxTableCells, and
// the reading follows the sheet's cells, not the area's 17 billion.
func TestRead_TableStyleWholeSheet(t *testing.T) {
	t.Parallel()
	for _, ref := range []string{"A1:XFD1048576", "A1:A1048576"} {
		t.Run(ref, func(t *testing.T) {
			t.Parallel()
			table := `<table ` + nsDecl + ` id="1" name="T" displayName="T" ref="` + ref + `"><tableColumns count="1"><tableColumn id="1" name="A"/></tableColumns>` +
				`<tableStyleInfo name="TableStyleMedium2" showRowStripes="1"/></table>`
			files := workbookFiles([]testSheet{{
				name: "S",
				xml:  `<sheetData><row r="1048576"><c r="A1048576"><v>1</v></c></row></sheetData><tableParts count="1"><tablePart r:id="rIdT"/></tableParts>`,
				rels: `<Relationship Id="rIdT" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/table" Target="../tables/table1.xml"/>`,
			}}, minimalStyles, "", "", map[string]string{"xl/tables/table1.xml": table, "xl/theme/theme1.xml": testTheme})
			start := time.Now()
			doc, _ := readBytes(t, zipFiles(t, files))
			if elapsed := time.Since(start); elapsed > 20*time.Second {
				t.Errorf("reading took %v", elapsed)
			}
			if c, ok := cellAt(doc.Sheets[0], 1048575, 0); !ok || c.Style == nil || c.Value.Number != 1 {
				t.Errorf("last cell %+v, want its value styled", c)
			}
		})
	}
}

func TestCell_Formulas(t *testing.T) {
	doc, _ := readBytes(t, oneSheet(t, `<sheetData><row r="1"><c r="A1"><f>B1+2</f><v>5</v></c><c r="B1"><f>SUM(C1:C2)</f></c>`+
		`<c r="C1"><f>1</f><v></v></c><c r="D1"><f t="shared" si="0"/></c></row></sheetData>`))
	sh := doc.Sheets[0]
	for i, want := range []string{"", "SUM(C1:C2)", "1"} {
		c, ok := cellAt(sh, 0, i)
		if !ok || c.Formula != want {
			t.Errorf("cell %d: %+v, want the formula %q", i, c, want)
		}
	}
	if _, ok := cellAt(sh, 0, 3); ok {
		t.Errorf("a shared formula's cell without text is kept")
	}
}

func TestChartsheet(t *testing.T) {
	chartXML := func(series string) string {
		return `<c:chartSpace xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart"><c:chart><c:plotArea><c:lineChart>` + series +
			`<c:axId val="1"/><c:axId val="2"/></c:lineChart><c:catAx><c:axId val="1"/><c:crossAx val="2"/></c:catAx><c:valAx><c:axId val="2"/><c:crossAx val="1"/></c:valAx></c:plotArea></c:chart></c:chartSpace>`
	}
	tests := []struct {
		name   string
		series string
		want   int
	}{
		{"a series", `<c:ser><c:idx val="0"/><c:order val="0"/><c:val><c:numLit><c:ptCount val="1"/><c:pt idx="0"><c:v>1</c:v></c:pt></c:numLit></c:val></c:ser>`, 1},
		{"no series prints nothing", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := workbookFiles([]testSheet{{name: "Data", xml: `<sheetData/>`}}, minimalStyles, "", "", map[string]string{
				"xl/chartsheets/sheet1.xml": `<chartsheet ` + nsDecl + `><pageMargins left="0.7" right="0.7" top="0.75" bottom="0.75" header="0.3" footer="0.3"/><drawing r:id="rIdD"/></chartsheet>`,
				"xl/chartsheets/_rels/sheet1.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
					`<Relationship Id="rIdD" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing" Target="../drawings/drawing1.xml"/></Relationships>`,
				"xl/drawings/drawing1.xml": `<xdr:wsDr ` + drawingNS + `><xdr:absoluteAnchor><xdr:pos x="0" y="0"/><xdr:ext cx="9292897" cy="6069724"/>` +
					`<xdr:graphicFrame macro=""><xdr:nvGraphicFramePr><xdr:cNvPr id="2" name="Chart"/><xdr:cNvGraphicFramePr/></xdr:nvGraphicFramePr><xdr:xfrm/>` +
					`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/chart"><c:chart xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" r:id="rIdC"/></a:graphicData></a:graphic></xdr:graphicFrame><xdr:clientData/></xdr:absoluteAnchor></xdr:wsDr>`,
				"xl/drawings/_rels/drawing1.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
					`<Relationship Id="rIdC" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/chart" Target="../charts/chart1.xml"/></Relationships>`,
				"xl/charts/chart1.xml": chartXML(tt.series),
			})
			files["xl/workbook.xml"] = strings.Replace(files["xl/workbook.xml"], `</sheets>`, `<sheet name="Chart" sheetId="9" r:id="rIdCS"/></sheets>`, 1)
			files["xl/_rels/workbook.xml.rels"] = strings.Replace(files["xl/_rels/workbook.xml.rels"], `</Relationships>`,
				`<Relationship Id="rIdCS" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/chartsheet" Target="chartsheets/sheet1.xml"/></Relationships>`, 1)
			doc, _ := readBytes(t, zipFiles(t, files))
			if len(doc.Sheets) != 2 {
				t.Fatalf("%d sheets", len(doc.Sheets))
			}
			sh := doc.Sheets[1]
			if len(sh.Drawings) != tt.want {
				t.Fatalf("%d drawings, want %d", len(sh.Drawings), tt.want)
			}
			if sh.Page.Scale != 0 || sh.Page.FitWidth != 1 || sh.Page.FitHeight != 1 {
				t.Errorf("page %+v, want fitted to one page", sh.Page)
			}
			if tt.want == 1 && (!sh.Drawings[0].Absolute || sh.Drawings[0].Chart == nil) {
				t.Errorf("drawing %+v", sh.Drawings[0])
			}
		})
	}
}

func TestUNCURL(t *testing.T) {
	tests := []struct{ in, want string }{
		{`file:///\\host\share\a%20b.docx`, "file://host/share/a%20b.docx"},
		{`\\host\share\x`, "file://host/share/x"},
		{"https://example.com/", "https://example.com/"},
		{`file:///\\`, `file:///\\`},
	}
	for _, tc := range tests {
		if got := uncURL(tc.in); got != tc.want {
			t.Errorf("uncURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestRead_Recalculation locks which cached formula results a workbook
// shows: all of them when Microsoft's or LibreOffice's application wrote
// it, or when a formula caches a number other than 0; none, every formula
// left for the builder to compute, otherwise, as LibreOffice
// recalculates such a workbook.
func TestRead_Recalculation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, app, cached string
		recalc            bool
	}{
		{"another application, results 0", "Apache POI", "0.0", true},
		{"another application, a result", "Apache POI", "7", false},
		{"Excel", "Microsoft Excel", "0", false},
		{"LibreOffice", "LibreOffice/7.6", "0", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ws := `<sheetData><row r="1"><c r="A1"><v>2</v></c><c r="B1"><f>A1*2</f><v>` + tc.cached + `</v></c></row></sheetData>`
			files := workbookFiles([]testSheet{{name: "S", xml: ws}}, minimalStyles, "", "",
				map[string]string{"docProps/app.xml": `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>` + tc.app + `</Application></Properties>`})
			doc, _ := readBytes(t, zipFiles(t, files))
			c := doc.Sheets[0].Rows[0].Cells[1]
			if (c.Formula == "A1*2") != tc.recalc {
				t.Errorf("formula %q, want recalculated %v", c.Formula, tc.recalc)
			}
		})
	}
}

func TestCell_FormulaCapCountsCharacters(t *testing.T) {
	accented := `"` + strings.Repeat("é", 3000) + `"`
	long := strings.Repeat("1+", maxFormulaRunes/2) + "1"
	doc, _ := readBytes(t, oneSheet(t, `<sheetData><row r="1"><c r="A1"><f>`+accented+`</f></c><c r="B1"><f>`+long+`</f></c></row></sheetData>`))
	if c, ok := cellAt(doc.Sheets[0], 0, 0); !ok || c.Formula != accented {
		t.Errorf("a formula of 3,002 characters in 6,002 bytes is not kept")
	}
	if c, ok := cellAt(doc.Sheets[0], 0, 1); ok && c.Formula != "" {
		t.Errorf("a formula of %d characters is kept", len(long))
	}
}

// TestRead_StandardRowHeights checks the rows Excel prints from the Normal
// font: a book without a custom default height prints its default rows at
// the font's standard row when defaultRowHeight stands within 2 screen
// pixels of it, 121 printer pixels for Calibri 11 at 14.4 points and 103
// for Arial 10, and its own default past that, 102 for 12.75 points of
// Calibri; a row of h points h in
// screen pixels times the standard row's printer pixels over its screen
// ones, truncated: 116.56 for 14.45 points of Calibri, 102.85 for 12.75,
// 103 for 12.75 points of Arial 10; Arial 11, Times New Roman 12 and
// Courier New 10 at the standard rows Excel measures from those faces.
func TestRead_StandardRowHeights(t *testing.T) {
	t.Parallel()
	fonts := pinnedFonts(t)
	for _, tc := range []struct {
		family       string
		size         string
		defHeight    string
		def          float64
		rows, height []float64
	}{
		{"Carlito", "11", "14.4", 121, []float64{14.45, 12.75, 15}, []float64{116, 102, 121}},
		{"Carlito", "11", "12.75", 102, nil, nil},
		{"Liberation Sans", "10", "14.4", 103, []float64{12.75, 15}, []float64{103, 121}},
		// Faces whose pixel extents decide ([deviceStandardRows]).
		{"Arial", "11", "15", 114, []float64{12.75, 20}, []float64{102, 160}},
		{"Times New Roman", "12", "15.75", 126, []float64{15, 20}, []float64{120, 160}},
		{"Courier New", "10", "13.2", 96, []float64{12.75, 15}, []float64{90, 106}},
	} {
		styles := strings.Replace(strings.Replace(minimalStyles, "Test Sans", tc.family, 1), `<sz val="11"/>`, `<sz val="`+tc.size+`"/>`, 1)
		var rows strings.Builder
		for i, h := range tc.rows {
			fmt.Fprintf(&rows, `<row r="%d" ht="%g"><c r="A%d"><v>1</v></c></row>`, i+1, h, i+1)
		}
		xml := `<sheetFormatPr defaultRowHeight="` + tc.defHeight + `"/><sheetData>` + rows.String() + `</sheetData>`
		data := zipFiles(t, workbookFiles([]testSheet{{name: "S", xml: xml}}, styles, "", "", nil))
		doc, _, err := load(context.Background(), bytes.NewReader(data), fonts, office.Options{})
		if err != nil {
			t.Fatal(err)
		}
		sh := doc.Sheets[0]
		if got := math.Round(sh.DefaultRowHeight / printerPixel); got != tc.def {
			t.Errorf("%s %s: default row %v printer pixels, want %v", tc.family, tc.size, got, tc.def)
		}
		for i, r := range sh.Rows {
			if got := math.Round(r.Height / printerPixel); got != tc.height[i] {
				t.Errorf("%s %s: a row of %v points prints %v printer pixels, want %v", tc.family, tc.size, tc.rows[i], got, tc.height[i])
			}
		}
	}
}

// TestRead_KnownFonts checks which books Excel measures as it opens them:
// one whose fonts part lacks x14ac:knownFonts (sheet.Document.FitRows),
// and, in either, a row without a height under a custom default keeps the
// default, unmeasured.
func TestRead_KnownFonts(t *testing.T) {
	t.Parallel()
	fonts := pinnedFonts(t)
	known := strings.Replace(minimalStyles, "<fonts>", `<fonts xmlns:x14ac="http://schemas.microsoft.com/office/spreadsheetml/2009/9/ac" x14ac:knownFonts="1">`, 1)
	for _, tc := range []struct {
		styles string
		fit    bool
	}{{minimalStyles, true}, {known, false}} {
		xml := `<sheetFormatPr defaultRowHeight="16.5" customHeight="1"/><sheetData><row r="1"><c r="A1"><v>1</v></c></row><row r="2" ht="20"><c r="A2"><v>1</v></c></row></sheetData>`
		data := zipFiles(t, workbookFiles([]testSheet{{name: "S", xml: xml}}, tc.styles, "", "", nil))
		doc, _, err := load(context.Background(), bytes.NewReader(data), fonts, office.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if doc.FitRows != tc.fit {
			t.Errorf("knownFonts %v: FitRows %v, want %v", !tc.fit, doc.FitRows, tc.fit)
		}
		if rows := doc.Sheets[0].Rows; !rows[0].CustomHeight || rows[1].CustomHeight {
			t.Errorf("knownFonts %v: custom rows %v and %v, want the row without a height custom", !tc.fit, rows[0].CustomHeight, rows[1].CustomHeight)
		}
	}
}
