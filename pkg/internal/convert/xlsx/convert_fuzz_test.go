package xlsx

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
)

// fuzzSheet is a worksheet exercising most of the reader.
const fuzzSheet = `<sheetPr><pageSetUpPr fitToPage="1"/></sheetPr><sheetViews><sheetView rightToLeft="1"/></sheetViews>` +
	`<sheetFormatPr baseColWidth="9" defaultRowHeight="16"/><cols><col min="1" max="3" width="12" style="1"/></cols>` +
	`<sheetData><row r="1" ht="20" customHeight="1" s="1" customFormat="1"><c r="A1" s="1" t="s"><v>0</v></c><c r="B1" t="inlineStr"><is><r><rPr><b/></rPr><t>x</t></r></is></c>` +
	`<c r="C1" t="d"><v>2025-01-01</v></c><c t="b"><v>1</v></c><c t="e"><v>#N/A</v></c></row><row><c><v>3.5</v></c></row></sheetData>` +
	`<mergeCells><mergeCell ref="A2:B3"/></mergeCells><hyperlinks><hyperlink ref="A1" location="x"/></hyperlinks>` +
	`<printOptions gridLines="1"/><pageMargins left="1" right="1" top="1" bottom="1" header="0.5" footer="0.5"/>` +
	`<pageSetup paperSize="9" orientation="landscape" fitToWidth="2" fitToHeight="0"/><headerFooter><oddHeader>&amp;L&amp;B&amp;P&amp;C&amp;A</oddHeader></headerFooter>` +
	`<rowBreaks><brk id="2" man="1"/></rowBreaks>`

// fuzzStyles is the stylesheet of the fuzz workbook.
const fuzzStyles = `<numFmts><numFmt numFmtId="164" formatCode="0.0"/></numFmts><fonts><font><sz val="11"/><name val="Test Sans"/></font><font><b/><u/><color theme="4" tint="0.4"/></font></fonts>` +
	`<fills><fill><patternFill/></fill><fill><patternFill patternType="darkGrid"><fgColor indexed="10"/></patternFill></fill></fills>` +
	`<borders><border diagonalUp="1"><left style="thin"/><diagonal style="double"/></border></borders>` +
	`<cellStyleXfs><xf/></cellStyleXfs><cellXfs><xf/><xf numFmtId="164" fontId="1" fillId="1" borderId="0"><alignment textRotation="120" indent="2"/></xf></cellXfs>`

func FuzzConvert(f *testing.F) {
	f.Add([]byte(fuzzSheet))
	f.Add([]byte(fuzzSheet[:len(fuzzSheet)/2]))
	fonts := testFonts(f)
	f.Fuzz(func(t *testing.T, ws []byte) {
		files := workbookFiles([]testSheet{{name: "S", xml: string(ws)}, {name: "T", state: "hidden", xml: string(ws)}}, fuzzStyles, `<workbookPr date1904="1"/>`,
			`<definedNames><definedName name="_xlnm.Print_Area" localSheetId="0">S!$A$1:$B$2</definedName></definedNames>`,
			map[string]string{"xl/sharedStrings.xml": `<sst ` + nsDecl + `><si><t>a</t></si></sst>`, "xl/theme/theme1.xml": testTheme})
		doc, _, err := load(context.Background(), bytes.NewReader(zipFiles(t, files)), fonts, office.Options{})
		if err != nil {
			return
		}
		checkModel(t, doc)
	})
}

// checkModel fails on a model that breaks the sheet model's contract.
func checkModel(t *testing.T, doc *sheet.Document) {
	t.Helper()
	for _, sh := range doc.Sheets {
		if sh.DefaultColumnWidth <= 0 || sh.DefaultRowHeight <= 0 || sh.Page.Width <= 0 || sh.Page.Height <= 0 {
			t.Fatalf("sheet %q: defaults %v, %v, page %v by %v", sh.Name, sh.DefaultColumnWidth, sh.DefaultRowHeight, sh.Page.Width, sh.Page.Height)
		}
		for i, c := range sh.Columns {
			if c.First > c.Last || i > 0 && c.First <= sh.Columns[i-1].Last {
				t.Fatalf("columns overlap or reverse: %+v", sh.Columns)
			}
		}
		for i, r := range sh.Rows {
			if i > 0 && r.Index <= sh.Rows[i-1].Index {
				t.Fatalf("rows out of order at %d", r.Index)
			}
			for j, c := range r.Cells {
				if j > 0 && c.Column <= r.Cells[j-1].Column {
					t.Fatalf("cells out of order in row %d", r.Index)
				}
			}
		}
		for _, d := range sh.Drawings {
			if (d.Picture == nil) == (d.Chart == nil) {
				t.Fatalf("drawing %+v holds not exactly one of a picture and a chart", d)
			}
		}
	}
}

func FuzzParseHeaderFooter(f *testing.F) {
	f.Add(`&L&"Arial,Bold"&14Page &P of &N&C&A&R&KFF0000&D &T&Z&F&&`)
	f.Add(`&"Ar`)
	base := sheet.Font{Families: []string{"Calibri"}, Size: 11}
	f.Fuzz(func(t *testing.T, s string) {
		for _, part := range parseHeaderFooter(s, base, nil) {
			for _, sp := range part {
				if sp.Font == nil || sp.Font.Size <= 0 || len(sp.Font.Families) == 0 {
					t.Fatalf("span %+v", sp)
				}
				if sp.Field == sheet.FieldNone && sp.Text == "" {
					t.Fatal("an empty text span")
				}
			}
		}
	})
}
