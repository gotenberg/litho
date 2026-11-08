package docx

import "github.com/gotenberg/litho/pkg/internal/css"

// This file holds the mappings of the layout engine's Office fields that
// no WordprocessingML property maps onto one to one: columns of
// different widths (css.Columns.Tracks), vertical text in cells and
// text boxes (css.Style.WritingMode), and the wrap, chains and stroked
// borders of css.Office.

// columnTrack is one column of a section of columns of different widths
// (ECMA-376 Part 1, 17.6.3 col): its width w and the space after it, in
// points. It maps one to one onto css.ColumnTrack{Width: w, Gap: space}.
type columnTrack struct {
	w, space float64
}

// setColumnTracks gives a section's block the columns of different
// widths tracks lists (17.6.4 cols with equalWidth false): each track's
// Width is its share of the content width less the gaps, its Gap the
// room after it, as [css.ColumnTrack] holds them.
func setColumnTracks(st *css.Style, tracks []columnTrack) {
	st.Columns.Tracks = make([]css.ColumnTrack, len(tracks))
	for i, t := range tracks {
		st.Columns.Tracks[i] = css.ColumnTrack{Width: t.w, Gap: t.space}
	}
}

// setWritingMode turns a table cell's or a text box's content by its
// text direction, dir in the names of 17.18.93 ST_TextDirection (the
// DrawingML bodyPr vert and the VML layout-flow are mapped onto them by
// [textDirection]): btLr reads bottom to top, tbRl top to bottom, a
// quarter turn of horizontal lines. Any other value keeps them
// horizontal.
func setWritingMode(st *css.Style, dir string) {
	switch dir {
	case "btLr":
		st.WritingMode = css.SidewaysLR
	case "tbRl":
		st.WritingMode = css.VerticalRL
	}
}

// setFrameWrap sets how the text wraps around the absolutely positioned
// drawing st by the name of its anchor's wrap element, wrap, and that
// element's wrapText (ECMA-376 Part 1, 20.4.2.15 to 20.4.2.19, 20.4.3.7
// ST_WrapText), as [css.FrameWrap] maps them: square, tight and through
// wrapping leave the text on the sides wrapText says, both by default;
// top and bottom keeps it above and below; no wrapping lets it run
// through.
func setFrameWrap(st *css.Style, wrap, wrapText string) {
	switch wrap {
	case "wrapSquare", "wrapTight", "wrapThrough":
		switch wrapText {
		case "left":
			st.FrameWrap = css.FrameWrapLeft
		case "right":
			st.FrameWrap = css.FrameWrapRight
		case "largest":
			st.FrameWrap = css.FrameWrapLargest
		default:
			st.FrameWrap = css.FrameWrapParallel
		}
	case "wrapTopAndBottom":
		st.FrameWrap = css.FrameWrapNone
	default:
		st.FrameWrap = css.FrameWrapThrough
	}
}

// setWrapGap sets the room the text keeps from the wrapped drawing st,
// its anchor's distT, distR, distB and distL in points (20.4.2.3), in
// the order of [css.Side].
func setWrapGap(st *css.Style, dist [4]float64) {
	st.WrapGap = dist
}

// setChain makes the text box st the link seq of the chain of linked
// text boxes id (MS-ODRAWXML, 2.3.4: wps:txbx id holds the chain's
// content, link 0; wps:linkedTxbx id and seq name the others). A link
// is h tall, its content never growing it, as Writer imports a linked
// box.
func setChain(st *css.Style, id string, seq int, h float64) {
	st.Chain = id
	st.ChainIndex = max(seq, 0)
	st.Height = css.Pts(max(h, 0))
	st.MinHeight = css.Pts(0)
}

// strokeBorders paints the borders of st as Writer prints them, each
// side a stroked line at least [css.MinBorderStroke] wide
// ([css.Office.StrokeBorders]).
func strokeBorders(st *css.Style) {
	st.StrokeBorders = true
}

// setPageTop starts the floating table st, positioned y points below its
// page's top edge (17.4.58 tblpPr vertAnchor page, tblpY), at least that
// low on its page, as Writer places it, the flow before it ending higher
// ([css.Office.PageTop]). A y of 0 or less sets none.
func setPageTop(st *css.Style, y float64) {
	if y > 0 {
		st.PageTop = y
	}
}
