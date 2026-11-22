package layout

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// This file projects 3D transforms onto the page (CSS Transforms 2). A
// box's transform functions multiply as 4x4 matrices, row by row for
// column vectors, about its transform origin; the perspective of its
// parent comes before them, about the parent's perspective origin (7, 8
// and 13). The box's plane, z = 0, then maps to the page by the
// projection (x, y) -> (X/W, Y/W). When W does not vary across the
// plane, which every transform without perspective and every translation
// along z under it gives, the projection is the 2D matrix drawn. When it
// does, the plane is tilted under a perspective: a PDF matrix maps
// parallels to parallels and cannot draw it, and the box is drawn by the
// 2D matrix that maps its four corners closest to where the projection
// puts them, by least squares.

// mat4 is a 4x4 matrix, row by row, for column vectors.
type mat4 [16]float64

// ident4 is the identity.
var ident4 = mat4{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}

// mul4 returns a·b: b applied first.
func mul4(a, b mat4) mat4 {
	var out mat4
	for r := range 4 {
		for c := range 4 {
			var s float64
			for k := range 4 {
				s += float64(a[r*4+k] * b[k*4+c])
			}
			out[r*4+c] = s
		}
	}
	return out
}

// translate4 returns the translation by (x, y, z).
func translate4(x, y, z float64) mat4 {
	m := ident4
	m[3], m[7], m[11] = x, y, z
	return m
}

// of2D returns the 4x4 matrix of the 2D matrix m.
func of2D(m typeset.Matrix) mat4 {
	return mat4{m.A, m.C, 0, m.E, m.B, m.D, 0, m.F, 0, 0, 1, 0, 0, 0, 0, 1}
}

// funcMatrix4 returns the 4x4 matrix of one transform function on the
// reference box r (CSS Transforms 2, 12).
func funcMatrix4(f css.TransformFunc, r typeset.Rect) mat4 {
	switch f.Kind { //nolint:exhaustive // the 2D functions go through funcMatrix
	case css.TransformMatrix3D:
		if f.M == nil {
			return ident4
		}
		m := mat4(*f.M)
		for i := range m {
			m[i] = clamp(m[i])
		}
		return m
	case css.TransformPerspective:
		m := ident4
		if d := clamp(f.V[0]); d > 0 {
			m[14] = -1 / d
		}
		return m
	case css.TransformTranslate:
		m := of2D(funcMatrix(f, r))
		m[11] = clamp(f.V[0])
		return m
	}
	return of2D(funcMatrix(f, r))
}

// is2D reports a transform list whose every function is 2D, which the
// 2D matrices compose exactly.
func is2D(st *css.Style) bool {
	for _, l := range st.TransformLists() {
		for _, f := range l {
			if f.Kind == css.TransformMatrix3D || f.Kind == css.TransformPerspective || f.Kind == css.TransformTranslate && f.V[0] != 0 {
				return false
			}
		}
	}
	return true
}

// perspectiveOf returns the perspective matrix the parent of b gives it,
// about the parent's perspective origin in its border box, in the
// coordinates r, b's border box, is in, and false when the parent has no
// perspective (CSS Transforms 2, 7 and 8).
func perspectiveOf(b *box, r typeset.Rect) (mat4, bool) {
	p := solidParent(b)
	if p == nil || !p.st.HasPerspective() || p.kind != kindBlock && p.kind != kindReplaced {
		return ident4, false
	}
	sp := p.st.Space()
	pr := ancestorRect(b, p, r)
	ox, oy := pr.X+float64(0.5*pr.Width), pr.Y+float64(0.5*pr.Height)
	if sp.OriginSet {
		ox, oy = pr.X+resolve(sp.Origin[0], pr.Width), pr.Y+resolve(sp.Origin[1], pr.Height)
	}
	pm := ident4
	pm[14] = -1 / sp.Perspective
	return mul4(translate4(ox, oy, 0), mul4(pm, translate4(-ox, -oy, 0))), true
}

