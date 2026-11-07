package odt

import "github.com/gotenberg/litho/pkg/internal/css"

// This file holds the mappings of the layout engine's Office fields that
// no ODF property maps onto one to one: columns of different widths
// (css.Columns.Tracks) and vertical writing modes (css.Style.WritingMode).

// columnTrack is one column of a style:columns whose style:column
// elements give widths and indents, as css.ColumnTrack holds it: Width
// its share of the content width, Gap the room after it.
type columnTrack struct {
	Width, Gap float64
}

// applyColumnTracks lays the columns of st out at the widths tracks give
// (ODF 1.3, Part 3, 17.13 style:column), the first column's start indent
// and the last one's end indent being st's padding, start and end: each
// track's Width is its share of the content width less the indents, its
// Gap the room after it, as [css.ColumnTrack] holds them.
func applyColumnTracks(st *css.Style, tracks []columnTrack, start, end float64) {
	st.Padding[css.Left] = css.Pts(st.Padding[css.Left].Value + start)
	st.Padding[css.Right] = css.Pts(st.Padding[css.Right].Value + end)
	st.Columns.Tracks = make([]css.ColumnTrack, len(tracks))
	for i, t := range tracks {
		st.Columns.Tracks[i] = css.ColumnTrack{Width: t.Width, Gap: t.Gap}
	}
}

// applyWritingMode turns the content of the frame or table cell styled st
// whose style:writing-mode is mode (ODF 1.3, Part 3, 20.394): bt-lr reads
// bottom to top, tb-rl top to bottom, a quarter turn of horizontal lines.
// Any other value keeps the horizontal lines.
func applyWritingMode(st *css.Style, mode string) {
	switch mode {
	case "bt-lr":
		st.WritingMode = css.SidewaysLR
	case "tb-rl":
		st.WritingMode = css.VerticalRL
	}
}
