package xlsx

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
)

// TestRead_Workbook checks the sheets in book order with their states,
// the print areas and titles of the defined names, the 1904 date system
// and the core properties.
func TestRead_Workbook(t *testing.T) {
	t.Parallel()
	data := `<sheetData><row r="1"><c r="A1" s="1"><v>0</v></c><c r="B1"><v>2.5</v></c></row></sheetData>`
	styles := minimalStyles[:len(minimalStyles)-len(`<cellXfs><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/></cellXfs><cellStyles><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>`)] +
		`<cellXfs><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="14" fontId="0" fillId="0" borderId="0" xfId="0"/></cellXfs>`
	files := workbookFiles([]testSheet{
		{name: "First", xml: data},
		{name: "Hidden", state: "hidden", xml: data},
		{name: "Very", state: "veryHidden", xml: data},
		{name: "Last &amp; one", xml: data},
	}, styles, `<workbookPr date1904="1"/>`,
		`<definedNames><definedName name="_xlnm.Print_Area" localSheetId="0">First!$A$1:$C$9,First!$E$2</definedName>`+
			`<definedName name="_xlnm.Print_Titles" localSheetId="3">'Last &amp; one'!$A:$B,'Last &amp; one'!$1:$2</definedName>`+
			`<definedName name="Other">First!$A$1</definedName></definedNames>`,
		map[string]string{"docProps/core.xml": `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/">` +
			`<dc:title>Budget</dc:title><dc:creator>Ann</dc:creator><dc:description>About</dc:description></cp:coreProperties>`})
	doc, _ := readBytes(t, zipFiles(t, files))
	var names []string
	var hidden []bool
	for _, s := range doc.Sheets {
		names = append(names, s.Name)
		hidden = append(hidden, s.Hidden)
	}
	if want := []string{"First", "Hidden", "Very", "Last & one"}; !equalStrings(names, want) {
		t.Errorf("sheets %q, want %q", names, want)
	}
	if want := []bool{false, true, true, false}; !equalBools(hidden, want) {
		t.Errorf("hidden %v, want %v", hidden, want)
	}
	first := doc.Sheets[0].Page
	if want := []sheet.Range{{LastRow: 8, LastColumn: 2}, {FirstRow: 1, FirstColumn: 4, LastRow: 1, LastColumn: 4}}; len(first.PrintAreas) != 2 || first.PrintAreas[0] != want[0] || first.PrintAreas[1] != want[1] {
		t.Errorf("print areas %v, want %v", first.PrintAreas, want)
	}
	last := doc.Sheets[3].Page
	if last.RepeatColumns == nil || last.RepeatColumns.FirstColumn != 0 || last.RepeatColumns.LastColumn != 1 {
		t.Errorf("repeated columns %v, want A:B", last.RepeatColumns)
	}
	if last.RepeatRows == nil || last.RepeatRows.FirstRow != 0 || last.RepeatRows.LastRow != 1 {
		t.Errorf("repeated rows %v, want 1:2", last.RepeatRows)
	}
	if doc.Sheets[1].Page.PrintAreas != nil || doc.Sheets[0].Page.RepeatRows != nil {
		t.Error("a defined name applies to another sheet than its own")
	}
	date, _ := cellAt(doc.Sheets[0], 0, 0)
	plain, _ := cellAt(doc.Sheets[0], 0, 1)
	if date.Value.Number != 1462 || plain.Value.Number != 2.5 {
		t.Errorf("1904 serials %v and %v, want a date shifted to 1462 and a number kept at 2.5", date.Value.Number, plain.Value.Number)
	}
	if doc.Meta.Title != "Budget" || doc.Meta.Author != "Ann" || doc.Meta.Subject != "About" {
		t.Errorf("metadata %+v", doc.Meta)
	}
}

// TestRead_SharedStrings checks plain and rich shared strings, the
// phonetic runs dropped, the ST_Xstring escapes decoded and the line
// breaks of a cell that does not wrap dropped.
func TestRead_SharedStrings(t *testing.T) {
	t.Parallel()
	sst := `<sst ` + nsDecl + `><si><t>plain</t></si>` +
		`<si><r><t>bo</t></r><r><rPr><b/><sz val="14"/><color rgb="FFFF0000"/><rFont val="Arial"/></rPr><t xml:space="preserve">ld </t></r><rPh sb="0" eb="1"><t>ふ</t></rPh></si>` +
		`<si><t>a_x000D__x000A_b_x005F_x0041_</t><rPh><t>x</t></rPh></si></sst>`
	files := workbookFiles([]testSheet{{name: "S", xml: `<sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c><c r="D1" t="s"><v>9</v></c></row></sheetData>`}},
		minimalStyles, "", "", map[string]string{"xl/sharedStrings.xml": sst})
	doc, _ := readBytes(t, zipFiles(t, files))
	sh := doc.Sheets[0]
	for _, tc := range []struct {
		col  int
		text string
		runs int
	}{{0, "plain", 0}, {1, "bold ", 2}, {2, "a\nb_x0041_", 0}} {
		c, ok := cellAt(sh, 0, tc.col)
		if !ok || c.Value.Kind != sheet.Text || c.Value.Text != tc.text || len(c.Runs) != tc.runs {
			t.Errorf("column %d: %+v, want text %q with %d runs", tc.col, c, tc.text, tc.runs)
		}
	}
	rich, _ := cellAt(sh, 0, 1)
	if len(rich.Runs) == 2 {
		if rich.Runs[0].Font != nil {
			t.Error("a run without properties carries a font")
		}
		f := rich.Runs[1].Font
		if f == nil || !f.Bold || f.Size != 14 || f.Families[0] != "Arial" || f.Color.R != 1 || f.Color.G != 0 {
			t.Errorf("run font %+v", f)
		}
	}
	if _, ok := cellAt(sh, 0, 3); ok {
		t.Error("a cell naming a missing shared string is kept")
	}
}

// equalStrings reports whether a and b hold the same strings.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// equalBools reports whether a and b hold the same values.
func equalBools(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestRead_NoFonts checks the column widths of a stylesheet without
// fonts: LibreOffice counts them in digits of 2 millimetres.
func TestRead_NoFonts(t *testing.T) {
	t.Parallel()
	files := workbookFiles([]testSheet{{name: "S", xml: `<cols><col min="1" max="1" width="30" customWidth="1"/></cols><sheetData/>`}},
		`<cellXfs><xf numFmtId="0"/></cellXfs>`, "", "", nil)
	doc, _ := readBytes(t, zipFiles(t, files))
	if w := doc.Sheets[0].Columns[0].Width; w != 170.1 {
		t.Errorf("width %v, want 30 digits of 2 mm, 170.1 points", w)
	}
}

func TestJoinLines(t *testing.T) {
	cell := sheet.Cell{Value: sheet.Value{Kind: sheet.Text, Text: "123\n456\r\n7"}, Runs: []sheet.Run{{Text: "123\n"}, {Text: "\n"}, {Text: "456\r\n7"}}}
	joinLines(&cell)
	if cell.Value.Text != "123456\n7" || len(cell.Runs) != 2 || cell.Runs[0].Text != "123" || cell.Runs[1].Text != "456\n7" {
		t.Errorf("joined cell %+v", cell)
	}
}
