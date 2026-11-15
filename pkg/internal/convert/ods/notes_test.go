package ods

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
)

// TestNotes_PrintedAsThePageStyleAsks reads a cell's office:annotation as
// its note, its paragraphs a line each and whether Calc shows it, and the
// page style's style:print with annotations as notes printed at the end
// (ODF 1.3, Part 3, 14.1 and 20.325).
func TestNotes_PrintedAsThePageStyleAsks(t *testing.T) {
	t.Parallel()
	for _, print := range []string{"", "annotations grid"} {
		props := ``
		if print != "" {
			props = ` style:print="` + print + `"`
		}
		doc, w := read(t, flat{
			auto:   `<style:page-layout style:name="pm1"><style:page-layout-properties fo:page-width="21cm" fo:page-height="29.7cm"` + props + `/></style:page-layout>`,
			master: `<style:master-page style:name="Default" style:page-layout-name="pm1"/>`,
			body: table("S", `<table:table-row><table:table-cell office:value-type="string"><office:annotation office:display="true"><dc:creator>Ada</dc:creator>`+
				`<text:p>One<text:s text:c="2"/>two</text:p><text:p>Three</text:p></office:annotation><text:p>x</text:p></table:table-cell></table:table-row>`),
		}.bytes())
		if len(w) > 0 {
			t.Errorf("%q: warnings %v", print, w)
		}
		sh := doc.Sheets[0]
		if len(sh.Notes) != 1 || sh.Notes[0].Text != "One  two\nThree" || !sh.Notes[0].Shown || sh.Notes[0].Row != 0 || sh.Notes[0].Column != 0 {
			t.Errorf("%q: notes %+v", print, sh.Notes)
		}
		if want := print != ""; (sh.Page.Notes == sheet.NotesAtEnd) != want {
			t.Errorf("%q: notes print %v", print, sh.Page.Notes)
		}
	}
}
