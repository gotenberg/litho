package odt

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// This file holds the mappings that wait for fields the layout cell adds
// beside this tree: css.Columns.Tracks (css/multicol.go) and
// css.Style.WritingMode (css/writing.go). Each hook computes what Writer
// lays out and states, in its comment, the one assignment the merge adds;
// until then it keeps the approximation its warning names.

// columnTrack is one column of a style:columns whose style:column
// elements give widths and indents, as css.ColumnTrack holds it: Width
// its share of the content width, Gap the room after it.
type columnTrack struct {
	Width, Gap float64
}

// applyColumnTracks lays the columns of st out at the widths tracks give
// (ODF 1.3, Part 3, 17.13 style:column), the first column's start indent
// and the last one's end indent being st's padding, start and end.
//
// At the merge: st.Columns.Tracks = []css.ColumnTrack{{Width: t.Width,
// Gap: t.Gap}, ...} for tracks, in order, and the warning goes. Until
// then the columns are equal, the gap the first column's end indent plus
// the second's start indent.
func (b *builder) applyColumnTracks(st *css.Style, tracks []columnTrack, start, end float64) {
	st.Padding[css.Left] = css.Pts(st.Padding[css.Left].Value + start)
	st.Padding[css.Right] = css.Pts(st.Padding[css.Right].Value + end)
	for _, t := range tracks[1:] {
		if math.Abs(t.Width-tracks[0].Width) > 0.01 {
			b.warn("column-widths", "columns of different widths are laid out equal")
			return
		}
	}
}

// applyWritingMode turns the content of a frame or a table cell whose
// style:writing-mode is mode (ODF 1.3, Part 3, 20.394): bt-lr reads
// bottom to top, tb-rl top to bottom, a quarter turn of horizontal lines.
//
// At the merge it takes the box's style st and sets st.WritingMode =
// css.SidewaysLR for bt-lr and css.VerticalRL for tb-rl, and the warning
// goes. Until then the content is laid out horizontally.
func (b *builder) applyWritingMode(mode string) {
	switch mode {
	case "bt-lr", "tb-rl":
		b.warn("writing-mode-"+mode, "text written "+mode+" is laid out horizontally")
	}
}
