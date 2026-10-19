package typography

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
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
func glyphInkBox(state *graphics.State, font *Font, fontSize float64, cid int, originX, originY float64) [4]float64 {
	corners := glyphTextCorners(font, cid)
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

// glyphTextCorners returns the corners of the ink of a glyph in text space
// per unit of font size, before Tfs, Th and Trise apply.
//
// A Type 3 glyph is bounded by what its description paints, or the box its d1
// states (ISO 32000-1, 9.6.5, Table 113), in glyph space, which /FontMatrix
// maps to text space: a glyph drawn in pixels through a matrix of 0.01 or 1,
// shown at Tf 1, inks many times its font size, and a matrix with a negative
// X scale draws it left of its origin. See [Font.type3Ink].
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
		if g, ok := font.type3Ink(cid); ok {
			fm := font.FontMatrix
			var out [4][2]float64
			for i, p := range rectCorners(g[0], g[1], g[2], g[3]) {
				out[i] = [2]float64{fm[0]*p[0] + fm[2]*p[1] + fm[4], fm[1]*p[0] + fm[3]*p[1] + fm[5]}
			}
			return out
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

// type3Ink returns the bounds, in glyph space, of the ink the Type 3 glyph
// code selects: that glyph's own when the code names a glyph whose ink is
// known, the union over every glyph of the font otherwise, and false when
// the font bounds none. The code reaches its glyph as [Font.GlyphWidth]
// reaches its width.
func (f *Font) type3Ink(code int) ([4]float64, bool) {
	if f.type3GlyphInk != nil && code >= 0 && code <= 255 {
		if r, ok := f.Encoding[byte(code)]; ok { //nolint:gosec // code bounds-checked above
			if box, ok := f.type3GlyphInk[encodings.RuneToGlyph(r)]; ok {
				return box, true
			}
		}
	}
	if f.type3FontInk != nil {
		return *f.type3FontInk, true
	}
	return [4]float64{}, false
}

// nonDegenerate reports whether box has area.
func nonDegenerate(box *[4]float64) bool {
	return box != nil && box[2] > box[0] && box[3] > box[1]
}

// type3PaintBounds returns the bounds, in glyph space, of the marks the
// operators ops of a Type 3 glyph description paint, nil when they paint
// none, and whether the walk accounts for every operator that can paint.
//
// It follows the current transformation matrix through q, Q and cm (ISO
// 32000-1, 8.4.4), so a bitmap glyph drawn as an image (8.9.5: the unit
// square through the CTM) and a path drawn in pixels under a scaling both
// land where they paint. A stroke widens its path by half the line width (w,
// 8.4.3.2). A path that is only clipped or ended by n paints nothing. A Do,
// a shading or text shown inside the glyph paints marks it does not bound,
// so the walk reports itself incomplete and the caller widens to the boxes
// the font states.
func type3PaintBounds(ops []content.Operation) (*[4]float64, bool) {
	type gstate struct {
		m  [6]float64
		lw float64
	}
	cur := gstate{m: [6]float64{1, 0, 0, 1, 0, 0}, lw: 1}
	// The stack is bounded by the operator count of an already parsed
	// stream, so an unbalanced q costs no more than the stream itself.
	var stack []gstate
	var painted, path *[4]float64
	complete := true
	transform := func(x, y float64) [2]float64 {
		m := cur.m
		return [2]float64{m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]}
	}
	addPath := func(x, y float64) { path = unionBox(path, &[4]float64{x, y, x, y}) }
	paint := func(stroke bool) {
		if path == nil {
			return
		}
		box := *path
		if stroke {
			pad := math.Abs(cur.lw) / 2 * maxStretch(cur.m)
			box = [4]float64{box[0] - pad, box[1] - pad, box[2] + pad, box[3] + pad}
		}
		painted = unionBox(painted, &box)
		path = nil
	}
	unitSquare := func() {
		for _, p := range rectCorners(0, 0, 1, 1) {
			q := transform(p[0], p[1])
			painted = unionBox(painted, &[4]float64{q[0], q[1], q[0], q[1]})
		}
	}
	// A Bezier segment through an affine map is the segment through the
	// mapped control points, so its bounds are taken after the map.
	curve := func(p [4][2]float64) {
		for i := range p {
			p[i] = transform(p[i][0], p[i][1])
		}
		bezierBounds(p, addPath)
	}
	var cx, cy float64
	for _, op := range ops {
		switch op.Operator {
		case "q":
			stack = append(stack, cur)
		case "Q":
			if n := len(stack); n > 0 {
				cur, stack = stack[n-1], stack[:n-1]
			}
		case "cm":
			if len(op.Operands) >= 6 {
				var a [6]float64
				for k := range 6 {
					a[k] = document.Float(op.Operands[k])
				}
				cur.m = concatMatrix(a, cur.m)
			}
		case "w":
			if len(op.Operands) >= 1 {
				cur.lw = document.Float(op.Operands[0])
			}
		case "f", "F", "f*":
			paint(false)
		case "S", "s", "B", "B*", "b", "b*":
			paint(true)
		case "n":
			path = nil
		case "BI":
			unitSquare()
		case "Do":
			unitSquare()
			complete = false
		case "sh", "Tj", "TJ", "'", "\"":
			complete = false
		default:
			v, ok := pathOperands(op)
			if !ok {
				continue
			}
			switch op.Operator {
			case "m", "l":
				cx, cy = v[0], v[1]
				q := transform(cx, cy)
				addPath(q[0], q[1])
			case "re":
				cx, cy = v[0], v[1]
				for _, p := range rectCorners(v[0], v[1], v[0]+v[2], v[1]+v[3]) {
					q := transform(p[0], p[1])
					addPath(q[0], q[1])
				}
			case "c":
				curve([4][2]float64{{cx, cy}, {v[0], v[1]}, {v[2], v[3]}, {v[4], v[5]}})
				cx, cy = v[4], v[5]
			case "v":
				curve([4][2]float64{{cx, cy}, {cx, cy}, {v[0], v[1]}, {v[2], v[3]}})
				cx, cy = v[2], v[3]
			case "y":
				curve([4][2]float64{{cx, cy}, {v[0], v[1]}, {v[2], v[3]}, {v[2], v[3]}})
				cx, cy = v[2], v[3]
			}
		}
	}
	return painted, complete
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
