package text

import (
	"unicode"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/shaping"
)

//go:generate go run ../../../cmd/gen-vertical-orientation -out orientation_gen.go

// Orientation is how the characters of a run stand when the run is set in
// a vertical line (CSS Writing Modes 4, 5.1). The caller lays a vertical
// line out as a horizontal one that the page turns a quarter turn
// clockwise, which sets every character sideways; an upright character
// is turned back.
type Orientation uint8

// The orientations.
const (
	// OrientNone sets the run as a horizontal line: in a vertical line,
	// every character sideways. It is the zero value.
	OrientNone Orientation = iota
	// OrientMixed sets upright the characters whose Vertical_Orientation
	// is U, Tu or Tr and sideways the others, CSS text-orientation:
	// mixed.
	OrientMixed
	// OrientUpright sets every character upright, CSS text-orientation:
	// upright.
	OrientUpright
)

// uprightInMixed reports a character text-orientation: mixed sets upright:
// one whose Vertical_Orientation is U, Tu or Tr (CSS Writing Modes 4, 5.1;
// UAX #50, 5). UAX #50 turns a Tr character sideways when the font has no
// vertical form for it; Chromium sets it upright whatever the font holds,
// and so does litho.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (Gotenberg 9.4.1) draws “A” and ‘B’ in a
//     vertical-rl line of DejaVu Sans, which has no vert feature, with
//     the quotation marks upright and the letters sideways.
func uprightInMixed(r rune) bool {
	return unicode.In(r, verticalUpright, verticalTransformedUpright, verticalTransformedRotated)
}

// followsBase reports a character that takes the orientation of the
// character before it: a combining mark, a joiner or a variation
// selector, which belong to the cluster of their base (UAX #50, 4.2).
func followsBase(r rune) bool {
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc, unicode.Variation_Selector) || r == zeroWidthJoin || r == '‌'
}

// splitByOrientation cuts in, shaped in a style of orientation o, into
// pieces of one orientation each: the upright pieces shaped top to
// bottom, with the vertical forms and metrics of their face (OpenType
// vert, which HarfBuzz applies to vertical text), the sideways ones as in
// was. OrientNone keeps in whole.
func splitByOrientation(in shaping.Input, o Orientation) []shaping.Input {
	if o == OrientNone {
		return []shaping.Input{in}
	}
	vertical := func(x shaping.Input) shaping.Input {
		x.Direction = di.DirectionTTB
		x.Direction.SetSideways(false)
		return x
	}
	if o == OrientUpright {
		return []shaping.Input{vertical(in)}
	}
	var out []shaping.Input
	start := in.RunStart
	up := false
	for i := in.RunStart; i < in.RunEnd; i++ {
		r := in.Text[i]
		u := up
		if i == in.RunStart || !followsBase(r) {
			u = uprightInMixed(r)
		}
		if i > in.RunStart && u != up {
			piece := in
			piece.RunStart, piece.RunEnd = start, i
			if up {
				piece = vertical(piece)
			}
			out = append(out, piece)
			start = i
		}
		up = u
	}
	piece := in
	piece.RunStart = start
	if up {
		piece = vertical(piece)
	}
	return append(out, piece)
}
