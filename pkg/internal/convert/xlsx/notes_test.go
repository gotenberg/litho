package xlsx

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// noteWorkbook returns a workbook of one sheet whose page setup prints its
// notes as cellComments says, with a shown note on B2, by Ada, and a
// hidden one on A3.
func noteWorkbook(t *testing.T, cellComments string) []byte {
	t.Helper()
	setup := ""
	if cellComments != "" {
		setup = `<pageSetup cellComments="` + cellComments + `"/>`
	}
	sh := testSheet{
		name: "S",
		xml: `<sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>a</t></is></c></row><row r="3"><c r="A3" t="inlineStr"><is><t>b</t></is></c></row></sheetData>` +
			setup + `<legacyDrawing r:id="rIdV"/>`,
		rels: `<Relationship Id="rIdV" Type="` + relVMLDrawing + `" Target="../drawings/vmlDrawing1.vml"/>` +
			`<Relationship Id="rIdC" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments" Target="../comments1.xml"/>`,
	}
	note := func(row, col, anchor string, visible bool) string {
		v := ""
		if visible {
			v = `<x:Visible/>`
		}
		return `<v:shape style="position:absolute;visibility:visible" fillcolor="#ffffe1"><v:shadow on="t" color="black" obscured="t"/><x:ClientData ObjectType="Note"><x:Anchor>` + anchor + `</x:Anchor><x:Row>` + row + `</x:Row><x:Column>` + col + `</x:Column>` + v + `</x:ClientData></v:shape>`
	}
	files := workbookFiles([]testSheet{sh}, minimalStyles, "", "", map[string]string{
		"xl/comments1.xml": `<comments ` + nsDecl + `><authors><author>Ada</author></authors><commentList>` +
			`<comment ref="B2" authorId="0"><text><r><rPr><b/><sz val="9"/></rPr><t>Ada:</t></r><r><t xml:space="preserve">` + "\n" + `Shown.</t></r></text></comment>` +
			`<comment ref="A3" authorId="0"><text><t>Hidden.</t></text></comment></commentList></comments>`,
		"xl/drawings/vmlDrawing1.vml": `<xml xmlns:v="urn:schemas-microsoft-com:vml" xmlns:o="urn:schemas-microsoft-com:office:office" xmlns:x="urn:schemas-microsoft-com:office:excel">` +
			note("1", "1", "2, 15, 0, 10, 4, 15, 4, 4", true) + note("2", "0", "1, 15, 1, 10, 3, 15, 5, 4", false) + `</xml>`,
	})
	return zipFiles(t, files)
}

// TestNotes_AsThePageSetupAsks reads the notes of a sheet as its page
// setup's cellComments asks them printed (ECMA-376 Part 1, 18.18.5):
// none by default, at the end with each note's cell and text, the author's
// run included, or as displayed, the shown note drawn with its arrow.
func TestNotes_AsThePageSetupAsks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		cellComments string
		want         sheet.PrintNotes
		notes        int
		drawings     int
	}{
		{"", sheet.NotesNone, 0, 0},
		{"none", sheet.NotesNone, 0, 0},
		{"atEnd", sheet.NotesAtEnd, 2, 0},
		{"asDisplayed", sheet.NotesAsDisplayed, 2, 2},
	} {
		doc, warnings := readBytes(t, noteWorkbook(t, tc.cellComments))
		if len(warnings) > 0 {
			t.Errorf("%q: warnings %v", tc.cellComments, warnings)
		}
		sh := doc.Sheets[0]
		if sh.Page.Notes != tc.want || len(sh.Notes) != tc.notes || len(sh.Drawings) != tc.drawings {
			t.Fatalf("%q: print %v, %d notes, %d drawings", tc.cellComments, sh.Page.Notes, len(sh.Notes), len(sh.Drawings))
		}
		if tc.notes == 0 {
			continue
		}
		if n := sh.Notes[0]; n.Row != 1 || n.Column != 1 || n.Text != "Ada:\nShown." || !n.Shown {
			t.Errorf("%q: first note %+v", tc.cellComments, n)
		}
		if n := sh.Notes[1]; n.Row != 2 || n.Column != 0 || n.Text != "Hidden." || n.Shown {
			t.Errorf("%q: second note %+v", tc.cellComments, n)
		}
		if tc.drawings == 0 {
			continue
		}
		arrow, box := sh.Drawings[0], sh.Drawings[1]
		// The arrow spans the box's top-left corner, column C 15 pixels in
		// and row 1 10 pixels down, and the top-right corner of B2.
		if arrow.From.Column != 2 || arrow.From.DX != 0 || arrow.From.Row != 0 || arrow.From.DY != 7.5 ||
			arrow.To.Column != 2 || arrow.To.DX != 11.25 || arrow.To.Row != 1 || arrow.To.DY != 0 {
			t.Errorf("arrow from %+v to %+v", arrow.From, arrow.To)
		}
		var texts []string
		var walk func([]typeset.Fragment)
		walk = func(fs []typeset.Fragment) {
			for _, f := range fs {
				if tx, ok := f.(*typeset.Text); ok {
					for _, g := range tx.Glyphs {
						texts = append(texts, g.Text)
					}
				}
			}
		}
		walk(box.Picture.Vector(t.Context(), typeset.Rect{Width: 140, Height: 50}))
		if got := strings.Join(texts, ""); !strings.Contains(got, "Ada:") || !strings.Contains(got, "Shown.") {
			t.Errorf("box draws %q", got)
		}
	}
}
