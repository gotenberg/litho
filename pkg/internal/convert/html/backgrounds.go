package html

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// withoutBackground returns a copy of st without a background colour or
// image: the style, under [Options.OmitBackgrounds], of the html and body
// elements and of every element that asks for economy.
//
// With the canvas background off, the root prints exact colours and
// passes them on; an element whose print-color-adjust computes to
// economy, because it or an ancestor below the root says so, gives up its
// background colour and image, and keeps its borders and shadows (CSS
// Color Adjust 1, 3.1). The canvas, which the html and body elements
// paint (CSS 2.1, 14.2), goes blank; every other background, the page
// box's included, prints as styled.
//
// This is what Gotenberg's Chromium route prints: it injects html {
// -webkit-print-color-adjust: exact !important } into every page, and,
// when backgrounds are not printed, html, body { background: none
// !important } (gotenberg/pkg/modules/chromium/tasks.go,
// forceExactColorsActionFunc; Gotenberg issue 1154).
//
// # Corpus evidence
//
//   - probe, html: the pinned Gotenberg route with printBackground=false
//     prints a div of print-color-adjust: economy, or of
//     -webkit-print-color-adjust: economy, without its background colour
//     or gradient, with its border and box shadow; with
//     printBackground=true it prints the background.
func withoutBackground(st *css.Style) *css.Style {
	if st == nil || st.BackgroundColor.A == 0 && st.BackgroundImage == "" && st.BackgroundGradient == nil {
		return st
	}
	c := *st
	c.BackgroundColor = css.Color{}
	c.BackgroundImage = ""
	c.BackgroundGradient = nil
	return &c
}

// exactColors is the rule Gotenberg's route injects into every page.
const exactColors = "html { -webkit-print-color-adjust: exact !important }"

// exactColorsSheet returns the rule Gotenberg's route injects, last of
// the author's sheets, so the root and what inherits from it print exact
// colours ([withoutBackground]).
func exactColorsSheet(ctx context.Context) css.Sheet {
	// A constant rule parses without error.
	s, _ := css.Parse(ctx, []byte(exactColors), css.ParseOptions{})
	return css.Sheet{Sheet: s, Origin: css.Author}
}