// project returns the 2D matrix that draws the plane z = 0 of the 3D
// matrix m on the page, for the box r, and false when the box is not
// drawn: a corner at or behind the eye, or a projection of no area.
func project(m mat4, r typeset.Rect) (typeset.Matrix, bool) {
	corners := [4][2]float64{{r.X, r.Y}, {r.X + r.Width, r.Y}, {r.X, r.Y + r.Height}, {r.X + r.Width, r.Y + r.Height}}
	var out [4][2]float64
	for i, c := range corners {
		w := float64(m[12]*c[0]) + float64(m[13]*c[1]) + m[15]
		if !(w > 0) {
			return typeset.Identity, false
		}
		out[i] = [2]float64{
			(float64(m[0]*c[0]) + float64(m[1]*c[1]) + m[3]) / w,
			(float64(m[4]*c[0]) + float64(m[5]*c[1]) + m[7]) / w,
		}
	}
	if m[12] == 0 && m[13] == 0 {
		w := m[15]
		return typeset.Matrix{A: m[0] / w, B: m[4] / w, C: m[1] / w, D: m[5] / w, E: m[3] / w, F: m[7] / w}, true
	}
	return fitCorners(corners, out)
}

// fitCorners returns the 2D matrix that maps the corners from closest to
// the points to, by least squares, and false for a degenerate box.
func fitCorners(from, to [4][2]float64) (typeset.Matrix, bool) {
	// The normal equations of x' = a x + c y + e (and of y' alike) over
	// the four corners, solved by Cramer's rule.
	var sxx, sxy, syy, sx, sy float64
	var tx, ty [3]float64
	for i, p := range from {
		x, y := p[0], p[1]
		sxx += float64(x * x)
		sxy += float64(x * y)
		syy += float64(y * y)
		sx += x
		sy += y
		for k, v := range [2]float64{to[i][0], to[i][1]} {
			t := &tx
			if k == 1 {
				t = &ty
			}
			t[0] += float64(v * x)
			t[1] += float64(v * y)
			t[2] += v
		}
	}
	det3 := func(a, b, c [3]float64) float64 {
		return float64(a[0]*(float64(b[1]*c[2])-float64(b[2]*c[1]))) -
			float64(a[1]*(float64(b[0]*c[2])-float64(b[2]*c[0]))) +
			float64(a[2]*(float64(b[0]*c[1])-float64(b[1]*c[0])))
	}
	r0, r1, r2 := [3]float64{sxx, sxy, sx}, [3]float64{sxy, syy, sy}, [3]float64{sx, sy, 4}
	d := det3(r0, r1, r2)
	if math.Abs(d) < 1e-12 {
		return typeset.Identity, false
	}
	solve := func(t [3]float64) [3]float64 {
		c0, c1, c2 := [3]float64{r0[0], r1[0], r2[0]}, [3]float64{r0[1], r1[1], r2[1]}, [3]float64{r0[2], r1[2], r2[2]}
		cols := func(a, b, c [3]float64) float64 {
			return det3([3]float64{a[0], b[0], c[0]}, [3]float64{a[1], b[1], c[1]}, [3]float64{a[2], b[2], c[2]})
		}
		return [3]float64{cols(t, c1, c2) / d, cols(c0, t, c2) / d, cols(c0, c1, t) / d}
	}
	x, y := solve(tx), solve(ty)
	return typeset.Matrix{A: x[0], C: x[1], E: x[2], B: y[0], D: y[1], F: y[2]}, true
}

// backfaceShown reports whether the back of the plane of the 3D matrix m
// faces the viewer: the z component of the third column of its inverse
// is negative (CSS Transforms 2, 11), read from the cofactor of m33 and
// the determinant, which share the inverse's denominator.
func backfaceShown(m mat4) bool {
	det := det4(m)
	if det == 0 {
		return false
	}
	cof := det3m(m[0], m[1], m[3], m[4], m[5], m[7], m[12], m[13], m[15])
	return float64(cof*det) < 0
}

// det3m returns the determinant of the 3x3 matrix given row by row.
func det3m(a, b, c, d, e, f, g, h, i float64) float64 {
	return float64(a*(float64(e*i)-float64(f*h))) - float64(b*(float64(d*i)-float64(f*g))) + float64(c*(float64(d*h)-float64(e*g)))
}

