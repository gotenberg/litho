package xlsx

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
)

// pivotWorkbook returns a workbook whose sheet S holds name and v in
// A1:B4 and a pivot table at E3 of the pivot field settings fieldAttrs and
// the page and data field elements extra.
func pivotWorkbook(t testing.TB, fieldAttrs, extra string) []byte {
	t.Helper()
	return pivotPackage(t, `<pivotTableDefinition `+nsDecl+` name="P1" cacheId="7" colGrandTotals="0"><location ref="E3:F6" firstHeaderRow="1" firstDataRow="1" firstDataCol="1"/>`+
		`<pivotFields count="2"><pivotField axis="axisRow" `+fieldAttrs+`><items count="3"><item x="1"/><item h="1" x="0"/><item t="default"/></items></pivotField><pivotField dataField="1"/></pivotFields>`+
		`<rowFields count="1"><field x="0"/></rowFields>`+extra+`</pivotTableDefinition>`)
}

// pivotPackage returns the workbook of pivotWorkbook with the pivot table
// definition def.
func pivotPackage(t testing.TB, def string) []byte {
	t.Helper()
	ws := `<sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>name</t></is></c><c r="B1" t="inlineStr"><is><t>v</t></is></c></row>` +
		`<row r="2"><c r="A2" t="inlineStr"><is><t>x</t></is></c><c r="B2"><v>1</v></c></row>` +
		`<row r="3"><c r="A3" t="inlineStr"><is><t>y</t></is></c><c r="B3"><v>2</v></c></row>` +
		`<row r="4"><c r="A4" t="inlineStr"><is><t>y</t></is></c><c r="B4"><v>3</v></c></row></sheetData>`
	rels := `<Relationship Id="rIdP" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/pivotTable" Target="../pivotTables/pivotTable1.xml"/>`
	files := workbookFiles([]testSheet{{name: "S", xml: ws, rels: rels}}, minimalStyles, "",
		`<pivotCaches><pivotCache cacheId="7" r:id="rIdC"/></pivotCaches>`, map[string]string{
			"xl/pivotCache/pivotCacheDefinition1.xml": `<pivotCacheDefinition ` + nsDecl + `><cacheSource type="worksheet"><worksheetSource ref="A1:B4" sheet="S"/></cacheSource>` +
				`<cacheFields count="2"><cacheField name="name"><sharedItems count="2"><s v="x"/><s v="y"/></sharedItems></cacheField><cacheField name="v"><sharedItems/></cacheField></cacheFields></pivotCacheDefinition>`,
			"xl/pivotTables/pivotTable1.xml": def,
		})
	wbRels := files["xl/_rels/workbook.xml.rels"]
	files["xl/_rels/workbook.xml.rels"] = wbRels[:len(wbRels)-len(`</Relationships>`)] +
		`<Relationship Id="rIdC" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/pivotCacheDefinition" Target="pivotCache/pivotCacheDefinition1.xml"/></Relationships>`
	return zipFiles(t, files)
}

