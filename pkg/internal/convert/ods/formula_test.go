package ods

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestDrawing_ReadsAFormula locks a formula object in a cell: a drawing
// of the frame's size, marked a formula, that draws the formula's glyphs.
func TestDrawing_ReadsAFormula(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("..", "..", "..", "..", "test", "integration", "testdata", "fonts", "gotenberg", "truetype", "liberation")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("fixture fonts not fetched (make corpus-fetch CORPUS=fonts)")
	}
	fonts, err := text.NewFonts(context.Background(), nil, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	body := table("S", `<table:table-row><table:table-cell><draw:frame svg:x="0.1cm" svg:y="0.1cm" svg:width="2cm" svg:height="1cm"><draw:object>`+
		`<math xmlns="http://www.w3.org/1998/Math/MathML" display="block"><mrow><mi>x</mi><mo>+</mo><mn>1</mn></mrow></math>`+
		`</draw:object></draw:frame></table:table-cell></table:table-row>`)
	doc, _, err := Read(context.Background(), flat{body: body}.bytes(), fonts, office.Options{})
	if err != nil {
		t.Fatal(err)
	}
	sh := doc.Sheets[0]
	if len(sh.Drawings) != 1 || !sh.Drawings[0].Formula || sh.Drawings[0].Picture == nil || sh.Drawings[0].Alt != "x + 1" {
		t.Fatalf("drawings %+v, want one formula", sh.Drawings)
	}
	glyphs := 0
	for _, f := range sh.Drawings[0].Picture.Vector(t.Context(), typeset.Rect{Width: 56.7, Height: 28.35}) {
		if g, ok := f.(*typeset.Group); ok {
			for _, k := range g.Content {
				if inner, ok := k.(*typeset.Group); ok {
					glyphs += len(inner.Content)
				}
			}
		}
	}
	if glyphs != 3 {
		t.Errorf("the formula draws %d runs, want 3", glyphs)
	}
}

// TestRead_CachedErrorResults locks the error a formula's cached result
// shows as its text: Calc reads it as that error, in the formula's cell
// and in the cells of an array formula's range, whatever number the cell
// stores; a plain number keeps its value.
//
// # Corpus evidence
//
//   - libreoffice/ods: sc/cachedValue.ods prints Err:502 and #N/A for
//     =SQRT(-1) and =NA() saved as 0, in cells and array ranges.
func TestRead_CachedErrorResults(t *testing.T) {
	body := table("S", `<table:table-row>`+
		`<table:table-cell table:number-matrix-columns-spanned="2" table:number-matrix-rows-spanned="1" table:formula="of:=SQRT(-1)" office:value-type="float" office:value="0"><text:p>Err:502</text:p></table:table-cell>`+
		`<table:table-cell office:value-type="float" office:value="0"><text:p>Err:502</text:p></table:table-cell>`+
		`<table:table-cell table:formula="of:=NA()" office:value-type="float" office:value="0"><text:p>#N/A</text:p></table:table-cell>`+
		`<table:table-cell office:value-type="float" office:value="0"><text:p>#N/A</text:p></table:table-cell>`+
		`<table:table-cell table:formula="of:=1" office:value-type="float" office:value="1"><text:p>Err:5x</text:p></table:table-cell>`+
		`</table:table-row>`)
	doc, _ := read(t, flat{body: body}.bytes())
	sh := doc.Sheets[0]
	for col, want := range []string{"Err:502", "Err:502", "#N/A", ""} {
		c := cellAt(sh, 0, col)
		if c == nil {
			t.Fatalf("no cell at column %d", col)
		}
		if want == "" {
			if c.Value.Kind != sheet.Number {
				t.Errorf("column %d = %+v, want the number", col, c.Value)
			}
			continue
		}
		if c.Value.Kind != sheet.Error || c.Value.Text != want {
			t.Errorf("column %d = %+v, want the error %s", col, c.Value, want)
		}
	}
	if c := cellAt(sh, 0, 4); c == nil || c.Value.Kind != sheet.Number {
		t.Errorf("column 4 = %+v, want the number", c)
	}
}

// TestRead_GeneralDecimals locks the limit of decimals of the General
// format, read from the default cell style (ODF 1.3 Part 3, 20.255
// style:decimal-places), and no limit when the style states none.
func TestRead_GeneralDecimals(t *testing.T) {
	body := table("S", `<table:table-row><table:table-cell office:value-type="float" office:value="1.5"/></table:table-row>`)
	doc, _ := read(t, flat{styles: `<style:default-style style:family="table-cell"><style:table-cell-properties style:decimal-places="2"/></style:default-style>`, body: body}.bytes())
	if !doc.LimitGeneral || doc.GeneralDecimals != 2 {
		t.Errorf("limit %v %d, want 2", doc.LimitGeneral, doc.GeneralDecimals)
	}
	doc, _ = read(t, flat{body: body}.bytes())
	if doc.LimitGeneral {
		t.Errorf("limit %d without a default style", doc.GeneralDecimals)
	}
}
