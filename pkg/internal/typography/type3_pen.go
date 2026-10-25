package typography

import (
	"context"
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

// maxType3LiftedStrokes caps the strokes with an inherited pen that the
// nested Type 3 glyphs one description shows hand up to it
// ([type3Lift]). Anti-DoS guard, not spec-mandated: each nested glyph hands
// up to [maxType3PenStrokes], once per show, so a description showing
// thousands of such glyphs would hold thousands of strokes before they
// merge. Past the cap, the description's text is not bounded at all
// ([type3TextBoxes]), and paints anywhere its clip lets it. A real glyph
// shows a few nested glyphs, so the cap is far above what it meets.
const maxType3LiftedStrokes = 16 * maxType3PenStrokes

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
//   - All corpora: 8 of the 135 files show text from a Type 3 glyph
//     description, and no nested Type 3 glyph among them strokes with an
//     inherited pen (pdfjs ContentStream*Type3insideType3 set 10 w): over
//     their first ten pages no ink box moves.
//   - litho t3nestpen_2b.pdf: a nested line stroked under the page's 2000 w.
//     The centred quarter over its pen left 760 pixels inside the region, and
//     now removes the outer glyph.
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

// resolve returns the stroke s as shown in a description whose pen is pen:
// each parameter s inherits that pen sets is pen's, or the one s holds where
// merged strokes set one reaching farther, as [type3PenStroke.reach] reads
// a showing state's, and is no longer inherited. It reports whether s still
// inherits a parameter, from the state the description is itself shown in.
func (s type3PenStroke) resolve(pen type3PenState) (type3PenStroke, bool) {
	set := s.inherit & pen.set
	if set&penWidth != 0 {
		s.lw = max(math.Abs(pen.lw), math.Abs(s.lw))
	}
	if set&penCap != 0 {
		s.lc = widerCap(pen.lc, s.lc)
	}
	if set&penJoin != 0 {
		s.lj = widerJoin(pen.lj, s.lj)
	}
	if set&penMiter != 0 {
		s.ml = max(miterOrInitial(pen.ml), miterOrInitial(s.ml))
	}
	s.inherit &^= set
	return s, s.inherit != 0
}

// through returns the stroke s of a glyph whose glyph space m maps to the
// space of the description showing it: its path box and clip box the boxes
// of their corners through m, and its pen stretched by m as well
// ([maxStretch]), since the width a stroke inherits is in the user space it
// strokes in (ISO 32000-1, 8.4.3.2). It returns false for a box m does not
// carry to finite numbers.
func (s type3PenStroke) through(m [6]float64) (type3PenStroke, bool) {
	boxThrough := func(b [4]float64) ([4]float64, bool) {
		var out *[4]float64
		for _, c := range rectCorners(b[0], b[1], b[2], b[3]) {
			x, y := m[0]*c[0]+m[2]*c[1]+m[4], m[1]*c[0]+m[3]*c[1]+m[5]
			out = unionBox(out, &[4]float64{x, y, x, y})
		}
		return *out, finiteBox(*out)
	}
	box, ok := boxThrough(s.box)
	if !ok {
		return type3PenStroke{}, false
	}
	s.box = box
	if s.clip != nil {
		clip, ok := boxThrough(*s.clip)
		if !ok {
			return type3PenStroke{}, false
		}
		s.clip = &clip
	}
	s.stretch *= maxStretch(m)
	if math.IsNaN(s.stretch) || math.IsInf(s.stretch, 0) {
		return type3PenStroke{}, false
	}
	return s, true
}

// within returns s cut to clip as well as to its own clip, nil for none: a
// stroke of a nested glyph paints under the clip its shower set too (ISO
// 32000-1, 8.5.4).
func (s type3PenStroke) within(clip *[4]float64) type3PenStroke {
	switch {
	case clip == nil:
	case s.clip == nil:
		c := *clip
		s.clip = &c
	default:
		c := [4]float64{max(s.clip[0], clip[0]), max(s.clip[1], clip[1]), min(s.clip[2], clip[2]), min(s.clip[3], clip[3])}
		s.clip = &c
	}
	return s
}

// type3LiftKey is the context key of the [type3Lift] in force.
type type3LiftKey struct{}

// type3Lift collects, while [type3TextBoxes] runs a Type 3 glyph
// description through the text extractor, the strokes with an inherited pen
// of each Type 3 glyph the description shows, in the description's glyph
// space, by the index of the element that glyph is. A nested glyph runs in
// the graphics state at its show (ISO 32000-1, 9.6.5), which is the
// description's own, itself the state the outer glyph is shown in for every
// parameter the description did not set: a nested stroke inherits that one
// from where the outer glyph is shown, and becomes a stroke of the outer
// glyph's [type3Pen]. Only the goroutine running the extraction touches it.
type type3Lift struct {
	strokes  []type3LiftedStroke
	overflow bool
}

// type3LiftedStroke is a stroke a nested Type 3 glyph hands up, and where
// the glyph is: the index of its element while [type3Lift] collects it, the
// index of the operator showing it once [type3TextBoxes] returns it.
type type3LiftedStroke struct {
	at     int
	stroke type3PenStroke
}

// type3LiftFrom returns the collector ctx carries, nil when none.
func type3LiftFrom(ctx context.Context) *type3Lift {
	l, _ := ctx.Value(type3LiftKey{}).(*type3Lift)
	return l
}

// record hands up the strokes with an inherited pen of the Type 3 glyph cid
// of font, shown at fontSize from machine as element: each through the
// matrix that maps its glyph space to the space machine shows it in, the
// font matrix and the text rendering matrix of ISO 32000-1, 9.4.4 and
// 9.2.4. A glyph bounded by the clip in force has no pen to hand up: its box
// is the clip's.
func (l *type3Lift) record(ctx context.Context, machine *graphics.Machine, font *Font, fontSize float64, cid, element int) {
	if l.overflow {
		return
	}
	ink, ok, clipped := type3ShownInk(ctx, machine, font, cid)
	if !ok || clipped || ink.pen == nil {
		return
	}
	state := machine.Current()
	th := state.Text.Tz / 100
	m := concatMatrix(font.FontMatrix, [6]float64{fontSize * th, 0, 0, fontSize, 0, state.Text.Ts})
	m = concatMatrix(concatMatrix(m, state.Text.Tm), state.CTM)
	for _, s := range ink.pen.strokes {
		t, ok := s.through(m)
		if !ok {
			continue
		}
		if len(l.strokes) >= maxType3LiftedStrokes {
			l.overflow = true
			return
		}
		l.strokes = append(l.strokes, type3LiftedStroke{at: element, stroke: t})
	}
}
