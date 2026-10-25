package typography

import (
	"context"
	"math"

	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
)

// Reach of a glyph's ink across its baseline, in text space units per unit
// of font size, for a font whose glyph outlines litho does not bound. Not
// spec-mandated: an ordinary font rises to about one em and dips a fifth of
// one below its baseline, the reach heuristics.ElementZone gives an element
// that records no [models.TextElement.InkBox].
const (
	inkAscentEm  = 1.0
	inkDescentEm = 0.2
)

// maxFontBBoxEm bounds the height and width, in ems, of a /FontBBox the ink
// of a glyph is read from. Anti-garbage guard, not spec-mandated: ISO
// 32000-1, 9.8.1, Table 122 makes the box the union of every glyph's bounds,
// which for real faces, CJK and symbol fonts among them, stays within two
// ems; a box past four is a producer's placeholder, and widening every glyph
// to it would take the neighbours of whatever a redaction region covers.
const maxFontBBoxEm = 4.0

// glyphInkBox returns the axis-aligned bounds, in user space, of the ink the
// glyph cid of font paints in state, together
// with its origin (originX, originY). The glyph's bounds in text space are
// carried through the text rendering matrix of ISO 32000-1, 9.4.4,
// [Tfs×Th 0 0 Tfs 0 Trise] × Tm × CTM, corner by corner, so a skewed,
// turned or mirrored matrix gives the box of the parallelogram it draws.
// fontSize is the Tfs the glyph is shown at, and code the bytes of the
// character code that selects it (9.4.3).
//
// A machine with [graphics.Machine.GlyphOutlineBounds] set also bounds a
// glyph of any font other than Type 3 by its outline: the box grows to hold
// the outline's bounds, so a glyph whose outline reaches past its advance,
// a script face's swash or an italic overhang (9.2.4 lets an outline paint
// anywhere in glyph space, its advance only moving the text position), is
// bounded where it inks. The advance box stays the least the box holds.
//
// A glyph a stroking text rendering mode draws (ISO 32000-1, 9.3.6, Table
// 106) paints its pen past its outline: its box grows on every side by the
// pen's reach ([strokeReach]), the line parameters of the graphics state
// through the CTM. A Type 3 glyph has no outline for the mode to stroke: a
// painting mode runs its description (9.6.5), as litho's renderer does, so
// its box stays what the description paints. A description runs under the
// graphics state it is shown in (9.6.5), so a stroke it paints without
// setting its own line width, cap, join or miter limit strokes with those of
// state, and its box grows by that pen ([type3InkBounds.penBox]), as litho's
// renderer strokes it.
//
// The box grows by half the line width, not by the tip a miter join may
// reach at a sharp corner of the outline (8.4.3.5): the box is the glyph's
// advance and its font's ascent and descent, not its outline, so it knows no
// corner, and the bound that holds every corner, the miter limit times half
// the width, grows fake bold text (mode 2, a pen under a point wide) by up
// to five times its pen, over its neighbours. Redaction then takes glyphs a
// region does not meet, and the text scene, which judges a glyph by the
// pixels its box alone covers, loses them. A tip past the box is ink the
// redaction pixel check finds.
//
// A Type 3 glyph whose description paints what no box in glyph space bounds
// ([type3InkBounds]), a shading over a clip it never set among them, paints
// as far as the clipping path in force where it is shown lets it (ISO
// 32000-1, 8.5.4): its box is the bounds of that path, which the machine
// holds in user space, seeded with the page's box. A glyph of a font stating
// no /Resources is first bounded again against the resources of the stream
// machine shows it from ([Font.type3ScopedInk]).
//
// # Corpus evidence
//
//   - govdocs, pdfjs, pdfcpu, verapdf: 113 files stroke text in their first
//     30 pages, and their boxes grow. Bounded by the miter limit, govdocs
//     thread_200_200151.pdf reports its keycaps, black outlines filled white,
//     as text that paints nothing, and five redaction scenarios remove one
//     to five glyphs more than by half the width, which moves no redaction
//     scenario's outcome against the base.
//   - govdocs thread_300_300508.pdf, thread_100_100870.pdf: map labels set
//     twice, a white halo stroked under the black fill, whose boxes the
//     fill's no longer hide: ExtractUnseenText reports the halos as text in
//     white, as it reports any white text.
//   - pdfjs issue20513.pdf: a white copy of "One-Stop" stroked in colour
//     gains pixels its twin's box did not cover, and the default reading
//     takes it, three copies where PDF.js reads four.
func glyphInkBox(ctx context.Context, machine *graphics.Machine, font *Font, fontSize float64, cid int, code []byte, originX, originY float64) [4]float64 {
	state := machine.Current()
	corners, bounded := [4][2]float64{}, false
	if font.Subtype == "Type3" {
		if ink, ok, clipped := type3ShownInk(ctx, machine, font, cid); clipped {
			return clippedInkBox(state, originX, originY)
		} else if ok {
			corners, bounded = type3TextCorners(font, ink.penBox(state)), true
		}
	}
	if !bounded {
		corners = glyphTextCorners(font, cid)
		if machine.GlyphOutlineBounds != nil && font.Subtype != "Type3" {
			if ob, ok := machine.GlyphOutlineBounds(ctx, machine.Resources, state.Text.Tf, code); ok {
				corners = unionTextCorners(corners, ob)
			}
		}
	}
	th := state.Text.Tz / 100
	rise := state.Text.Ts
	tm, ctm := state.Text.Tm, state.CTM
	box := [4]float64{originX, originY, originX, originY}
	for _, c := range corners {
		tx, ty := c[0]*fontSize*th, c[1]*fontSize+rise
		ux, uy := tx*tm[0]+ty*tm[2]+tm[4], tx*tm[1]+ty*tm[3]+tm[5]
		px, py := ux*ctm[0]+uy*ctm[2]+ctm[4], ux*ctm[1]+uy*ctm[3]+ctm[5]
		// A comparison with NaN is false, so a matrix a file fills with
		// non-numbers leaves the box at the origin rather than poisoning it.
		if px < box[0] {
			box[0] = px
		}
		if py < box[1] {
			box[1] = py
		}
		if px > box[2] {
			box[2] = px
		}
		if py > box[3] {
			box[3] = py
		}
	}
	if font.Subtype != "Type3" && strokesText(state.Text.Tmode) {
		r := strokeReach(state.LineWidth, state.LineJoin, state.LineCap, state.MiterLimit, state.CTM, false)
		box = [4]float64{box[0] - r, box[1] - r, box[2] + r, box[3] + r}
	}
	return box
}

