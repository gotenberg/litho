package pdf

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// testBookSheet is one sheet of a test workbook: its name, whether it is
// hidden, and the cells of its first row, SpreadsheetML c elements.
type testBookSheet struct {
	name   string
	hidden bool
	row    string
}

// testWorkbook returns an XLSX workbook of sheets that application wrote.
func testWorkbook(t *testing.T, application string, sheets ...testBookSheet) []byte {
	t.Helper()
	const ns = `xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
	const relNS = `xmlns="http://schemas.openxmlformats.org/package/2006/relationships"`
	files := [][2]string{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="xml" ContentType="application/xml"/><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/></Types>`},
		{"_rels/.rels", `<Relationships ` + relNS + `>` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/></Relationships>`},
		{"docProps/app.xml", `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>` + application + `</Application></Properties>`},
	}
	var wb, rels strings.Builder
	wb.WriteString(`<workbook ` + ns + `><sheets>`)
	rels.WriteString(`<Relationships ` + relNS + `>`)
	for i, s := range sheets {
		state := "visible"
		if s.hidden {
			state = "hidden"
		}
		fmt.Fprintf(&wb, `<sheet name="%s" sheetId="%d" state="%s" r:id="rIdS%d"/>`, s.name, i+1, state, i+1)
		fmt.Fprintf(&rels, `<Relationship Id="rIdS%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i+1, i+1)
		files = append(files, [2]string{fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), `<worksheet ` + ns + `><sheetData><row r="1">` + s.row + `</row></sheetData></worksheet>`})
	}
	wb.WriteString(`</sheets></workbook>`)
	rels.WriteString(`</Relationships>`)
	files = append(files, [2]string{"xl/workbook.xml", wb.String()}, [2]string{"xl/_rels/workbook.xml.rels", rels.String()})
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		w, err := zw.Create(f[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(f[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// inlineCell is a c element at ref holding the text s.
func inlineCell(ref, s string) string {
	return `<c r="` + ref + `" t="inlineStr"><is><t>` + s + `</t></is></c>`
}

// convertedPages converts c and returns the text of each page and the
// titles of the outline.
func convertedPages(t *testing.T, c *Conversion) ([]string, []string) {
	t.Helper()
	ctx := context.Background()
	var out bytes.Buffer
	if _, err := c.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	n, err := r.PageCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var pages []string
	for p := 1; p <= n; p++ {
		text, err := r.ExtractText(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, strings.Join(strings.Fields(text), " "))
	}
	marks, err := r.Bookmarks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, m := range marks {
		titles = append(titles, m.Title)
	}
	return pages, titles
}

// TestConvert_SheetSelection checks which sheets of a workbook print under
// each selection, in book order, and the outline that follows them.
func TestConvert_SheetSelection(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	var sheets []testBookSheet
	for i, name := range []string{"North", "South", "East", "West", "Total"} {
		sheets = append(sheets, testBookSheet{name: name, hidden: i == 1, row: inlineCell("A1", strings.ToLower(name))})
	}
	book := testWorkbook(t, "Microsoft Excel", sheets...)
	tests := []struct {
		name string
		opts []SpreadsheetOption
		want []string
	}{
		{"every shown sheet", nil, []string{"North", "East", "West", "Total"}},
		{"hidden sheets", []SpreadsheetOption{WithHiddenSheets(true)}, []string{"North", "South", "East", "West", "Total"}},
		{"by name, in book order", []SpreadsheetOption{WithSheets("Total", "South")}, []string{"South", "Total"}},
		{"by number, hidden counted", []SpreadsheetOption{WithSheetNumbers(3, 2)}, []string{"South", "East"}},
	}
	for _, tt := range tests {
		pages, outline := convertedPages(t, FromXLSX(bytes.NewReader(book), append([]SpreadsheetOption{WithConvertFontDirs(dir)}, tt.opts...)...))
		if !slices.Equal(outline, tt.want) || len(pages) != len(tt.want) {
			t.Errorf("%s: outline %v over %d pages, want %v", tt.name, outline, len(pages), tt.want)
			continue
		}
		for i, p := range pages {
			if p != strings.ToLower(tt.want[i]) {
				t.Errorf("%s: page %d reads %q, want %q", tt.name, i+1, p, strings.ToLower(tt.want[i]))
			}
		}
	}
	for name, opt := range map[string]SpreadsheetOption{"West ": WithSheets("West "), "6": WithSheetNumbers(1, 6)} {
		_, err := FromXLSX(bytes.NewReader(book), WithConvertFontDirs(dir), opt).WriteTo(context.Background(), &bytes.Buffer{})
		if !errors.Is(err, ErrInvalidConvertOptions) || !IsInvalidInput(err) || !strings.Contains(err.Error(), strings.TrimSpace(name)) {
			t.Errorf("sheet %q: err = %v, want ErrInvalidConvertOptions naming it", name, err)
		}
	}
}

// TestConvert_FormulaRecalculation checks what each mode prints for a
// formula whose stored result is stale (B1 stores 7 for 2*2) and one that
// stores none (C1), in a workbook Excel wrote, whose results litho trusts
// by default.
func TestConvert_FormulaRecalculation(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	book := testWorkbook(t, "Microsoft Excel", testBookSheet{name: "S", row: `<c r="A1"><v>2</v></c><c r="B1"><f>A1*2</f><v>7</v></c><c r="C1"><f>A1+1</f></c>`})
	for mode, want := range map[Recalculation]string{
		RecalculateAsNeeded: "2 7 3",
		RecalculateNever:    "2 7",
		RecalculateAlways:   "2 4 3",
	} {
		pages, _ := convertedPages(t, FromXLSX(bytes.NewReader(book), WithConvertFontDirs(dir), WithFormulaRecalculation(mode)))
		if len(pages) != 1 || pages[0] != want {
			t.Errorf("mode %d: %q, want %q", mode, pages, want)
		}
	}
	// An OpenDocument spreadsheet is recomputed as needed, its stored
	// result kept under RecalculateNever alone.
	ods := `<?xml version="1.0" encoding="UTF-8"?><office:document xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" ` +
		`xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0" xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" ` +
		`office:version="1.3" office:mimetype="application/vnd.oasis.opendocument.spreadsheet"><office:body><office:spreadsheet>` +
		`<table:table table:name="S"><table:table-row><table:table-cell office:value-type="float" office:value="2"><text:p>2</text:p></table:table-cell>` +
		`<table:table-cell table:formula="of:=[.A1]*2" office:value-type="float" office:value="7"><text:p>7</text:p></table:table-cell></table:table-row></table:table>` +
		`</office:spreadsheet></office:body></office:document>`
	for mode, want := range map[Recalculation]string{RecalculateAsNeeded: "2 4", RecalculateNever: "2 7", RecalculateAlways: "2 4"} {
		pages, _ := convertedPages(t, FromODS(strings.NewReader(ods), WithConvertFontDirs(dir), WithFormulaRecalculation(mode)))
		if len(pages) != 1 || pages[0] != want {
			t.Errorf("ODS mode %d: %q, want %q", mode, pages, want)
		}
	}
}
