package odt

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// This file holds the mappings that wait for the layout engine's Office
// fields (css.Style.LineRule, css.Style.PageAnchor and
// css.DecorationCloneAll), filled in round 2 once the layout fields land
// in this tree. Until then each function approximates the behaviour with
// the fields the engine already has, and its comment states the round-2
// mapping.

// lineRule sets the line height of a paragraph block st from the
// paragraph's line spacing p, its text properties being t (ODF 1.3, Part
// 3, 20.317, 20.335 and 20.336).
//
// Round 2 sets LineRule: a percentage is LineRuleMultiple with LineHeight
// Number(p.lineVal), the natural baseline kept and the room below; a
// length is LineRuleExact with LineHeight Points, the baseline at four
// fifths of the height; style:line-height-at-least stays MinLineHeight
// with LineHeight Normal; style:line-spacing is LineRuleMultiple of 1 with
// the leading added below. Until then a percentage multiplies the face's
// natural line height through a CSS number, which centres the extra room
// on the baseline where Writer puts it below.
func (b *builder) lineRule(st *css.Style, p paraState, t textState) {
	switch p.line { //nolint:exhaustive // lineNormal keeps the initial line-height
	case lineProportional:
		st.LineHeight = css.Dimension{Kind: css.Number, Value: p.lineVal * b.lineRatio(t)}
	case lineFixed:
		st.LineHeight = css.Pts(p.lineVal)
	case lineAtLeast:
		st.LineHeight = css.Dimension{Kind: css.Normal}
		st.MinLineHeight = p.lineVal
	case lineSpacing:
		st.LineHeight = css.Dimension{Kind: css.Number, Value: b.lineRatio(t) + p.lineVal/math.Max(t.size, 1)}
	}
}

// pageAnchor positions the frame style st against the page, at x and y
// from the page box's top-left corner (ODF 1.3, Part 3, 19.756,
// text:anchor-type page, and 20.298, style:horizontal-rel page).
//
// Round 2 sets st.PageAnchor with Left and Top at x and y: the box is then
// placed on the page box, margins included, of the page its anchor lands
// on. Until then the box is positioned against the first page's area: x
// and y less that page's margins, right on the first page of the
// document alone.
func (b *builder) pageAnchor(st *css.Style, x, y float64) {
	st.Position = css.Absolute
	st.Left, st.Top = css.Pts(x-b.pageOrigin[0]), css.Pts(y-b.pageOrigin[1])
	b.warn("page-anchor", "frames anchored to a page are placed on the document's first page")
}

// cloneDecorations makes a bordered paragraph or cell st draw its whole
// border and padding on every page it spans, as Writer does at a page
// break.
//
// Round 2 sets st.BoxDecorationBreak to css.DecorationCloneAll. Until
// then the box is sliced at a page break, CSS's default.
func cloneDecorations(*css.Style) {}