func TestPivotTables_Read(t *testing.T) {
	x, y := sheet.Value{Kind: sheet.Text, Text: "x"}, sheet.Value{Kind: sheet.Text, Text: "y"}
	tests := []struct {
		name       string
		fieldAttrs string
		extra      string
		check      func(t *testing.T, p sheet.Pivot)
	}{
		{
			name:  "row field, data field, location",
			extra: `<dataFields count="1"><dataField name="Sum of v" fld="1"/></dataFields>`,
			check: func(t *testing.T, p sheet.Pivot) {
				if p.Row != 2 || p.Column != 4 || p.Clear == nil || *p.Clear != (sheet.Range{FirstRow: 2, FirstColumn: 4, LastRow: 5, LastColumn: 5}) {
					t.Errorf("location = %d,%d clear %+v", p.Row, p.Column, p.Clear)
				}
				if p.SourceSheet != 0 || p.Source != (sheet.Range{LastRow: 3, LastColumn: 1}) {
					t.Errorf("source = %d %+v", p.SourceSheet, p.Source)
				}
				if !p.RowGrand || p.ColumnGrand || p.HideHeader || !p.ExpandCollapse {
					t.Errorf("flags = %+v", p)
				}
				if len(p.Rows) != 1 || p.Rows[0].Field != 0 || p.Rows[0].Name != "name" || p.Rows[0].Layout != sheet.PivotCompact ||
					!p.Rows[0].ShowEmpty || len(p.Rows[0].Subtotals) != 1 || p.Rows[0].Subtotals[0] != sheet.PivotAuto {
					t.Errorf("row field = %+v", p.Rows)
				}
				if f := p.Rows[0]; len(f.Order) != 2 || f.Order[0] != y || f.Order[1] != x || len(f.Hidden) != 1 || f.Hidden[0] != x {
					t.Errorf("items = %+v %+v", f.Order, f.Hidden)
				}
				if len(p.Data) != 1 || p.Data[0] != (sheet.PivotData{Field: 1, Name: "v", Function: sheet.PivotSum, Caption: "Sum of v"}) {
					t.Errorf("data = %+v", p.Data)
				}
			},
		},
		{
			name:       "tabular, sorted, subtotals by function",
			fieldAttrs: `outline="0" sortType="descending" showAll="0" sumSubtotal="1" countSubtotal="1" insertBlankRow="1"`,
			extra:      `<dataFields count="1"><dataField fld="1" subtotal="countNums"/></dataFields>`,
			check: func(t *testing.T, p sheet.Pivot) {
				f := p.Rows[0]
				if f.Layout != sheet.PivotTabular || f.Sort != sheet.PivotDescending || f.ShowEmpty || !f.AddEmpty || len(f.Order) != 0 {
					t.Errorf("field = %+v", f)
				}
				if len(f.Subtotals) != 2 || f.Subtotals[0] != sheet.PivotSum || f.Subtotals[1] != sheet.PivotCountNums {
					t.Errorf("subtotals = %v", f.Subtotals)
				}
				if p.Data[0].Function != sheet.PivotCountNums {
					t.Errorf("function = %v", p.Data[0].Function)
				}
			},
		},
		{
			name:       "page field above the location",
			fieldAttrs: `outline="1" subtotalTop="0"`,
			extra:      `<pageFields count="1"><pageField fld="0" item="0"/></pageFields><dataFields count="1"><dataField fld="1"/></dataFields>`,
			check: func(t *testing.T, p sheet.Pivot) {
				if p.Row != 0 || len(p.Pages) != 1 || p.Pages[0].Selected == nil || *p.Pages[0].Selected != y {
					t.Errorf("row %d pages %+v", p.Row, p.Pages)
				}
				if p.Rows[0].Layout != sheet.PivotOutlineBottom {
					t.Errorf("layout = %v", p.Rows[0].Layout)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, _ := readBytes(t, pivotWorkbook(t, tt.fieldAttrs, tt.extra))
			if len(doc.Sheets[0].Pivots) != 1 {
				t.Fatalf("pivots = %+v", doc.Sheets[0].Pivots)
			}
			tt.check(t, doc.Sheets[0].Pivots[0])
		})
	}
}

func TestPivotTables_Unsupported(t *testing.T) {
	doc, warnings := readBytes(t, pivotWorkbook(t, "", `<dataFields count="1"><dataField fld="1" showDataAs="percentOfTotal"/></dataFields>`))
	if len(doc.Sheets[0].Pivots) != 0 {
		t.Errorf("pivots = %+v", doc.Sheets[0].Pivots)
	}
	if len(warnings) == 0 {
		t.Errorf("no warning")
	}
}

func FuzzPivotTable(f *testing.F) {
	f.Add([]byte(`<pivotTableDefinition ` + nsDecl + ` name="P1" cacheId="7"><location ref="E3:G9" firstHeaderRow="1"/>` +
		`<pivotFields count="2"><pivotField axis="axisRow" outline="0" sumSubtotal="1" insertBlankRow="1"><items count="2"><item x="0"/><item t="default"/></items></pivotField>` +
		`<pivotField axis="axisCol" dataField="1" sortType="descending"/></pivotFields><rowFields><field x="0"/><field x="-2"/></rowFields><colFields><field x="1"/></colFields>` +
		`<pageFields><pageField fld="1" item="0"/></pageFields><dataFields><dataField fld="1" subtotal="average"/><dataField fld="0" subtotal="count" name="n"/></dataFields></pivotTableDefinition>`))
	f.Add([]byte(`<pivotTableDefinition ` + nsDecl + ` name="P1" cacheId="7"><location ref="A1:A1"/><rowFields><field x="9"/></rowFields></pivotTableDefinition>`))
	fonts := testFonts(f)
	f.Fuzz(func(t *testing.T, def []byte) {
		data := pivotPackage(t, string(def))
		doc, _, err := load(context.Background(), bytes.NewReader(data), fonts, office.Options{})
		if err != nil {
			return
		}
		checkModel(t, doc)
		if _, err := sheet.Convert(context.Background(), doc, fonts, office.Options{MaxPages: 50}); err != nil && !errors.Is(err, office.ErrTooManyPages) {
			t.Fatalf("convert: %v", err)
		}
	})
}

func TestPivotTables_ExternalSourceFromRecords(t *testing.T) {
	def := `<pivotTableDefinition ` + nsDecl + ` name="P1" cacheId="7"><location ref="E3:F6" firstHeaderRow="1"/>` +
		`<pivotFields count="2"><pivotField axis="axisRow"/><pivotField dataField="1"/></pivotFields>` +
		`<rowFields count="1"><field x="0"/></rowFields><dataFields count="1"><dataField fld="1"/></dataFields></pivotTableDefinition>`
	data := pivotPackage(t, def)
	files := map[string]string{}
	zr := mustUnzip(t, data)
	for k, v := range zr {
		files[k] = v
	}
	files["xl/pivotCache/pivotCacheDefinition1.xml"] = `<pivotCacheDefinition ` + nsDecl + `><cacheSource type="worksheet"><worksheetSource ref="A1:B4" sheet="Gone" r:id="rIdX"/></cacheSource>` +
		`<cacheFields count="2"><cacheField name="name"><sharedItems count="2"><s v="x"/><e v="#N/A"/></sharedItems></cacheField><cacheField name="v"><sharedItems/></cacheField></cacheFields></pivotCacheDefinition>`
	files["xl/pivotCache/_rels/pivotCacheDefinition1.xml.rels"] = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rIdR" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/pivotCacheRecords" Target="pivotCacheRecords1.xml"/></Relationships>`
	files["xl/pivotCache/pivotCacheRecords1.xml"] = `<pivotCacheRecords ` + nsDecl + `><r><x v="0"/><n v="2"/></r><r><x v="1"/><n v="3"/></r></pivotCacheRecords>`
	doc, _ := readBytes(t, zipFiles(t, files))
	if len(doc.Sheets) != 2 || !doc.Sheets[1].Hidden || doc.Sheets[1].Name != "DPCache_Gone" {
		t.Fatalf("sheets = %d", len(doc.Sheets))
	}
	p := doc.Sheets[0].Pivots
	if len(p) != 1 || p[0].SourceSheet != 1 || p[0].Source != (sheet.Range{LastRow: 2, LastColumn: 1}) {
		t.Fatalf("pivots = %+v", p)
	}
	if c, ok := cellAt(doc.Sheets[1], 1, 0); !ok || c.Value.Text != "x" {
		t.Errorf("A2 = %+v", c)
	}
	if c, ok := cellAt(doc.Sheets[1], 2, 0); ok && c.Value.Kind != sheet.Empty {
		t.Errorf("A3 = %+v, want the error item empty", c)
	}
}

// mustUnzip returns the parts of the zip archive data.
func mustUnzip(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = string(b)
	}
	return out
}
