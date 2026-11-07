package xlsx

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
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
		`<row r="7" s="0"/><row><c><v>1</v></c><c><v>2</v></c><c r="E8"/><c r="E8" t="b"><v>1</v></c></row></sheetData>`))
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
	doc, warnings := readBytes(t, oneSheet(t, `<sheetViews><sheetView rightToLeft="1" showZeros="0" workbookViewId="0"/></sheetViews><sheetData><row r="1">`+
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
	for _, col := range []int{2, 10, 11} {
		if c, ok := cellAt(sh, 0, col); ok {
			t.Errorf("column %d kept: %+v", col, c)
		}
	}
	if !sh.RightToLeft || sh.HideZeros {
		t.Errorf("right to left %v, hide zeros %v: want right to left, zeros printed", sh.RightToLeft, sh.HideZeros)
	}
	if len(sh.Merges) != 1 || sh.Merges[0] != (sheet.Range{FirstRow: 2, FirstColumn: 0, LastRow: 3, LastColumn: 2}) {
		t.Errorf("merges %+v", sh.Merges)
	}
	if !hasWarning(warnings, "conditional formatting") {
		t.Errorf("warnings %v lack the conditional formatting", warnings)
	}
}

// TestRead_Hyperlinks checks external links through their relationships
// on the cells of their range, and the internal ones left out.
func TestRead_Hyperlinks(t *testing.T) {
	t.Parallel()
	files := workbookFiles([]testSheet{{name: "S",
		xml: `<sheetData><row r="1"><c r="A1"><v>1</v></c><c r="B1"><v>2</v></c><c r="C1"><v>3</v></c></row></sheetData>` +
			`<hyperlinks><hyperlink ref="A1:B1" r:id="rIdL" location="top"/><hyperlink ref="C1" location="'S'!A1"/></hyperlinks>`,
		rels: `<Relationship Id="rIdL" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com/a" TargetMode="External"/>`,
	}}, minimalStyles, "", "", nil)
	doc, warnings := readBytes(t, zipFiles(t, files))
	sh := doc.Sheets[0]
	for col, want := range []string{"https://example.com/a#top", "https://example.com/a#top", ""} {
		if c, _ := cellAt(sh, 0, col); c.Href != want {
			t.Errorf("column %d links %q, want %q", col, c.Href, want)
		}
	}
	if !hasWarning(warnings, "place in the workbook") {
		t.Errorf("warnings %v lack the internal link", warnings)
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
