package xlsx

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// TestRead_RecalculationModes locks what each recalculation mode keeps of
// a workbook's formulas: the cached result of B1 and the uncached C1.
func TestRead_RecalculationModes(t *testing.T) {
	t.Parallel()
	ws := `<sheetData><row r="1"><c r="A1"><v>2</v></c><c r="B1"><f>A1*2</f><v>7</v></c><c r="C1"><f>A1+1</f></c></row></sheetData>`
	files := workbookFiles([]testSheet{{name: "S", xml: ws}}, minimalStyles, "", "",
		map[string]string{"docProps/app.xml": `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>Microsoft Excel</Application></Properties>`})
	data := zipFiles(t, files)
	tests := []struct {
		mode office.Recalculation
		b, c string
	}{
		{office.RecalculateAsNeeded, "", "A1+1"},
		{office.RecalculateNever, "", ""},
		{office.RecalculateAlways, "A1*2", "A1+1"},
	}
	for _, tc := range tests {
		doc, _, err := load(context.Background(), bytes.NewReader(data), testFonts(t), office.Options{Recalculation: tc.mode})
		if err != nil {
			t.Fatal(err)
		}
		sh := doc.Sheets[0]
		b, _ := cellAt(sh, 0, 1)
		c, cok := cellAt(sh, 0, 2)
		if b.Formula != tc.b || b.Value.Number != 7 || c.Formula != tc.c {
			t.Errorf("mode %d: B1 %q %v, C1 %q, want %q and %q", tc.mode, b.Formula, b.Value, c.Formula, tc.b, tc.c)
		}
		if tc.mode == office.RecalculateNever && cok {
			t.Errorf("never: C1 listed %+v, want nothing: it stores no value", c)
		}
	}
}

// TestRead_SheetSelection checks that a sheet the selection leaves out is
// read for its cells and without its drawing, and that a name the book
// lacks fails.
func TestRead_SheetSelection(t *testing.T) {
	t.Parallel()
	drawn := testSheet{
		name: "Drawn", state: "hidden", xml: `<sheetData><row r="1"><c r="A1"><v>5</v></c></row></sheetData><drawing r:id="rIdD"/>`,
		rels: `<Relationship Id="rIdD" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing" Target="../drawings/drawing1.xml"/>`,
	}
	drawing := `<xdr:wsDr ` + drawingNS + `><xdr:absoluteAnchor><xdr:pos x="0" y="0"/><xdr:ext cx="127000" cy="127000"/>` +
		`<xdr:sp><xdr:nvSpPr><xdr:cNvPr id="3" name="Box"/><xdr:cNvSpPr/></xdr:nvSpPr><xdr:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="12700" cy="12700"/></a:xfrm>` +
		`<a:prstGeom prst="rect"/></xdr:spPr></xdr:sp><xdr:clientData/></xdr:absoluteAnchor></xdr:wsDr>`
	files := workbookFiles([]testSheet{{name: "Front", xml: `<sheetData/>`}, drawn}, minimalStyles, "", "",
		map[string]string{"xl/drawings/drawing1.xml": drawing})
	data := zipFiles(t, files)
	read := func(opts office.Options) int {
		t.Helper()
		doc, _, err := load(context.Background(), bytes.NewReader(data), testFonts(t), opts)
		if err != nil {
			t.Fatal(err)
		}
		if len(doc.Sheets) != 2 || len(doc.Sheets[1].Rows) != 1 {
			t.Fatalf("sheets %+v, want both read with their cells", doc.Sheets)
		}
		return len(doc.Sheets[1].Drawings)
	}
	if n := read(office.Options{}); n != 0 {
		t.Errorf("hidden sheet: %d drawings, want none read", n)
	}
	if n := read(office.Options{HiddenSheets: true}); n != 1 {
		t.Errorf("hidden sheets printed: %d drawings, want 1", n)
	}
	if n := read(office.Options{SheetNumbers: []int{2}}); n != 1 {
		t.Errorf("selected by number: %d drawings, want 1", n)
	}
	if n := read(office.Options{Sheets: []string{"Front"}}); n != 0 {
		t.Errorf("left out by name: %d drawings, want none", n)
	}
	_, err := Convert(context.Background(), bytes.NewReader(data), testFonts(t), office.Options{Sheets: []string{"Back"}})
	if !errors.Is(err, office.ErrInvalidOptions) || !strings.Contains(err.Error(), `"Back"`) {
		t.Errorf("missing sheet: %v", err)
	}
}
