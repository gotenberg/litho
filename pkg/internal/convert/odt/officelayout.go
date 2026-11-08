package odt

import (
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// This file holds the mappings of the layout engine's Office fields that
// no ODF property maps onto one to one: columns of different widths
// (css.Columns.Tracks), vertical writing modes (css.Style.WritingMode),
// and the wrap, chains and stroked borders of css.Office.

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

// frameWraps maps the values of style:wrap (ODF 1.3 Part 3, 20.393) onto
// [css.FrameWrap]; a missing or unknown value lets the text run through,
// the zero value.
var frameWraps = map[string]css.FrameWrap{
	"none":        css.FrameWrapNone,
	"left":        css.FrameWrapLeft,
	"right":       css.FrameWrapRight,
	"parallel":    css.FrameWrapParallel,
	"dynamic":     css.FrameWrapDynamic,
	"biggest":     css.FrameWrapLargest,
	"run-through": css.FrameWrapThrough,
}

// applyFrameWrap sets how the text wraps around the page-anchored frame
// st by its style:wrap, wrap, keeping the room gap from it, its
// fo:margin in points in the order of [css.Side] (20.199 to 20.204).
func applyFrameWrap(st *css.Style, wrap string, gap [4]float64) {
	st.FrameWrap = frameWraps[wrap]
	st.WrapGap = gap
}

// chainLink is a text box's place in a chain of linked frames: the
// name of the chain's first frame, and the box's index from it.
type chainLink struct {
	name  string
	index int
}

// maxChain bounds the links followed in one chain. Anti-DoS guard, not
// spec-mandated.
const maxChain = 1 << 12

// scanChains maps the name of every frame of a chain of linked text
// boxes under root (ODF 1.3 Part 3, 19.112 draw:chain-next-name) onto
// its place in the chain, counted from the frame no other names. A
// chain closed on itself has no first frame and links none.
func scanChains(root *odf.Element) map[string]chainLink {
	next := map[string]string{}
	named := map[string]bool{}
	stack := []*odf.Element{root}
	for len(stack) > 0 {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if e == nil {
			continue
		}
		if e.Is(odf.NSDraw, "frame") {
			if tb := e.Child(odf.NSDraw, "text-box"); tb != nil {
				if name, to := e.A(odf.NSDraw, "name"), tb.A(odf.NSDraw, "chain-next-name"); name != "" && to != "" && to != name {
					next[name] = to
					named[to] = true
				}
			}
		}
		stack = append(stack, e.Children...)
	}
	if len(next) == 0 {
		return nil
	}
	out := map[string]chainLink{}
	for head := range next {
		if named[head] {
			continue
		}
		name := head
		for i := 0; i < maxChain && name != ""; i++ {
			if _, seen := out[name]; seen {
				break
			}
			out[name] = chainLink{name: head, index: i}
			name = next[name]
		}
	}
	return out
}

// applyChain makes the text box st the link l of its chain
// ([css.Office.Chain]).
func applyChain(st *css.Style, l chainLink) {
	st.Chain = l.name
	st.ChainIndex = l.index
}

// strokeBorders paints the borders of st as Writer prints them, each
// side a stroked line at least [css.MinBorderStroke] wide
// ([css.Office.StrokeBorders]).
func strokeBorders(st *css.Style) {
	st.StrokeBorders = true
}
