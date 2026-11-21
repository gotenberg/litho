package html

import "testing"

// TestTableCellBaseline_BottomOfContent holds the baseline of a cell
// without a line box (CSS 2.1, 17.5.3): the bottom of its content edge,
// so a baseline-aligned cell holding a block sits on the bottom of a
// taller block in the next cell; an empty cell, or one holding only an
// absolutely positioned box, takes no part, its row as tall as it is.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 aligns a 40px
//     block on the bottom of a 100px block, and a row of an empty cell
//     of 50px bottom padding beside a 40px block is 50px tall.
func TestTableCellBaseline_BottomOfContent(t *testing.T) {
	t.Parallel()
	got := pxBoxes(convertDoc(t, anchorHead+`<style>td { padding: 0; vertical-align: baseline } table { border-spacing: 0 }</style>
		<table><tr><td><div class=g style="width: 20px; height: 40px"></div></td>
		<td><div class=k style="width: 20px; height: 100px"></div></td></tr></table>
		<table><tr><td class=r style="padding-bottom: 50px; width: 20px"></td>
		<td><div class=m style="width: 20px; height: 40px"></div><div style="position: absolute"></div></td></tr></table>`,
		Options{PreferCSSPageSize: true}))[0]
	for class, want := range map[string]string{"g": "0,60+20x40", "k": "20,0+20x100", "r": "0,100+20x50", "m": "20,100+20x40"} {
		if got[class] != want {
			t.Errorf("%s: %q, want %q", class, got[class], want)
		}
	}
}