// type3ShownInk returns the ink, in glyph space, that bounds the Type 3 glyph
// cid of font shown from machine: the glyph's own ([Font.type3Ink]), or,
// when that paints past every box, its ink against the resources of the
// stream machine shows it from ([Font.type3ScopedInk]). ok is false when the
// font bounds no ink for the glyph, and clipped is true when no box in glyph
// space bounds it, so it paints as far as the clip in force lets it.
func type3ShownInk(ctx context.Context, machine *graphics.Machine, font *Font, cid int) (ink type3InkBounds, ok, clipped bool) {
	ink, ok = font.type3Ink(cid)
	if !ok || !ink.unbounded {
		return ink, ok, false
	}
	scoped, ok := font.type3ScopedInk(ctx, machine.Doc, machine.Resources, cid)
	if !ok || scoped.unbounded {
		return type3InkBounds{}, false, true
	}
	return scoped, true, false
}

// initialMiterLimit is the miter limit of the initial graphics state (ISO
// 32000-1, 8.4.1, Table 52).
const initialMiterLimit = 10.0

// strokesText reports whether the text rendering mode strokes the glyph
// outlines: modes 1, 2, 5 and 6 of ISO 32000-1, 9.3.6, Table 106. A mode
// outside the table fills alone, as poppler and PDF.js paint it, and litho's
// renderer with them.
func strokesText(mode int) bool {
	return mode == 1 || mode == 2 || mode == 5 || mode == 6
}

