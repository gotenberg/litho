package typography

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/graphics"
)

// The line parameters a Type 3 glyph description may set for its strokes,
// as bits of [type3PenState.set] and [type3PenStroke.inherit]: the line
// width, cap, join and miter limit of ISO 32000-1, 8.4.3.2 to 8.4.3.5. The
// dash pattern (8.4.3.6) is not among them: a dash paints no farther from
// the path than the line it cuts, and the square caps at its ends reach no
// farther than [strokeReach] already lets every square cap reach.
const (
	penWidth = 1 << iota
	penCap
	penJoin
	penMiter
	penAll = penWidth | penCap | penJoin | penMiter
)

// Line parameters that reach least in [strokeReach], held for a parameter
// a stroke inherits, so that the showing state's own wins over them: no
// width, a butt cap, a bevel join and a miter limit that never stretches a
// join past half the width (ISO 32000-1, 8.4.3.3 to 8.4.3.5, Tables 54 and
// 55).
const (
	leastCap   = 0
	leastJoin  = 2
	leastMiter = 0
)

// maxType3PenStrokes caps the strokes with an inherited pen one Type 3
// glyph records apart. Anti-DoS guard, not spec-mandated: each is grown at
// every show of the glyph, so a description stroking thousands of paths
// with the showing state's pen would cost thousands of steps per glyph.
// Past the cap, the strokes merge into one that reaches as far as any of
// them ([type3PenStroke.merge]), which bounds every one of them, a little
// wider. A real glyph strokes a few paths, so the cap is far above what it
// meets.
const maxType3PenStrokes = 64

// type3PenState is the line width, cap, join and miter limit a Type 3 glyph
// description strokes with, and the [penWidth] bits of those it set itself.
type type3PenState struct {
	lw     float64
	lc, lj int
	ml     float64
	set    int
}

// type3PenStroke is one stroke of a Type 3 glyph description that takes
// some of its line parameters from the graphics state the glyph is shown in
// (ISO 32000-1, 9.6.5): the box of its path in glyph space, the clip box it
// is painted under (nil for none), how far the description's own CTM
// stretches the pen ([maxStretch]), whether the path joins segments, the
// [penWidth] bits of the parameters it inherits, and the values of those it
// sets. An inherited parameter holds the value that reaches least, which
// the showing state's overrides.
type type3PenStroke struct {
	box     [4]float64
	clip    *[4]float64
	stretch float64
	joined  bool
	inherit int
	lw      float64
	lc, lj  int
	ml      float64
}

// newType3PenStroke returns the stroke of the path whose box in glyph space
// is box, under clip, with the line parameters of pen through the
// description's matrix m.
func newType3PenStroke(box [4]float64, clip *[4]float64, pen type3PenState, m [6]float64, joined bool) type3PenStroke {
	s := type3PenStroke{
		box: box, clip: clip, stretch: maxStretch(m), joined: joined, inherit: penAll &^ pen.set,
		lw: pen.lw, lc: pen.lc, lj: pen.lj, ml: pen.ml,
	}
	if s.inherit&penWidth != 0 {
		s.lw = 0
	}
	if s.inherit&penCap != 0 {
		s.lc = leastCap
	}
	if s.inherit&penJoin != 0 {
		s.lj = leastJoin
	}
	if s.inherit&penMiter != 0 {
		s.ml = leastMiter
	}
	return s
}

// type3Pen is the strokes of one Type 3 glyph that take their pen from
// where the glyph is shown, at most [maxType3PenStrokes] of them. It is
// read-only once [type3PaintBounds] returns it.
//
// # Corpus evidence
//
//   - All corpora: 135 files hold a Type 3 font. 523 glyphs of 27 fonts in
//     six files stroke with an inherited line parameter (pdfjs issue19624.pdf
//     the miter limit alone, 511 glyphs); none is shown under a pen reaching
//     past the initial one, and over the first ten pages of the 135 files no
//     text and no ink box moves.
//   - litho t3pen_2b.pdf: a line stroked under the showing state's 2000 w.
//     The centred quarter over its pen left 720 pixels inside the region, and
//     now removes the glyph.
type type3Pen struct {
	strokes []type3PenStroke
}

