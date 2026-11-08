package xlsx

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestRead_ColumnsAndRows checks the column widths in digits of the
// Normal font (Test Sans at 11 points: 5.5 points a digit), the default
// width and height of the format properties and of a sheet without them,
// the rows' heights, flags and styles.
func TestRead_ColumnsAndRows(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name               string
		xml                string
		defWidth, defHeigh float64
	}{
		{"no format properties", `<sheetData/>`, 46.75, 12.8},
		{"base width", `<sheetFormatPr baseColWidth="10" defaultRowHeight="15"/><sheetData/>`, 58.75, 15},
		{"default width", `<sheetFormatPr baseColWidth="10" defaultColWidth="12" defaultRowHeight="20.5"/><sheetData/>`, 66, 20.5},
		{"default base", `<sheetFormatPr defaultRowHeight="0"/><sheetData/>`, 47.75, 12.8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := readBytes(t, oneSheet(t, tc.xml))
			sh := doc.Sheets[0]
			if math.Abs(sh.DefaultColumnWidth-tc.defWidth) > 1e-9 || sh.DefaultRowHeight != tc.defHeigh {
				t.Errorf("defaults %v by %v, want %v by %v", sh.DefaultColumnWidth, sh.DefaultRowHeight, tc.defWidth, tc.defHeigh)
			}
		})
	}
	doc, _ := readBytes(t, oneSheet(t, `<cols><col min="3" max="4" width="10" customWidth="1"/><col min="1" max="1" width="2"/>`+
		`<col min="4" max="6" hidden="1"/><col min="8" max="8" style="0"/><col min="9" max="9"/></cols>`+
		`<sheetData><row r="2" ht="30" customHeight="1"><c r="A2"><v>1</v></c></row><row r="3" ht="40"><c r="A3"><v>1</v></c></row><row r="5" hidden="1" s="0" customFormat="1"/>`+
		`<row r="7" s="0"/><row><c><v>1</v></c><c r="1_1"><v>2</v></c><c r="E8"/><c r="E8" t="b"><v>1</v></c></row></sheetData>`))
	sh := doc.Sheets[0]
	want := []sheet.Column{{First: 0, Last: 0, Width: 11}, {First: 2, Last: 3, Width: 55}, {First: 4, Last: 5, Hidden: true}, {First: 7, Last: 7}}
	if len(sh.Columns) != len(want) {
		t.Fatalf("columns %+v, want %+v", sh.Columns, want)
	}
	for i, c := range sh.Columns {
		if c.First != want[i].First || c.Last != want[i].Last || c.Width != want[i].Width || c.Hidden != want[i].Hidden {
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
	if r := sh.Rows[0]; r.Height != 30 || !r.CustomHeight {
		t.Errorf("row 2 = %+v", r)
	}
	if r := sh.Rows[1]; r.Height != 0 {
		t.Errorf("row 3 = %+v, want the optimal height of a row without a custom one", r)
	}
	if r := sh.Rows[2]; !r.Hidden || r.Style == nil {
		t.Errorf("row 5 = %+v", r)
	}
	if r := sh.Rows[3]; len(r.Cells) != 3 || r.Cells[0].Column != 0 || r.Cells[1].Column != 1 || r.Cells[2].Value.Kind != sheet.Bool {
		t.Errorf("row 8 cells %+v, want A8, B8 and the boolean E8", r.Cells)
	}
}

// TestRead_RowHeightsOfOffice checks the row heights of a workbook an
// Office application wrote, rounded down to three quarters of a point as
// LibreOffice reads them.
func TestRead_RowHeightsOfOffice(t *testing.T) {
	t.Parallel()
	files := workbookFiles([]testSheet{{name: "S", xml: `<sheetFormatPr defaultRowHeight="14.4"/><sheetData><row r="1" ht="20.1" customHeight="1"><c r="A1"><v>1</v></c></row></sheetData>`}}, minimalStyles, "", "",
		map[string]string{"docProps/app.xml": `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>Microsoft Excel</Application></Properties>`})
	doc, _ := readBytes(t, zipFiles(t, files))
	sh := doc.Sheets[0]
	if sh.DefaultRowHeight != 14.25 || sh.Rows[0].Height != 19.5 {
		t.Errorf("heights %v and %v, want 14.25 and 19.5", sh.DefaultRowHeight, sh.Rows[0].Height)
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

// TestRead_PageSetup checks the page setup over LibreOffice's defaults.
func TestRead_PageSetup(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		xml   string
		check func(p sheet.PageSetup) bool
	}{
		{"defaults", `<sheetData/>`, func(p sheet.PageSetup) bool {
			return p.Width == 612 && p.Height == 792 && p.Scale == 100 && math.Abs(p.Margin[3]-53.858) < 0.01 && math.Abs(p.Margin[0]-70.838) < 0.01 && math.Abs(p.HeaderTop-36.85) < 0.01
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
		{"printer defaults", `<sheetData/><pageSetup paperSize="9" usePrinterDefaults="1"/>`, func(p sheet.PageSetup) bool { return p.Width == 612 }},
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
func hasWarning(warnings []compose.Warning, s string) bool {
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
	files := workbookFiles([]testSheet{{name: "S",
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