// det4 returns the determinant of m, by cofactors along its first row.
func det4(m mat4) float64 {
	minor := func(col int) float64 {
		var v [9]float64
		k := 0
		for r := 1; r < 4; r++ {
			for c := range 4 {
				if c == col {
					continue
				}
				v[k] = m[r*4+c]
				k++
			}
		}
		return det3m(v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7], v[8])
	}
	return float64(m[0]*minor(0)) - float64(m[1]*minor(1)) + float64(m[2]*minor(2)) - float64(m[3]*minor(3))
}

// holds3D reports a box that its perspective or its preserve-3d makes a
// stacking context and the containing block of its absolute and fixed
// descendants (CSS Transforms 2, 7 and 9).
func holds3D(b *box) bool {
	sp := b.st.Space()
	return sp != nil && (sp.Perspective > 0 || sp.Preserve3D) && (b.kind == kindBlock || b.kind == kindReplaced) && b.tpart <= partCell
}

// maxContext3D bounds how many ancestors a 3D rendering context chains
// through. Anti-DoS guard, not spec-mandated: each level multiplies the
// matrices of every ancestor above it once more, and real documents nest
// preserve-3d a few levels deep.
const maxContext3D = 64

// needs3D reports a transformed box b whose matrix the 2D path cannot
// compose: a 3D function, a perspective from its parent, a hidden back
// face, or a parent of transform-style: preserve-3d (CSS Transforms 2, 9
// and 11).
func needs3D(b *box) bool {
	if sp := b.st.Space(); sp != nil && sp.BackfaceHidden {
		return true
	}
	if !is2D(b.st) {
		return true
	}
	p := solidParent(b)
	return p != nil && p.st.HasPerspective() || context3DParent(b) != nil
}

// solidParent returns the nearest ancestor of b that is no anonymous box.
func solidParent(b *box) *box {
	p := b.parent
	for p != nil && p.anon {
		p = p.parent
	}
	return p
}

// context3DParent returns b's parent when it extends a 3D rendering
// context to b: a transformable box whose used transform-style is
// preserve-3d (CSS Transforms 2, 9).
func context3DParent(b *box) *box {
	p := solidParent(b)
	if p == nil || !preserves3D(p) {
		return nil
	}
	return p
}

// preserves3D reports a transformable box b whose used transform-style is
// preserve-3d: its computed value, unless a grouping property makes it
// flat (CSS Transforms 2, 9): overflow other than visible and clip,
// opacity below 1, a filter, clip, clip-path, isolation, a mask, a blend
// mode or paint containment.
func preserves3D(b *box) bool {
	sp := b.st.Space()
	if sp == nil || !sp.Preserve3D || b.kind != kindBlock && b.kind != kindReplaced || b.tpart > partCell {
		return false
	}
	st := b.st
	return st.Overflow == css.OverflowVisible && st.Opacity >= 1 && !st.Clip.Set && !b.composited() && st.Contain&css.ContainPaint == 0
}

// planeHidden reports a transformed box b that [transformOf] does not
// draw because its plane projects to no area, whose 3D rendering context
// still holds its transformed descendants (CSS Transforms 2, 9): a box of
// used preserve-3d whose accumulated matrix is invertible and whose back
// face, when hidden, does not face the viewer. Its own content, which
// lies in its plane, draws nothing; its descendants draw by their own
// matrices.
func planeHidden(b *box, border typeset.Rect) bool {
	if !preserves3D(b) || !b.transformed() {
		return false
	}
	full := accum3D(b, border, 0)
	return det4(full) != 0 && (!b.st.Space().BackfaceHidden || !backfaceShown(full))
}