// strokeReach returns how far past the path it strokes, in the space m maps
// user space to, a pen of line width lw reaches with the line join join, the
// line cap lineCap and the miter limit miterLimit of the graphics state
// (ISO 32000-1, 8.4.3.2 to 8.4.3.5). The pen is lw wide in user space,
// whatever the text matrix (9.3.6: the stroking modes stroke the glyph
// outline with the graphics state's line parameters, as S strokes a path),
// so only m stretches it.
//
// Half the line width bounds a round or bevel join and a butt or round cap.
// A miter join reaches up to miterLimit times that from its vertex, the
// ratio of the miter length to the line width that 8.4.3.5 lets stand
// before the join is bevelled; miters asks for that bound, which a box of
// the path, holding none of the angles that would tell how far each join
// reaches, needs to hold every tip. A projecting square cap, at the ends of
// an open or a dashed path (8.4.3.6), reaches √2 times half the width at its
// corners (Table 54). A join or cap outside Tables 54 and 55 strokes
// as the initial miter join and butt cap (Table 52), and a miter limit that
// is not a number as the initial 10, as litho's renderer strokes them.
//
// A zero width strokes the thinnest line a device can draw (8.4.3.2), whose
// reach is a fraction of a device pixel no user-space box can state; it and
// a width or limit that is not finite, which no renderer strokes, reach
// nothing here.
func strokeReach(lw float64, join, lineCap int, miterLimit float64, m [6]float64, miters bool) float64 {
	const (
		miterJoin = 0
		bevelJoin = 2
		squareCap = 2
	)
	factor := 1.0
	if join < miterJoin || join > bevelJoin {
		join = miterJoin
	}
	if math.IsNaN(miterLimit) {
		miterLimit = initialMiterLimit
	}
	if miters && join == miterJoin && miterLimit > factor {
		factor = miterLimit
	}
	if lineCap == squareCap {
		factor = max(factor, math.Sqrt2)
	}
	r := math.Abs(lw) / 2 * factor * maxStretch(m)
	if math.IsNaN(r) || math.IsInf(r, 0) {
		return 0
	}
	return r
}

// clippedInkBox returns the bounds of the clipping path in force in state,
// grown to hold the origin (originX, originY): the box of a glyph that may
// paint anywhere the clip lets it. A clip narrowed to nothing lets nothing
// paint, and leaves the box at the origin.
func clippedInkBox(state *graphics.State, originX, originY float64) [4]float64 {
	box := [4]float64{originX, originY, originX, originY}
	c := state.ClippingPath
	if !(c.MinX <= c.MaxX && c.MinY <= c.MaxY) {
		return box
	}
	return [4]float64{min(box[0], c.MinX), min(box[1], c.MinY), max(box[2], c.MaxX), max(box[3], c.MaxY)}
}

// glyphTextCorners returns the corners of the ink of a glyph in text space
// per unit of font size, before Tfs, Th and Trise apply.
//
// A Type 3 glyph is bounded by what its description paints, or the box its d1
// states (ISO 32000-1, 9.6.5, Table 113), in glyph space, which /FontMatrix
// maps to text space: a glyph drawn in pixels through a matrix of 0.01 or 1,
// shown at Tf 1, inks many times its font size, and a matrix with a negative
// X scale draws it left of its origin. See [Font.type3Ink]: [glyphInkBox]
// bounds a glyph that paints past every box before it gets here.
//
// Any other font is bounded across its baseline by its /FontBBox (Table
// 122), grown to reach at least [inkAscentEm] and [inkDescentEm] so that no
// zone is smaller than the size-based reach it replaces, and along it by its
// advance: the box's horizontal extent is the union over every glyph, and
// would widen each one onto its neighbours. A glyph that does not advance,
// as combining marks do not, takes the box's horizontal extent instead. A
// vertical font centres its glyphs on the advance, an em wide (9.7.4.3).
//
// # Corpus evidence
//
//   - govdocs thread_200_200408.pdf: bitmap Type 3 fonts with a /FontMatrix
//     of [1 0 0 -1 0 0] shown at Tf 1 ink 14 to 154 times their font size.
//   - govdocs: 97 of 110 Type 3 fonts ink more than five times their font
//     size, and in 19 files their ink reaches over 1 unit past a zone sized
//     by the font size; pdfjs: 47 of 108 fonts, 19 files; pdfcpu: 4 of 15,
//     2 files.
//   - govdocs, pdfjs, pdfcpu, facturx, pdf20: the text of every page the
//     text goldens hold, 2490 files, does not move.
//   - govdocs, pdfjs, pdfcpu, facturx: with /FontBBox, a region over the
//     middle of one run meets more of the runs around it on 0.8%, 5.1%,
//     0.4% and 11.2% of runs, the tall boxes of faces such as
//     [-628 -376 2034 1010] reaching the next line.
func glyphTextCorners(font *Font, cid int) [4][2]float64 {
	if font.Subtype == "Type3" {
		if ink, ok := font.type3Ink(cid); ok && !ink.unbounded {
			return type3TextCorners(font, ink.box)
		}
	}
	if font.WMode == 1 {
		w1, _, _ := font.VerticalMetrics(cid)
		return rectCorners(-0.5, min(0, w1/1000), 0.5, max(0, w1/1000))
	}
	w0 := font.GlyphWidth(cid) / 1000
	x0, x1 := min(0, w0), max(0, w0)
	y0, y1 := -inkDescentEm, inkAscentEm
	if bb, ok := font.descriptorBBox(); ok {
		y0, y1 = min(y0, bb[1]/1000), max(y1, bb[3]/1000)
		if w0 == 0 {
			x0, x1 = bb[0]/1000, bb[2]/1000
		}
	}
	return rectCorners(x0, y0, x1, y1)
}

