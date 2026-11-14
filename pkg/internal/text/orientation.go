package text

import (
	"unicode"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font/opentype"
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
	// OrientCombine sets the run as one upright character one em high,
	// CSS text-combine-upright: all (CSS Writing Modes 4, 9.1): its
	// characters side by side as a horizontal line, narrowed to fit the
	// em, with no break between them.
	OrientCombine
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
	if o == OrientNone || o == OrientCombine {
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

// combineFeatures are the OpenType features that draw two, three and four
// characters in a fraction of an em (OpenType feature registry, hwid,
// twid and qwid), which Chromium asks of a combined run wider than an em
// (CSS Writing Modes 4, 9.1.2 leaves the means to the user agent): by its
// character count, four and more taking qwid.
var combineFeatures = [...]opentype.Tag{
	opentype.MustNewTag("hwid"), opentype.MustNewTag("twid"), opentype.MustNewTag("qwid"),
}

// combineWidth returns out, the run in shaped for text-combine-upright:
// shaped again with the feature of [combineFeatures] its character count
// asks for when it is wider than an em.
func (st *layoutState) combineWidth(in shaping.Input, out shaping.Output) shaping.Output {
	n := in.RunEnd - in.RunStart
	if n < 2 || out.Advance <= in.Size {
		return out
	}
	in.FontFeatures = append(in.FontFeatures[:len(in.FontFeatures):len(in.FontFeatures)], shaping.FontFeature{Tag: combineFeatures[min(n, 4)-2], Value: 1})
	narrow := st.kit.shaper.Shape(in)
	rescale(&narrow, in.Size)
	st.instanceAdvances(&narrow, in.Size)
	return narrow
}

// combine sets the glyphs of it, a run of text-combine-upright shaped as
// a horizontal line, as one upright character an em high at the font
// size em: the line, narrowed by a scale when it is still wider than the
// em, is centred across the vertical line on the central baseline, its em
// box filling the em along it, and its characters break nowhere (CSS
// Writing Modes 4, 9.1.2). The em advance sits on the last glyph.
func (p *Paragraph) combine(it *shapedItem, em float64) {
	width := 0.0
	for _, g := range it.glyphs {
		width += g.advance
	}
	k := 1.0
	if width > em {
		k = em / width
		it.squeeze = k
	}
	center := float64((it.emDescent - it.emAscent) / 2)
	// The em box's left edge on the page is the frame's far side of the
	// central baseline; each glyph sits that far from it.
	left := center + float64(em/2) - float64((em-float64(width*k))/2)
	pen := 0.0
	for i := range it.glyphs {
		g := &it.glyphs[i]
		x := float64((pen + g.xoff) * k)
		g.xoff, g.yoff = it.emAscent+g.yoff, left-x
		pen += g.advance
		g.advance = 0
	}
	if n := len(it.glyphs); n > 0 {
		it.glyphs[n-1].advance = em
	}
	it.upright = true
	for i := it.start; i < it.end && i < len(p.adv); i++ {
		p.adv[i] = 0
		if i > it.start {
			p.brk[i] = NoBreak
		}
	}
	if n := len(it.glyphs); n > 0 {
		if c := it.glyphs[n-1].cluster(); c < len(p.adv) {
			p.adv[c] = em
		}
	}
}
