package odt

import (
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// TestCell_ClipsANestedTable clips the content of a cell that holds a
// table, which Writer lays out at its own width and cuts at the cell's
// edges, and leaves a cell of paragraphs alone.
func TestCell_ClipsANestedTable(t *testing.T) {
	inner := `<table:table><table:table-column/><table:table-row><table:table-cell><text:p>in</text:p></table:table-cell></table:table-row></table:table>`
	body := `<table:table><table:table-column table:number-columns-repeated="2"/><table:table-row>` +
		`<table:table-cell><text:p>a</text:p>` + inner + `</table:table-cell><table:table-cell><text:p>b</text:p></table:table-cell></table:table-row></table:table>`
	b := newTestBuilder(t, doc{body: body})
	root, _ := b.document(odfBody(b))
	var clips []bool
	walkNodes(root, func(n *layout.Node) {
		if n.Role == "TD" {
			clips = append(clips, n.Style.ClipOnly())
		}
	})
	if want := []bool{true, false, false}; !slices.Equal(clips, want) {
		t.Errorf("cells clip %v, want %v", clips, want)
	}
}

// TestConvert_CellStyleText sets a cell style's text and paragraph
// properties on the cell's paragraphs of the default style, under their
// automatic style, and not on a paragraph of another common style, as
// LibreOffice prints the same document (probe through the pinned image).
func TestConvert_CellStyleText(t *testing.T) {
	styles := `<style:style style:name="Big" style:family="paragraph"><style:paragraph-properties fo:text-align="start"/><style:text-properties fo:font-size="20pt"/></style:style>`
	auto := `<style:style style:name="ta" style:family="table"><style:table-properties style:width="15cm" table:align="left"/></style:style>` +
		`<style:style style:name="co" style:family="table-column"><style:table-column-properties style:column-width="5cm"/></style:style>` +
		`<style:style style:name="ce1" style:family="table-cell"><style:paragraph-properties fo:text-align="end"/><style:text-properties fo:font-size="8pt"/></style:style>` +
		`<style:style style:name="P20" style:family="paragraph" style:parent-style-name="Standard"><style:text-properties fo:font-size="20pt"/></style:style>`
	cell := func(p string) string {
		return `<table:table-cell table:style-name="ce1" office:value-type="string">` + p + `</table:table-cell>`
	}
	body := `<table:table table:style-name="ta"><table:table-column table:style-name="co" table:number-columns-repeated="3"/><table:table-row>` +
		cell(`<text:p>plain</text:p>`) + cell(`<text:p text:style-name="P20">auto</text:p>`) + cell(`<text:p text:style-name="Big">big</text:p>`) +
		`</table:table-row></table:table>`
	res := convertDoc(t, doc{styles: styles, contentAuto: auto, body: body}.pack(t), office.Options{})
	want := map[string]struct {
		size float64
		end  bool
	}{"plain": {8, true}, "auto": {20, true}, "big": {20, false}}
	for _, r := range runs(res.Pages[0]) {
		w, ok := want[r.text]
		if !ok {
			continue
		}
		delete(want, r.text)
		// The cells start at 56.7, 198.4 and 340.2 points.
		start := 56.7 + float64(slices.Index([]string{"plain", "auto", "big"}, r.text))*141.73
		if r.size != w.size || (r.x > start+20) != w.end {
			t.Errorf("%s at %.1f, size %v, want size %v, at the end %v", r.text, r.x, r.size, w.size, w.end)
		}
	}
	if len(want) > 0 {
		t.Errorf("not drawn: %v", want)
	}
}

// TestConvert_TableWithoutWidths spans a table that states no width, of
// its own or of its columns, across the text area, its columns sharing it.
func TestConvert_TableWithoutWidths(t *testing.T) {
	body := `<table:table><table:table-column table:number-columns-repeated="2"/><table:table-row>` +
		`<table:table-cell><text:p>L</text:p></table:table-cell><table:table-cell><text:p>R</text:p></table:table-cell></table:table-row></table:table>`
	res := convertDoc(t, doc{body: body}.pack(t), office.Options{})
	for _, r := range runs(res.Pages[0]) {
		// The text area runs from 56.7 to 538.6 points: the second
		// column starts at 297.6.
		if r.text == "R" && (r.x < 297 || r.x > 305) {
			t.Errorf("second cell's text at %.1f, want past 297.6", r.x)
		}
	}
}

// TestConvert_TableBodyBesideItsHeader locks the structure of a table
// with header rows: the THead holds them and a TBody the rows after them
// (ISO 32000-1, 14.8.4.3.4, Table 337; ISO 14289-1, 7.2), and a table
// without header rows holds its rows directly.
//
// # Corpus evidence
//
//   - litho/odt paper-*, libreoffice/odt odfimport/incorrectsum.odt and
//     four others: a THead beside rows of no TBody failed PDF/UA-1, 7.2
//     until a remediation regrouped them.
func TestConvert_TableBodyBesideItsHeader(t *testing.T) {
	cell := func(s string) string { return `<table:table-cell><text:p>` + s + `</text:p></table:table-cell>` }
	headed := `<table:table table:name="T"><table:table-column table:number-columns-repeated="2"/>` +
		`<table:table-header-rows><table:table-row>` + cell("h1") + cell("h2") + `</table:table-row></table:table-header-rows>` +
		`<table:table-row>` + cell("a") + cell("b") + `</table:table-row><table:table-row>` + cell("c") + cell("d") + `</table:table-row></table:table>`
	res := convertDoc(t, doc{body: headed}.pack(t), office.Options{})
	if got := roles(res.Structure); !strings.Contains(got, "Table,THead,TR,TH,P,TH,P,TBody,TR,TD,P,TD,P,TR,TD,P,TD,P") {
		t.Errorf("structure %s, want the header rows in a THead and the others in a TBody", got)
	}
	plain := `<table:table table:name="T"><table:table-column/><table:table-row>` + cell("a") + `</table:table-row></table:table>`
	if got := roles(convertDoc(t, doc{body: plain}.pack(t), office.Options{}).Structure); strings.Contains(got, "TBody") {
		t.Errorf("structure %s, want no TBody without header rows", got)
	}
}
