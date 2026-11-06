package html

import "github.com/gotenberg/litho/pkg/internal/css"

// withoutBackground returns a copy of st without a background colour or
// image: the style of the html and body elements of a conversion under
// [Options.OmitBackgrounds].
//
// Gotenberg's Chromium route does not print with Chromium's own economy
// mode: it injects html { -webkit-print-color-adjust: exact !important }
// into every page, and, when backgrounds are not printed, html, body {
// background: none !important } (gotenberg/pkg/modules/chromium/tasks.go,
// forceExactColorsActionFunc; Gotenberg issue 1154). The canvas, which
// those two elements paint (CSS 2.1, 14.2), goes blank; every other
// background, the page box's included, prints as styled.
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
