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
// fontSize is the Tfs the glyph is shown at.
//
// A Type 3 glyph whose description paints what no box in glyph space bounds
// ([type3InkBounds]), a shading over a clip it never set among them, paints
// as far as the clipping path in force where it is shown lets it (ISO
// 32000-1, 8.5.4): its box is the bounds of that path, which the machine
// holds in user space, seeded with the page's box. A glyph of a font stating
// no /Resources is first bounded again against the resources of the stream
// machine shows it from ([Font.type3ScopedInk]).
func glyphInkBox(ctx context.Context, machine *graphics.Machine, font *Font, fontSize float64, cid int, originX, originY float64) [4]float64 {
	state := machine.Current()
	corners, bounded := [4][2]float64{}, false
	if font.Subtype == "Type3" {
		if ink, ok := font.type3Ink(cid); ok && ink.unbounded {
			scoped, ok := font.type3ScopedInk(ctx, machine.Doc, machine.Resources, cid)
			if !ok || scoped.unbounded {
				return clippedInkBox(state, originX, originY)
			}
			corners, bounded = type3TextCorners(font, scoped.box), true
		}
	}
	if !bounded {
		corners = glyphTextCorners(font, cid)
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
	return box
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
