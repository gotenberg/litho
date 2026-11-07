package layout

import "github.com/gotenberg/litho/pkg/internal/css"

// FootnoteRule is the separator a page draws between its body and its
// footnote area, as Writer's style:footnote-sep (ODF 1.3 Part 3, 17.19)
// and Word's separator footnote (ECMA-376 Part 1, 17.11, the w:separator
// mark) place it: a horizontal rule of Thickness points in Color, Width
// times the footnote area's width long, at the area's start, centre or
// end per Align, with SpaceAbove points between the body text and the
// rule and SpaceBelow between the rule and the first note. The rule and
// its spaces take room from the page area as the notes do, and are drawn
// only on a page whose footnote area holds a note.
type FootnoteRule struct {
	// Width is the rule's length as a fraction of the footnote area's
	// width, 0 to 1; Writer's style:rel-width, Word's separator being
	// one third of the text width. 0 draws no rule but keeps the spaces.
	Width float64
	// Thickness is the rule's stroke width in points, finite and at least
	// 0; 0 draws a hairline.
	Thickness float64
	// Color is the rule's colour; its Alpha 0 draws it in the body's text
	// colour.
	Color css.Color
	// Align places the rule in the footnote area: [css.AlignLeft],
	// [css.AlignCenter] or [css.AlignRight]; start and end follow the
	// page's direction, and any other value reads as left.
	Align css.TextAlign
	// Line is the rule's line style: [css.Solid], [css.Dotted] or
	// [css.Dashed]; [css.BorderNone] draws none, and any other value
	// draws solid.
	Line css.BorderStyle
	// SpaceAbove and SpaceBelow are the room in points left above and
	// below the rule, finite and at least 0.
	SpaceAbove, SpaceBelow float64
}
