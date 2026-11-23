package html

import (
	"strings"
	"testing"
)

// TestQuirksMode_FillMakesNoPage keeps the height the viewport fill adds
// to the html and body elements of a quirks mode document inside the page
// it is on (Quirks Mode Standard, 3.6 and 3.7): a document converts to as
// many pages as with an HTML5 DOCTYPE, whatever takes room from its pages
// (a footnote area, a note area, bands that grow the margins, page-margin
// boxes), when its content runs over a page, in columns, and under a
// height or min-height of the body.
func TestQuirksMode_FillMakesNoPage(t *testing.T) {
	const page = `@page { size: 300pt 200pt; margin: 20pt } body { font: 10pt "Test Serif" } p { margin: 0 }`
	long := strings.Repeat("<p>line</p>", 40)
	for _, tc := range []struct {
		name, src string
		o         Options
		pages     int
	}{
		{"footnote", `<style>` + page + ` .fn { float: footnote }</style><p>One<span class=fn>Note</span> two.</p>`, Options{PreferCSSPageSize: true}, 1},
		{"note area", `<style>` + page + ` @page { @note-area { content: element(note, all-once) } } .n { float: note }</style><p>One<span class=n>Note</span>.</p>`, Options{PreferCSSPageSize: true}, 1},
		{"footnote past a page", `<style>` + page + ` .fn { float: footnote }</style>` + long + `<p>x<span class=fn>Note</span></p>`, Options{PreferCSSPageSize: true}, 0},
		{"content past a page", `<style>` + page + `</style>` + long, Options{PreferCSSPageSize: true}, 0},
		{"bands", `<p>one</p>`, bandOptions(Band{HTML: `<p>H</p>`, Room: RoomContent}, Band{Footer: true, HTML: `<p>F</p>`, Room: RoomContent}), 1},
		{"margin boxes", `<style>` + page + ` @page { @top-center { content: "top" } @bottom-center { content: "bottom" } }</style><p>one</p>`, Options{PreferCSSPageSize: true}, 1},
		{"columns", `<style>` + page + ` body { columns: 2 }</style>` + long, Options{PreferCSSPageSize: true}, 0},
		{"height", `<style>` + page + ` body { height: 50pt }</style><p>one</p>`, Options{PreferCSSPageSize: true}, 1},
		{"min-height", `<style>` + page + ` body { min-height: 50pt }</style><p>one</p>`, Options{PreferCSSPageSize: true}, 1},
	} {
		quirks := len(convertDoc(t, tc.src, tc.o).Pages)
		standard := len(convertDoc(t, "<!DOCTYPE html>"+tc.src, tc.o).Pages)
		if quirks != standard || tc.pages > 0 && quirks != tc.pages {
			t.Errorf("%s: %d pages in quirks mode, %d with a DOCTYPE", tc.name, quirks, standard)
		}
	}
}
