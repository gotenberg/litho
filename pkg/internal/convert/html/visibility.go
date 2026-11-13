package html

import (
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// hiddenContents returns the node of an element of content-visibility:
// hidden: its box alone, under size, layout, style and paint containment,
// its contents skipped (CSS Contain 2, 4.1); contain-intrinsic-size sizes
// it.
//
// # Corpus evidence
//
//   - probe, html: Chromium prints a div of content-visibility: hidden
//     with its border and background and without its text, as tall as
//     its contain-intrinsic-size or as its padding and border alone, and
//     a div of content-visibility: auto with its text.
func hiddenContents(st *css.Style) *layout.Node {
	c := *st
	c.Contain |= css.ContainSize | css.ContainLayout | css.ContainStyle | css.ContainPaint
	return &layout.Node{Kind: layout.Element, Style: &c}
}
