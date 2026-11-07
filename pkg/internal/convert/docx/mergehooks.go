package docx

import "github.com/gotenberg/litho/pkg/internal/css"

// This file holds the hooks that fill the layout contract fields the
// layout cell added after this branch began, which this tree does not
// declare: css.Columns.Tracks and css.Style.WritingMode. Every call site
// is in place; until the hooks are wired, each reports false and its
// caller lays the construct out as before, with a warning.

// columnTrack is one column of a section of columns of different widths
// (ECMA-376 Part 1, 17.6.3 col): its width w and the space after it, in
// points. It maps one to one onto css.ColumnTrack{Width: w, Gap: space}.
type columnTrack struct {
	w, space float64
}

// setColumnTracks gives a section's block the columns of different
// widths tracks lists: st.Columns.Tracks = []css.ColumnTrack{{Width:
// t.w, Gap: t.space}, ...}, Width a relative share of the content width
// less the gaps. It reports whether it set them.
func setColumnTracks(_ *css.Style, _ []columnTrack) bool { return false }

// setWritingMode turns a table cell's or a text box's content by its
// text direction, dir in the names of 17.18.93 ST_TextDirection (the
// DrawingML bodyPr vert and the VML layout-flow are mapped onto them):
// btLr sets st.WritingMode = css.SidewaysLR, tbRl css.VerticalRL. It
// reports whether it set one.
func setWritingMode(_ *css.Style, _ string) bool { return false }