// transform3DOf returns what [transformOf] returns for a box b that
// [needs3D]: its accumulated 3D matrix, through the 3D rendering context
// it takes part in, projected onto the page for its border box border,
// and drawn inside the group of its nearest transformed ancestor in that
// context, whose projection it undoes. A box whose back faces the viewer
// under backface-visibility: hidden, or whose matrix is not invertible, is
// not drawn (CSS Transforms 1, 6; CSS Transforms 2, 11).
func transform3DOf(b *box, border typeset.Rect) (typeset.Matrix, bool, bool) {
	full := accum3D(b, border, 0)
	for _, v := range full {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return typeset.Identity, true, false
		}
	}
	if det4(full) == 0 || b.st.Space() != nil && b.st.Space().BackfaceHidden && backfaceShown(full) {
		return typeset.Identity, true, false
	}
	p, ok := project(full, border)
	if !ok || !finiteMatrix(p) {
		return typeset.Identity, true, false
	}
	depth := 0
	for a := context3DParent(b); a != nil && depth < maxContext3D; a, depth = context3DParent(a), depth+1 {
		if !a.transformed() {
			continue
		}
		ar := ancestorRect(b, a, border)
		d, ok := project(accum3D(a, ar, 0), ar)
		if !ok {
			return typeset.Identity, true, false
		}
		inv, ok := invertMatrix(d)
		if !ok {
			// The ancestor's plane projects to no area: it pushes no
			// group ([planeHidden]), and the next one up holds b.
			continue
		}
		p = mul(inv, p)
		break
	}
	if !finiteMatrix(p) || math.Abs(float64(p.A*p.D)-float64(p.B*p.C)) < 1e-12 {
		return typeset.Identity, true, false
	}
	return p, true, true
}

// finiteMatrix reports a 2D matrix of finite entries within the layout's
// lengths.
func finiteMatrix(m typeset.Matrix) bool {
	for _, v := range []float64{m.A, m.B, m.C, m.D, m.E, m.F} {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > maxLength {
			return false
		}
	}
	return true
}

// accum3D returns the accumulated 3D matrix of b, whose border box is
// border: that of the 3D rendering context its parent extends to it, then
// the perspective its parent gives it, then its own transform (CSS
// Transforms 2, 13). depth counts the ancestors already chained.
func accum3D(b *box, border typeset.Rect, depth int) mat4 {
	m := local3D(b, border)
	if persp, ok := perspectiveOf(b, border); ok {
		m = mul4(persp, m)
	}
	if p := context3DParent(b); p != nil && depth < maxContext3D {
		m = mul4(accum3D(p, ancestorRect(b, p, border), depth+1), m)
	}
	return m
}

// local3D returns the 3D matrix of b's own transform about its origin,
// border being its border box: the identity for a box without one.
func local3D(b *box, border typeset.Rect) mat4 {
	if !b.transformed() {
		return ident4
	}
	r := border
	if b.st.TransformBox == css.TransformContentBox {
		r.X += b.bd[css.Left] + b.pd[css.Left]
		r.Y += b.bd[css.Top] + b.pd[css.Top]
		r.Width = max(r.Width-hEdges(b), 0)
		r.Height = max(r.Height-vEdges(b), 0)
	}
	o := typeset.Point{X: r.X + resolve(b.st.TransformOrigin[0], r.Width), Y: r.Y + resolve(b.st.TransformOrigin[1], r.Height)}
	m := translate4(o.X, o.Y, 0)
	for i, l := range b.st.TransformLists() {
		if i == 3 {
			if mm, ok := motionOf(b, border, o); ok {
				m = mul4(m, of2D(mm))
			}
		}
		for _, f := range l {
			m = mul4(m, funcMatrix4(f, r))
		}
	}
	return mul4(m, translate4(-o.X, -o.Y, 0))
}

// ancestorRect returns the border box of a, an ancestor of b, in the
// coordinates b's border box r is in: b's place there, less the relative
// offsets of b and of the boxes between it and a.
func ancestorRect(b, a *box, r typeset.Rect) typeset.Rect {
	dx, dy := float64(r.X-b.x), float64(r.Y-b.y)
	for k := b; k != nil && k != a; k = k.parent {
		ox, oy := ownOffset(k)
		dx, dy = dx-ox, dy-oy
	}
	return typeset.Rect{X: a.x + dx, Y: a.y + dy, Width: a.borderW(), Height: a.borderH()}
}