// add returns p with the stroke s, merged into the last one past
// [maxType3PenStrokes]. A nil p is the pen of no stroke.
func (p *type3Pen) add(s type3PenStroke) *type3Pen {
	if p == nil {
		return &type3Pen{strokes: []type3PenStroke{s}}
	}
	if n := len(p.strokes); n >= maxType3PenStrokes {
		p.strokes[n-1] = p.strokes[n-1].merge(s)
		return p
	}
	p.strokes = append(p.strokes, s)
	return p
}

// merge returns one stroke that reaches at least as far as s and o: their
// boxes and clips joined, the larger stretch, the parameters either
// inherits inherited, and each value the one reaching farther
// ([strokeReach]).
func (s type3PenStroke) merge(o type3PenStroke) type3PenStroke {
	out := type3PenStroke{
		box:     [4]float64{min(s.box[0], o.box[0]), min(s.box[1], o.box[1]), max(s.box[2], o.box[2]), max(s.box[3], o.box[3])},
		stretch: max(s.stretch, o.stretch),
		joined:  s.joined || o.joined,
		inherit: s.inherit | o.inherit,
		lw:      max(math.Abs(s.lw), math.Abs(o.lw)),
		lc:      widerCap(s.lc, o.lc),
		lj:      widerJoin(s.lj, o.lj),
		ml:      max(miterOrInitial(s.ml), miterOrInitial(o.ml)),
	}
	if s.clip != nil && o.clip != nil {
		// unionBox grows its first box in place, and a clip box is shared
		// with the walk's saved states, so the union starts from a copy.
		out.clip = unionBox(unionBox(nil, s.clip), o.clip)
	}
	return out
}

// reach returns how far, in glyph space, the stroke's pen reaches past its
// path when the glyph is shown in state: each inherited parameter is the
// showing state's, or the stroke's own where merged strokes set one that
// reaches farther.
func (s type3PenStroke) reach(state *graphics.State) float64 {
	lw, lc, lj, ml := s.lw, s.lc, s.lj, s.ml
	if s.inherit&penWidth != 0 {
		lw = max(math.Abs(state.LineWidth), math.Abs(lw))
	}
	if s.inherit&penCap != 0 {
		lc = widerCap(state.LineCap, lc)
	}
	if s.inherit&penJoin != 0 {
		lj = widerJoin(state.LineJoin, lj)
	}
	if s.inherit&penMiter != 0 {
		ml = max(miterOrInitial(state.MiterLimit), miterOrInitial(ml))
	}
	return strokeReach(lw, lj, lc, ml, [6]float64{s.stretch, 0, 0, s.stretch, 0, 0}, s.joined)
}

// widerCap returns the projecting square cap when either a or b is one,
// the one cap [strokeReach] lets reach past half the width, and a
// otherwise.
func widerCap(a, b int) int {
	const squareCap = 2
	if b == squareCap {
		return b
	}
	return a
}

// widerJoin returns the miter join when either a or b strokes as one, a
// join outside Table 55 among them ([strokeReach]), and a otherwise.
func widerJoin(a, b int) int {
	const miterJoin, bevelJoin = 0, 2
	if b <= miterJoin || b > bevelJoin {
		return miterJoin
	}
	return a
}

// miterOrInitial returns ml, or the initial miter limit for one that is
// not a number, as [strokeReach] reads it.
func miterOrInitial(ml float64) float64 {
	if math.IsNaN(ml) {
		return initialMiterLimit
	}
	return ml
}

// penBox returns the ink box of the Type 3 glyph ink in glyph space when it
// is shown in state: its box, grown by each stroke of its description that
// strokes with the pen of state (ISO 32000-1, 9.6.5), that stroke's path
// box widened by the pen's reach and cut to the clip it is painted under.
func (ink type3InkBounds) penBox(state *graphics.State) [4]float64 {
	box := ink.box
	if ink.pen == nil {
		return box
	}
	for _, s := range ink.pen.strokes {
		r := s.reach(state)
		b := [4]float64{s.box[0] - r, s.box[1] - r, s.box[2] + r, s.box[3] + r}
		if c := s.clip; c != nil {
			b = [4]float64{max(b[0], c[0]), max(b[1], c[1]), min(b[2], c[2]), min(b[3], c[3])}
			if b[0] > b[2] || b[1] > b[3] {
				continue
			}
		}
		box = [4]float64{min(box[0], b[0]), min(box[1], b[1]), max(box[2], b[2]), max(box[3], b[3])}
	}
	return box
}
