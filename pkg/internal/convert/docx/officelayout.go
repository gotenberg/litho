package docx

import "github.com/gotenberg/litho/pkg/internal/css"

// This file holds the mappings of the layout engine's Office fields that
// no WordprocessingML property maps onto one to one: columns of
// different widths (css.Columns.Tracks) and vertical text in cells and
// text boxes (css.Style.WritingMode).

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