// type3TextCorners returns the corners, in text space per unit of font
// size, of the box g a Type 3 glyph inks in glyph space, carried through the
// font's /FontMatrix (ISO 32000-1, 9.6.5).
func type3TextCorners(font *Font, g [4]float64) [4][2]float64 {
	fm := font.FontMatrix
	var out [4][2]float64
	for i, p := range rectCorners(g[0], g[1], g[2], g[3]) {
		out[i] = [2]float64{fm[0]*p[0] + fm[2]*p[1] + fm[4], fm[1]*p[0] + fm[3]*p[1] + fm[5]}
	}
	return out
}

// unionTextCorners returns the corners of the smallest rectangle holding the
// rectangle corners spans, as [rectCorners] lists them, and the box b
// [minX minY maxX maxY]. A box holding a value that is not a number leaves
// corners as they were.
func unionTextCorners(corners [4][2]float64, b [4]float64) [4][2]float64 {
	for _, v := range b {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return corners
		}
	}
	lo, hi := corners[0], corners[2]
	return rectCorners(min(lo[0], b[0]), min(lo[1], b[1]), max(hi[0], b[2]), max(hi[1], b[3]))
}

// rectCorners returns the corners of the rectangle from (x0, y0) to (x1, y1).
func rectCorners(x0, y0, x1, y1 float64) [4][2]float64 {
	return [4][2]float64{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}
}

// descriptorBBox returns the /FontBBox of a font other than Type 3, in glyph
// space units, when it states a box with area that [maxFontBBoxEm] admits.
func (f *Font) descriptorBBox() ([4]float64, bool) {
	bb := f.FontBBox
	w, h := bb[2]-bb[0], bb[3]-bb[1]
	if f.Subtype == "Type3" || !(w > 0 && h > 0) || w > maxFontBBoxEm*1000 || h > maxFontBBoxEm*1000 {
		return [4]float64{}, false
	}
	return bb, true
}

// type3Ink returns the ink, in glyph space, of the Type 3 glyph code
// selects: that glyph's own when the code names a glyph whose description
// litho read, the union over every glyph of the font otherwise, and false
// when the font bounds none. The code reaches its glyph as [Font.GlyphWidth]
// reaches its width.
func (f *Font) type3Ink(code int) (type3InkBounds, bool) {
	if f.type3GlyphInk != nil && code >= 0 && code <= 255 {
		if r, ok := f.Encoding[byte(code)]; ok { //nolint:gosec // code bounds-checked above
			if ink, ok := f.type3GlyphInk[encodings.RuneToGlyph(r)]; ok {
				return ink, true
			}
		}
	}
	if f.type3FontUnbounded {
		return type3InkBounds{unbounded: true}, true
	}
	if f.type3FontInk != nil {
		return type3InkBounds{box: *f.type3FontInk}, true
	}
	return type3InkBounds{}, false
}

// nonDegenerate reports whether box has area.
func nonDegenerate(box *[4]float64) bool {
	return box != nil && box[2] > box[0] && box[3] > box[1]
}

// maxStretch returns the most the linear part of m lengthens a unit vector,
// its largest singular value, so a pen of radius r drawn through m reaches no
// farther than r × maxStretch(m) from the path.
func maxStretch(m [6]float64) float64 {
	f2 := m[0]*m[0] + m[1]*m[1] + m[2]*m[2] + m[3]*m[3]
	det := m[0]*m[3] - m[1]*m[2]
	return math.Sqrt((f2 + math.Sqrt(math.Max(f2*f2-4*det*det, 0))) / 2)
}

// concatMatrix returns a × b, the matrix cm sets when a is its operand and b
// the current transformation matrix (ISO 32000-1, 8.4.4).
func concatMatrix(a, b [6]float64) [6]float64 {
	return [6]float64{
		a[0]*b[0] + a[1]*b[2],
		a[0]*b[1] + a[1]*b[3],
		a[2]*b[0] + a[3]*b[2],
		a[2]*b[1] + a[3]*b[3],
		a[4]*b[0] + a[5]*b[2] + b[4],
		a[4]*b[1] + a[5]*b[3] + b[5],
	}
}
