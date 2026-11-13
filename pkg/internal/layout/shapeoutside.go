package layout

import (
	"bytes"
	"compress/zlib"
	"io"
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/svg"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// floatShape is the float area of a float with shape-outside (CSS Shapes
// 1, 6.1), its shape-margin applied, in coordinates from the top left of
// the float's margin box. The lines beside the float wrap around it; the
// float itself is placed, and clears, by its margin box (CSS Shapes 1, 2).
type floatShape struct {
	// rect and radii are a rounded rectangle: a box value, inset(),
	// circle() or ellipse(). They hold no shape when polys and rows are
	// empty and rect is empty.
	rect  typeset.Rect
	radii [4][2]float64
	// polys are the outlines of a polygon() or a path(), each a closed
	// ring of points.
	polys [][]wrapPt
	// rows are the spans of an image's pixels over shape-image-threshold,
	// one a pixel row from rowTop, rowH tall (CSS Shapes 1, 4).
	rows   []shapeRow
	rowTop float64
	rowH   float64
	// margin is the shape-margin polys and rows grow by (CSS Shapes 1,
	// 6.3); the rounded rectangle has it applied.
	margin float64
	// frags, when set, are the float areas of the parts of a float a page
	// or column break cuts, in its order; the fields above are then
	// unused.
	frags []shapeFrag
}

// shapeFrag is the float area of the part of a float between top and
// bottom, from the top of its margin box; s is from the top of the part.
type shapeFrag struct {
	top, bottom float64
	s           *floatShape
}

// maxShapeFrags caps the parts a float area is cut into at page or column
// breaks. Anti-DoS guard, not spec-mandated: a float spans a few pages;
// past the cap the last part's area stands for the rest.
const maxShapeFrags = 256

// shapeRow is the span from x0 to x1 of the pixels of one row of a shape
// image over the threshold; x0 > x1 for a row with none.
type shapeRow struct{ x0, x1 float64 }

// maxShapeSegments caps the straight segments a path() float area is
// flattened to. Anti-DoS guard, not spec-mandated: a path's data is
// capped at 1 MiB, and each curve takes [shapeCurveSteps] segments; a
// drawn outline takes a few hundred.
const maxShapeSegments = 1 << 16

// shapeCurveSteps is the number of straight segments a cubic Bézier of a
// path() float area is flattened to: at the sizes of a float, a step is a
// small fraction of a pixel off the curve.
const shapeCurveSteps = 16

// shapeStep is how far a line that does not fit beside a float area moves
// down before it tries again, one CSS pixel. CSS Shapes 1 leaves the next
// position to the line layout; Chromium moves down a pixel at a time
// while a shape narrows the line.
//
// # Corpus evidence
//
//   - probe, html: a 250px-wide inline block beside circle(70px at 0 0)
//     lands at 49px, not at 48.99px where the circle first leaves room;
//     beside circle(70.3px at 0 0) at 50px, not 49.42px; beside a
//     triangle that frees room at 33.3px, at 34px.
const shapeStep = css.Pt

// maxShapeTries caps the places a line tries beside floats with
// shape-outside, a [shapeStep] apart. Anti-DoS guard, not spec-mandated:
// it covers floats some ten thousand pixels tall; past it a line goes
// below the floats it does not fit beside.
const maxShapeTries = 10000

// maxShapeCells caps the pixels an image float area is read at: rows
// times columns. Anti-DoS guard, not spec-mandated: a float a thousand
// pixels square takes a million; past the cap the area is read at a
// coarser grid.
const maxShapeCells = 1 << 22

// maxShapeImagePixels caps the pixels of a picture whose alpha channel
// makes a float area. Anti-DoS guard, not spec-mandated: the alpha is
// inflated whole, one byte a pixel; a picture of 16 million pixels takes
// 16 MiB.
const maxShapeImagePixels = 1 << 24

// imageShape returns the float area of the float f whose shape-outside is
// an image, grown by the margin m: the pixels whose alpha is over
// shape-image-threshold, the image filling the content box as a replaced
// element's would (CSS Shapes 1, 4). The area is read a CSS pixel row at
// a time, as Chromium reads it. nil when the image does not load.
func (e *engine) imageShape(f *box, so *css.ShapeOutside, m float64) *floatShape {
	x0 := f.m[css.Left] + f.bd[css.Left] + f.pd[css.Left]
	y0 := f.m[css.Top] + f.bd[css.Top] + f.pd[css.Top]
	w, h := f.w, f.h
	if !(w > 0) || !(h > 0) {
		return nil
	}
	rows := max(int(min(math.Ceil(h/css.Pt), maxShapeCells)), 1)               // fits int: at most maxShapeCells
	cols := max(int(min(math.Ceil(w/css.Pt), maxShapeCells/float64(rows))), 1) // fits int: at most maxShapeCells
	var alpha func(x, y float64) float64
	if g := so.Image.Gradient; g != nil {
		fill, ok := e.gradientFill(g, w, h, f.y)
		if !ok {
			alpha = func(float64, float64) float64 { return 0 }
		} else {
			alpha = func(x, y float64) float64 { return fillAlpha(fill, x, y) }
		}
	} else {
		pic := e.picture(so.Image.URL, f.y)
		if pic == nil {
			return nil
		}
		alpha = pictureAlpha(pic, w, h)
	}
	s := &floatShape{rowTop: y0, rowH: h / float64(rows), margin: m, rows: make([]shapeRow, rows)}
	cw := w / float64(cols)
	for r := range rows {
		yc := (float64(r) + 0.5) * s.rowH
		row := shapeRow{x0: 1, x1: 0}
		for c := range cols {
			if alpha((float64(c)+0.5)*cw, yc) > so.Threshold {
				if row.x0 > row.x1 {
					row.x0 = x0 + float64(c)*cw
				}
				row.x1 = x0 + float64(c+1)*cw
			}
		}
		s.rows[r] = row
	}
	return s
}

// inflateFull decompresses the zlib stream z into dst, which it must
// fill.
func inflateFull(z []byte, dst []byte) error {
	r, err := zlib.NewReader(bytes.NewReader(z))
	if err != nil {
		return err
	}
	defer r.Close() // Close reports nothing a read did not
	_, err = io.ReadFull(r, dst)
	return err
}

// fillAlpha returns the opacity of the fill at x, y: its colour's, or
// its shading's there, the shading extended past its ends.
func fillAlpha(fill *typeset.Fill, x, y float64) float64 {
	sh := fill.Shading
	if sh == nil {
		return fill.Color.A
	}
	tm := sh.Transform
	if tm == (typeset.Matrix{}) {
		tm = typeset.Identity
	}
	det := tm.A*tm.D - tm.B*tm.C
	if det == 0 {
		return 0
	}
	// The point in the shading's coordinates: tm inverted.
	ux, uy := x-tm.E, y-tm.F
	p := typeset.Point{X: (tm.D*ux - tm.C*uy) / det, Y: (tm.A*uy - tm.B*ux) / det}
	var t float64
	switch sh.Kind {
	case typeset.Axial:
		dx, dy := sh.To.X-sh.From.X, sh.To.Y-sh.From.Y
		if l := dx*dx + dy*dy; l > 0 {
			t = ((p.X-sh.From.X)*dx + (p.Y-sh.From.Y)*dy) / l
		}
	case typeset.Radial:
		// The circles are concentric: the radius at p over the span.
		if d := sh.ToRadius - sh.FromRadius; d != 0 {
			t = (math.Hypot(p.X-sh.From.X, p.Y-sh.From.Y) - sh.FromRadius) / d
		}
	case typeset.Conic:
		// The turn from the direction of To, clockwise.
		a0 := math.Atan2(sh.To.X-sh.From.X, -(sh.To.Y - sh.From.Y))
		a := math.Atan2(p.X-sh.From.X, -(p.Y - sh.From.Y))
		t = math.Mod(a-a0+4*math.Pi, 2*math.Pi) / (2 * math.Pi)
	}
	stops := sh.Stops
	if len(stops) == 0 {
		return 0
	}
	if t <= stops[0].Offset {
		return stops[0].Color.A
	}
	for i := 1; i < len(stops); i++ {
		a, b := stops[i-1], stops[i]
		if t <= b.Offset {
			if b.Offset <= a.Offset {
				return b.Color.A
			}
			return a.Color.A + (b.Color.A-a.Color.A)*(t-a.Offset)/(b.Offset-a.Offset)
		}
	}
	return stops[len(stops)-1].Color.A
}

// pictureAlpha returns the opacity of the picture pic stretched over a w
// by h box, at a point of the box: its alpha channel's, a pixel at a
// time; 1 everywhere for a picture without one and for a vector picture,
// whose coverage the engine does not read.
func pictureAlpha(pic *Picture, w, h float64) func(x, y float64) float64 {
	opaque := func(float64, float64) float64 { return 1 }
	src := pic.Source
	if src == nil || src.Alpha == nil || src.Width <= 0 || src.Height <= 0 || src.Width > maxShapeImagePixels/src.Height {
		return opaque
	}
	a := make([]byte, src.Width*src.Height) // fits int: at most maxShapeImagePixels
	if inflateFull(src.Alpha, a) != nil {
		// An alpha stream the picture's own decoder wrote does not fail;
		// one that does leaves the picture opaque, as it then paints.
		return opaque
	}
	return func(x, y float64) float64 {
		c := min(max(int(x/w*float64(src.Width)), 0), src.Width-1)   // fits int: clamped to the width
		r := min(max(int(y/h*float64(src.Height)), 0), src.Height-1) // fits int: clamped to the height
		return float64(a[r*src.Width+c]) / 255
	}
}

// newFloatShape returns the float area of the float f of shape-outside so,
// in a containing block cbw wide; nil when it has no area.
func (e *engine) newFloatShape(f *box, so *css.ShapeOutside, cbw float64) *floatShape {
	m := max(resolve(so.Margin, cbw), 0)
	if so.HasImage() {
		return e.imageShape(f, so, m)
	}
	ref, radii := shapeReferenceBox(f, so.Box)
	s := &floatShape{margin: m}
	switch {
	case so.Shape == nil:
		s.rect, s.radii = ref, radii
	case so.Shape.Kind == css.ShapePolygon:
		ring := make([]wrapPt, 0, len(so.Shape.Points))
		for _, pt := range so.Shape.Points {
			ring = append(ring, wrapPt{x: ref.X + resolve(pt[0], ref.Width), y: ref.Y + resolve(pt[1], ref.Height)})
		}
		s.polys = [][]wrapPt{ring}
	case so.Shape.Kind == css.ShapePath || so.Shape.Kind == css.ShapeShape:
		s.polys = pathRings(shapePathData(so.Shape, ref.Width, ref.Height), ref)
	default:
		s.rect, s.radii = basicShapeRect(so.Shape, ref)
	}
	if len(s.polys) == 0 && len(s.rows) == 0 && m > 0 {
		// The margin grows every corner, a square one included, by the
		// margin: the contour at a distance (CSS Shapes 1, 6.3).
		s.rect = typeset.Rect{X: s.rect.X - m, Y: s.rect.Y - m, Width: s.rect.Width + 2*m, Height: s.rect.Height + 2*m}
		for c := range s.radii {
			s.radii[c] = [2]float64{s.radii[c][0] + m, s.radii[c][1] + m}
		}
		s.radii = fitRadii(s.rect, s.radii)
	}
	return s
}

// fragmentedShape returns the float area of the float f, its margin box
// from top to bottom in flow coordinates: one area a part where a page or
// column break cuts it, each part's reference box its own fragment's,
// the box's top edges on the first and its bottom edges on the last (CSS
// Fragmentation 3, 5.4, box-decoration-break: slice). CSS Shapes 1 says
// nothing of fragments; Chromium sizes the shape to each fragment. nil
// when no break cuts f.
//
// # Corpus evidence
//
//   - probe, html: a 1000px float of polygon(0 0, 100% 100%, 0 100%) on
//     600px pages: Chromium's lines on the first page start at
//     200 (y + 20) / 600, those on the second at 200 (y - 600 + 20) /
//     400, as if each page held a float of its own height.
func (e *engine) fragmentedShape(f *box, so *css.ShapeOutside, cbw, top, bottom float64) *floatShape {
	if !e.paginating() {
		return nil
	}
	var cuts []float64
	for p := e.pageOf(top + eps); len(cuts) < maxShapeFrags; p++ {
		end := e.pageTop(p) + e.areaH(p)
		if end >= bottom-eps || end <= top+eps {
			break
		}
		cuts = append(cuts, end)
	}
	if len(cuts) == 0 {
		return nil
	}
	out := &floatShape{}
	from := top
	for i := 0; i <= len(cuts); i++ {
		to := bottom
		if i < len(cuts) {
			to = cuts[i]
		}
		part := *f
		if i > 0 {
			part.m[css.Top], part.bd[css.Top], part.pd[css.Top] = 0, 0, 0
		}
		if i < len(cuts) {
			part.m[css.Bottom], part.bd[css.Bottom], part.pd[css.Bottom] = 0, 0, 0
		}
		part.h = max(to-from-part.m[css.Top]-part.bd[css.Top]-part.pd[css.Top]-part.m[css.Bottom]-part.bd[css.Bottom]-part.pd[css.Bottom], 0)
		out.frags = append(out.frags, shapeFrag{top: from - top, bottom: to - top, s: e.newFloatShape(&part, so, cbw)})
		from = to
	}
	return out
}

// shapeReferenceBox returns f's box g, from the top left of its margin
// box, and the radii of its corners (CSS Shapes 1, 5): the border box's
// radii, those of the inner edges for the padding and content boxes, and
// for the margin box each grown by its side's margin, by the rule of CSS
// Shapes 1, 5 that keeps a small radius beside a wide margin nearly sharp.
func shapeReferenceBox(f *box, g css.GeometryBox) (typeset.Rect, [4][2]float64) {
	ml, mt := f.m[css.Left], f.m[css.Top]
	r := typeset.Rect{X: ml, Y: mt, Width: f.borderW(), Height: f.borderH()}
	radii := radiiOf(f, r.Width, r.Height)
	switch g {
	case css.GeoBorder:
	case css.GeoPadding:
		r, radii = insetRect(r, radii, f.bd)
	case css.GeoContent:
		r, radii = insetRect(r, radii, addSides(f.bd, f.pd))
	case css.GeoMargin:
		r = typeset.Rect{Width: f.marginW(), Height: f.marginH()}
		radii = marginRadii(r, radii, f.m)
	}
	return r, radii
}

// marginRadii returns the corner radii of the margin box r of a box whose
// border radii are radii and margins m: each grown by the margin of its
// side ([marginRadius]), then fitted to r.
func marginRadii(r typeset.Rect, radii [4][2]float64, m [4]float64) [4][2]float64 {
	side := [4][2]css.Side{
		css.TopLeft: {css.Left, css.Top}, css.TopRight: {css.Right, css.Top},
		css.BottomRight: {css.Right, css.Bottom}, css.BottomLeft: {css.Left, css.Bottom},
	}
	for c := range radii {
		if radii[c] == [2]float64{} {
			continue
		}
		for a := range 2 {
			radii[c][a] = marginRadius(radii[c][a], m[side[c][a]])
		}
		if radii[c][0] <= 0 || radii[c][1] <= 0 {
			radii[c] = [2]float64{}
		}
	}
	return fitRadii(r, radii)
}

// marginRadius returns the radius of a margin box corner whose border
// radius is r beside the margin m (CSS Shapes 1, 5, margin-box).
func marginRadius(r, m float64) float64 {
	if m <= 0 || r >= m {
		return max(r+m, 0)
	}
	q := r/m - 1
	return r + m*(1+q*q*q)
}

// basicShapeRect returns the rounded rectangle of an inset(), a circle()
// or an ellipse() in the reference box ref. Insets that sum past the box
// meet where their proportions put them (CSS Shapes 1, 3.1, inset(), by
// CSS Backgrounds 3, 4.5).
func basicShapeRect(s *css.Shape, ref typeset.Rect) (typeset.Rect, [4][2]float64) {
	w, h := ref.Width, ref.Height
	if s.Kind == css.ShapeInset {
		t, r, b, l := resolve(s.Inset[css.Top], h), resolve(s.Inset[css.Right], w), resolve(s.Inset[css.Bottom], h), resolve(s.Inset[css.Left], w)
		if l+r > w && l+r > 0 {
			f := w / (l + r)
			l, r = l*f, r*f
		}
		if t+b > h && t+b > 0 {
			f := h / (t + b)
			t, b = t*f, b*f
		}
		in := typeset.Rect{X: ref.X + l, Y: ref.Y + t, Width: max(w-l-r, 0), Height: max(h-t-b, 0)}
		var radii [4][2]float64
		for c, rd := range s.Radii {
			// The radii's percentages are of the reference box (CSS
			// Shapes 1, 3.1, inset()).
			radii[c] = [2]float64{max(resolve(rd.H, w), 0), max(resolve(rd.V, h), 0)}
			if radii[c][0] == 0 || radii[c][1] == 0 {
				radii[c] = [2]float64{}
			}
		}
		return in, fitRadii(in, radii)
	}
	cx, cy, rx, ry := ellipseOf(s, w, h)
	e := typeset.Rect{X: ref.X + cx - rx, Y: ref.Y + cy - ry, Width: 2 * rx, Height: 2 * ry}
	return e, [4][2]float64{{rx, ry}, {rx, ry}, {rx, ry}, {rx, ry}}
}

// ellipseOf returns the centre and the radii of a circle() or an
// ellipse() in a reference box w by h (CSS Shapes 1, 3.1).
func ellipseOf(s *css.Shape, w, h float64) (float64, float64, float64, float64) {
	cx, cy := resolve(s.Center[0], w), resolve(s.Center[1], h)
	dx0, dx1, dy0, dy1 := math.Abs(cx), math.Abs(w-cx), math.Abs(cy), math.Abs(h-cy)
	side := func(sr css.ShapeRadius, near, far, ref float64) float64 {
		switch sr.Side {
		case css.RadiusFarthest:
			return far
		case css.RadiusLength:
			return max(resolve(sr.Length, ref), 0)
		case css.RadiusClosestCorner:
			return math.Hypot(min(dx0, dx1), min(dy0, dy1))
		case css.RadiusFarthestCorner:
			return math.Hypot(max(dx0, dx1), max(dy0, dy1))
		case css.RadiusClosest:
		}
		return near
	}
	if s.Kind == css.ShapeCircle {
		// A circle's percentage is of the reference box's diagonal over
		// the square root of 2.
		r := side(s.Radius[0], min(dx0, dx1, dy0, dy1), max(dx0, dx1, dy0, dy1), math.Hypot(w, h)/math.Sqrt2)
		return cx, cy, r, r
	}
	return cx, cy, side(s.Radius[0], min(dx0, dx1), max(dx0, dx1), w), side(s.Radius[1], min(dy0, dy1), max(dy0, dy1), h)
}

// pathRings returns the subpaths of the path data d, in CSS pixels from
// the top left of ref, as rings of points, curves flattened.
func pathRings(d string, ref typeset.Rect) [][]wrapPt {
	// A path cut short by an error keeps what it drew, as SVG 1.1, F.2
	// asks.
	ops, _ := svg.PathData(d)
	var rings [][]wrapPt
	var cur []wrapPt
	n := 0
	at := func(p typeset.Point) wrapPt { return wrapPt{x: ref.X + p.X*css.Pt, y: ref.Y + p.Y*css.Pt} }
	flush := func() {
		if len(cur) > 1 {
			rings = append(rings, cur)
		}
		cur = nil
	}
	for _, op := range ops {
		if n >= maxShapeSegments {
			break
		}
		switch op.Kind {
		case typeset.MoveTo:
			flush()
			cur = []wrapPt{at(op.P[0])}
		case typeset.LineTo:
			cur = append(cur, at(op.P[0]))
			n++
		case typeset.CurveTo:
			if len(cur) == 0 {
				continue
			}
			p0 := cur[len(cur)-1]
			p1, p2, p3 := at(op.P[0]), at(op.P[1]), at(op.P[2])
			for i := 1; i <= shapeCurveSteps; i++ {
				t := float64(i) / shapeCurveSteps
				u := 1 - t
				cur = append(cur, wrapPt{
					x: u*u*u*p0.x + 3*u*u*t*p1.x + 3*u*t*t*p2.x + t*t*t*p3.x,
					y: u*u*u*p0.y + 3*u*u*t*p1.y + 3*u*t*t*p2.y + t*t*t*p3.y,
				})
			}
			n += shapeCurveSteps
		case typeset.ClosePath:
			if len(cur) > 0 {
				start := cur[0]
				flush()
				cur = []wrapPt{start}
			}
		}
	}
	flush()
	return rings
}

// moveDown moves the float area d down.
func (s *floatShape) moveDown(d float64) {
	for i := range s.frags {
		s.frags[i].top += d
		s.frags[i].bottom += d
	}
	s.rect.Y += d
	s.rowTop += d
	for _, ring := range s.polys {
		for i := range ring {
			ring[i].y += d
		}
	}
}

// extent returns the left and right edges of the float area over the
// band [y0, y1), both from the margin box's left; false when the area
// leaves the band free. A zero-height band is the line at y0.
func (s *floatShape) extent(y0, y1 float64) (float64, float64, bool) {
	if len(s.frags) > 0 {
		fr := s.frags[len(s.frags)-1]
		for _, c := range s.frags {
			if y0 < c.bottom-eps {
				fr = c
				break
			}
		}
		if fr.s == nil {
			return 0, 0, false
		}
		return fr.s.extent(y0-fr.top, y1-fr.top)
	}
	switch {
	case len(s.polys) > 0:
		return s.polyExtent(y0, y1)
	case s.rowH > 0:
		return s.rowExtent(y0, y1)
	}
	return roundedExtent(s.rect, s.radii, y0, y1)
}

// roundedExtent returns the left and right edges of the rounded rectangle
// r over the band [y0, y1); false when it lies outside the band or
// encloses no area. Chromium's float area of an empty rectangle, a line
// among them, leaves every line alone, where CSS Shapes 1, 2 keeps the
// edges of a shape without area.
func roundedExtent(r typeset.Rect, radii [4][2]float64, y0, y1 float64) (float64, float64, bool) {
	if r.Width <= 0 || r.Height <= 0 || y0 >= r.Y+r.Height || y1 < r.Y || (y1 <= r.Y && y1 > y0) {
		return 0, 0, false
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, y := range [2]float64{max(y0, r.Y), min(y1, r.Y+r.Height)} {
		l, h := roundedAt(r, radii, y)
		lo, hi = min(lo, l), max(hi, h)
	}
	// A band that reaches the straight part of a side takes the side.
	if y0 <= r.Y+radii[css.TopLeft][1] && y1 >= r.Y+r.Height-radii[css.BottomLeft][1] {
		lo = r.X
	}
	if y0 <= r.Y+radii[css.TopRight][1] && y1 >= r.Y+r.Height-radii[css.BottomRight][1] {
		hi = r.X + r.Width
	}
	return lo, hi, true
}

// roundedAt returns the left and right edges of the rounded rectangle r
// at the height y, within it.
func roundedAt(r typeset.Rect, radii [4][2]float64, y float64) (float64, float64) {
	x0, x1 := r.X, r.X+r.Width
	in := func(rx, ry, dy float64) float64 {
		// How far inside the corner's ellipse reaches at dy from its
		// centre, along x.
		if rx <= 0 || ry <= 0 {
			return 0
		}
		t := min(math.Abs(dy)/ry, 1)
		return rx - rx*math.Sqrt(1-t*t)
	}
	top, bottom := r.Y, r.Y+r.Height
	if tl := radii[css.TopLeft]; y < top+tl[1] {
		x0 += in(tl[0], tl[1], top+tl[1]-y)
	} else if bl := radii[css.BottomLeft]; y > bottom-bl[1] {
		x0 += in(bl[0], bl[1], y-(bottom-bl[1]))
	}
	if tr := radii[css.TopRight]; y < top+tr[1] {
		x1 -= in(tr[0], tr[1], top+tr[1]-y)
	} else if br := radii[css.BottomRight]; y > bottom-br[1] {
		x1 -= in(br[0], br[1], y-(bottom-br[1]))
	}
	return x0, x1
}

// polyExtent returns the left and right edges, over the band [y0, y1), of
// the polygons' edges grown by the margin; false when none lies there.
// The edges bound the area: a band inside a polygon crosses an edge on
// either side.
func (s *floatShape) polyExtent(y0, y1 float64) (float64, float64, bool) {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, ring := range s.polys {
		n := len(ring)
		for i, a := range ring {
			b := ring[(i+1)%n]
			if l, h, ok := segmentExtent(a, b, y0, y1, s.margin); ok {
				lo, hi = min(lo, l), max(hi, h)
			}
		}
	}
	if lo > hi {
		return 0, 0, false
	}
	return lo, hi, true
}

// segmentExtent returns the left and right edges, over the band [y0, y1],
// of the points within m of the segment a-b; false when none lies there.
func segmentExtent(a, b wrapPt, y0, y1, m float64) (float64, float64, bool) {
	if max(a.y, b.y) < y0-m || min(a.y, b.y) > y1+m {
		return 0, 0, false
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	add := func(l, h float64) { lo, hi = min(lo, l), max(hi, h) }
	// The part of the segment inside the band, grown by m sideways.
	// Without a margin, a band of some height takes an edge that crosses
	// its inside, not one that touches its top or bottom alone, as
	// Chromium does: a horizontal edge or a vertex on the band's edge
	// leaves it free.
	open := m <= 0 && y1 > y0
	dy := b.y - a.y
	if math.Abs(dy) <= 1e-9 {
		if a.y >= y0 && a.y <= y1 && (!open || a.y > y0+eps && a.y < y1-eps) {
			add(min(a.x, b.x)-m, max(a.x, b.x)+m)
		}
	} else {
		t0, t1 := (y0-a.y)/dy, (y1-a.y)/dy
		ta, tb := max(0, min(t0, t1)), min(1, max(t0, t1))
		if ta <= tb && (!open || (tb-ta)*math.Abs(dy) > eps) {
			xa, xb := a.x+ta*(b.x-a.x), a.x+tb*(b.x-a.x)
			add(min(xa, xb)-m, max(xa, xb)+m)
		}
	}
	if m <= 0 {
		if lo > hi {
			return 0, 0, false
		}
		return lo, hi, true
	}
	// The parts above and below the band reach into it by the discs of
	// radius m around their points.
	for _, edge := range [2]float64{y0, y1} {
		l, h, ok := capReach(a, b, edge, m, edge == y1)
		if ok {
			add(l, h)
		}
	}
	if lo > hi {
		return 0, 0, false
	}
	return lo, hi, true
}

// capReach returns the leftmost and rightmost points, at the band edge e,
// of the discs of radius m around the points of the segment a-b on the
// far side of e: below it when below is set, above it otherwise; false
// when no disc reaches e.
func capReach(a, b wrapPt, e, m float64, below bool) (float64, float64, bool) {
	// u is the distance from e into the far side, x the point's x; both
	// are linear along the segment.
	ua, ub := e-a.y, e-b.y
	if below {
		ua, ub = a.y-e, b.y-e
	}
	// Clip the segment to 0 <= u <= m.
	t0, t1 := 0.0, 1.0
	du := ub - ua
	if math.Abs(du) <= 1e-12 {
		if ua < 0 || ua > m {
			return 0, 0, false
		}
	} else {
		ta, tb := (0-ua)/du, (m-ua)/du
		t0, t1 = max(t0, min(ta, tb)), min(t1, max(ta, tb))
		if t0 > t1 {
			return 0, 0, false
		}
	}
	x := func(t float64) float64 { return a.x + t*(b.x-a.x) }
	u := func(t float64) float64 { return ua + t*du }
	reach := func(t float64) float64 { return math.Sqrt(max(m*m-u(t)*u(t), 0)) }
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, t := range [2]float64{t0, t1} {
		lo, hi = min(lo, x(t)-reach(t)), max(hi, x(t)+reach(t))
	}
	if math.Abs(du) > 1e-12 {
		// x + sqrt(m² - u²) peaks where u = m k / sqrt(1 + k²), k being
		// dx/du; x - sqrt(m² - u²) bottoms out at the opposite k.
		k := (b.x - a.x) / du
		for _, uk := range [2]float64{m * k / math.Sqrt(1+k*k), -m * k / math.Sqrt(1+k*k)} {
			t := (uk - ua) / du
			if t > t0 && t < t1 {
				lo, hi = min(lo, x(t)-reach(t)), max(hi, x(t)+reach(t))
			}
		}
	}
	return lo, hi, true
}

// rowExtent returns the left and right edges of the image rows over the
// band [y0, y1), grown by the margin; false when none lies there.
func (s *floatShape) rowExtent(y0, y1 float64) (float64, float64, bool) {
	lo, hi := math.Inf(1), math.Inf(-1)
	m := s.margin
	first := max(int(math.Floor((y0-m-s.rowTop)/s.rowH)), 0) // fits int: clamped to the row count below
	for i := first; i < len(s.rows); i++ {
		top := s.rowTop + float64(i)*s.rowH
		if top > y1+m {
			break
		}
		r := s.rows[i]
		if r.x0 > r.x1 {
			continue
		}
		reach := m
		if m > 0 {
			// The distance from the band to the row, which the margin's
			// disc around the row's ends must cover.
			d := max(y0-(top+s.rowH), top-y1, 0)
			if d > m {
				continue
			}
			reach = math.Sqrt(m*m - d*d)
		} else if top+s.rowH <= y0 || top > y1 || (top == y1 && y1 > y0) {
			continue
		}
		lo, hi = min(lo, r.x0-reach), max(hi, r.x1+reach)
	}
	if lo > hi {
		return 0, 0, false
	}
	return lo, hi, true
}
