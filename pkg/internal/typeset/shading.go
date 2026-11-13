package typeset

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// finePrecision is the number of decimals a shading's coordinates and its
// function's bounds keep: [precision] would round two stops a few
// hundredths of a point apart on a long axis onto one bound, and ISO
// 32000-1, 7.10.4 wants the bounds of a stitching function increasing.
const finePrecision = 1e6

// fine returns f rounded to [finePrecision] as a PDF real.
func fine(f float64) ast.PdfReal {
	r := math.Round(f*finePrecision) / finePrecision
	if r == 0 {
		return 0
	}
	return ast.PdfReal(r)
}

// shadingRefs are the objects of one distinct shading: the colour shading,
// and the grey shading of its alpha when a stop is not opaque.
type shadingRefs struct {
	color ast.PdfIndirectRef
	alpha ast.PdfIndirectRef
	// translucent is set when alpha is written.
	translucent bool
}

// shUse is one shading a page's resources name.
type shUse struct {
	name string
	ref  ast.PdfIndirectRef
}

// maskUse is one ExtGState with a soft mask a page's resources name, and
// the blend mode it sets when withBlend is set.
type maskUse struct {
	name      string
	alpha     alphaState
	smask     ast.PdfIndirectRef
	blend     BlendMode
	withBlend bool
}

// shadingKey returns what identifies sh's dictionaries: its kind,
// coordinates and stops as written. Two shadings with equal keys, placed
// under different transforms, share their objects.
func shadingKey(sh *Shading) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d %v %v %v %v %v %v", sh.Kind, fine(sh.From.X), fine(sh.From.Y), fine(sh.To.X), fine(sh.To.Y), fine(sh.FromRadius), fine(sh.ToRadius))
	for _, s := range sh.Stops {
		c := s.Color
		fmt.Fprintf(&b, ";%v %v %v %v %v", fine(s.Offset), pdfReal(c.R), pdfReal(c.G), pdfReal(c.B), pdfReal(c.A))
	}
	return b.String()
}

// shading returns the objects of sh, writing them on first use. domain
// is the box a conic shading paints, in its own coordinates, which its
// objects depend on; the other kinds ignore it.
func (w *docWriter) shading(sh *Shading, domain [4]float64) shadingRefs {
	key := shadingKey(sh)
	if sh.Kind == Conic {
		key += fmt.Sprintf(";%v %v %v %v", fine(domain[0]), fine(domain[1]), fine(domain[2]), fine(domain[3]))
	}
	if r, ok := w.shadings[key]; ok {
		return r
	}
	rgb := func(c Color) ast.PdfArray { return ast.PdfArray{pdfReal(c.R), pdfReal(c.G), pdfReal(c.B)} }
	dict := func(cs ast.PdfName, comp func(Color) ast.PdfArray) ast.PdfDict {
		if sh.Kind == Conic {
			return w.conicDict(sh, domain, cs, comp)
		}
		return shadingDict(sh, cs, comp)
	}
	r := shadingRefs{color: w.alloc.Alloc(dict(ast.PdfName("DeviceRGB"), rgb))}
	for _, s := range sh.Stops {
		if round(s.Color.A) < 1 {
			r.translucent = true
			break
		}
	}
	if r.translucent {
		grey := func(c Color) ast.PdfArray { return ast.PdfArray{pdfReal(c.A)} }
		r.alpha = w.alloc.Alloc(dict(ast.PdfName("DeviceGray"), grey))
	}
	w.shadings[key] = r
	return r
}

// conicDict returns the dictionary of the conic shading sh over domain:
// type 1, function-based (ISO 32000-1, 8.7.4.5.2, Table 79), whose
// /Matrix puts From at the origin of its domain and To on the positive x
// axis, so that the angle of a point, clockwise on the page, is the atan
// of its coordinates; its function is a PostScript calculator function
// (7.10.5) of that angle as a fraction of a turn, which looks the stops up
// by bisection and interpolates between two linearly, as the type 2
// functions of an axial shading do.
func (w *docWriter) conicDict(sh *Shading, domain [4]float64, cs ast.PdfName, comp func(Color) ast.PdfArray) ast.PdfDict {
	ux, uy := sh.To.X-sh.From.X, sh.To.Y-sh.From.Y
	n := detmath.Hypot(ux, uy)
	ux, uy = ux/n, uy/n
	dom := ast.PdfArray{fine(domain[0]), fine(domain[1]), fine(domain[2]), fine(domain[3])}
	ncomp := len(comp(Color{}))
	rng := make(ast.PdfArray, 0, 2*ncomp)
	for range ncomp {
		rng = append(rng, ast.PdfInt(0), ast.PdfInt(1))
	}
	var prog strings.Builder
	prog.WriteString("{exch atan 360 div\n")
	writeConicLookup(&prog, conicSegments(sh.Stops), comp)
	prog.WriteString("}\n")
	fn := ast.NewStream(ast.PdfDict{
		"FunctionType": ast.PdfInt(4),
		"Domain":       dom,
		"Range":        rng,
	}, []byte(prog.String()))
	return ast.PdfDict{
		"ShadingType": ast.PdfInt(1),
		"ColorSpace":  cs,
		"Domain":      dom,
		"Matrix":      ast.PdfArray{fine(ux), fine(uy), fine(-uy), fine(ux), fine(sh.From.X), fine(sh.From.Y)},
		"Function":    w.alloc.Alloc(fn),
	}
}

// conicSegment is one ramp of a conic shading's stops: from offset a with
// colour ca to offset b with colour cb, a below b.
type conicSegment struct {
	a, b   float64
	ca, cb Color
}

// conicSegments returns the ramps between the stops apart, as
// [stopFunction] stitches them: a pair at one offset is a sharp edge.
func conicSegments(stops []ColorStop) []conicSegment {
	var out []conicSegment
	for i := 0; i+1 < len(stops); i++ {
		a, b := stops[i], stops[i+1]
		if fine(b.Offset) <= fine(a.Offset) {
			continue
		}
		out = append(out, conicSegment{a: a.Offset, b: b.Offset, ca: a.Color, cb: b.Color})
	}
	return out
}

// writeConicLookup writes the PostScript that turns the offset on the
// stack into the components comp takes of the colour segs give it: a
// bisection on the segments' starts, then the linear ramp of the one
// segment left. The bisection keeps the program's nesting logarithmic in
// the stops.
func writeConicLookup(b *strings.Builder, segs []conicSegment, comp func(Color) ast.PdfArray) {
	if len(segs) > 1 {
		mid := len(segs) / 2
		fmt.Fprintf(b, "dup %s lt {\n", psNum(segs[mid].a))
		writeConicLookup(b, segs[:mid], comp)
		b.WriteString("} {\n")
		writeConicLookup(b, segs[mid:], comp)
		b.WriteString("} ifelse\n")
		return
	}
	s := segs[0]
	ca, cb := comp(s.ca), comp(s.cb)
	fmt.Fprintf(b, "%s sub %s mul", psNum(s.a), psNum(1/(s.b-s.a)))
	for i := range ca {
		x, y := float64(ca[i].(ast.PdfReal)), float64(cb[i].(ast.PdfReal))
		if i < len(ca)-1 {
			b.WriteString(" dup")
		}
		fmt.Fprintf(b, " %s mul %s add", psNum(y-x), psNum(x))
		if i < len(ca)-1 {
			b.WriteString(" exch")
		}
	}
	b.WriteString("\n")
}

// psNum formats a number of a PostScript calculator function: at most six
// decimals, never an exponent, which the calculator's grammar lacks (ISO
// 32000-1, 7.10.5.1).
func psNum(f float64) string {
	return strconv.FormatFloat(float64(fine(f)), 'f', -1, 64)
}

// shadingDict returns the shading dictionary of sh in colour space cs, its
// stops' components taken by comp: type 2, axial, with /Coords [x0 y0 x1
// y1] (ISO 32000-1, 8.7.4.5.3, Table 80), or type 3, radial, with /Coords
// [x0 y0 r0 x1 y1 r1] (8.7.4.5.4, Table 81), extended past both ends.
func shadingDict(sh *Shading, cs ast.PdfName, comp func(Color) ast.PdfArray) ast.PdfDict {
	d := ast.PdfDict{
		"ColorSpace": cs,
		"Function":   stopFunction(sh.Stops, comp),
		"Extend":     ast.PdfArray{ast.PdfBool(true), ast.PdfBool(true)},
	}
	if sh.Kind == Radial {
		d["ShadingType"] = ast.PdfInt(3)
		d["Coords"] = ast.PdfArray{fine(sh.From.X), fine(sh.From.Y), fine(sh.FromRadius), fine(sh.To.X), fine(sh.To.Y), fine(sh.ToRadius)}
	} else {
		d["ShadingType"] = ast.PdfInt(2)
		d["Coords"] = ast.PdfArray{fine(sh.From.X), fine(sh.From.Y), fine(sh.To.X), fine(sh.To.Y)}
	}
	return d
}

// stopFunction returns the function of t in [0, 1] the stops describe: one
// type 2 exponential function of exponent 1 per pair of stops apart (ISO
// 32000-1, 7.10.3), stitched by a type 3 function (7.10.4) when there are
// several. A pair at one offset, a sharp edge, has no function of its own:
// the stitching bound between its neighbours switches colour there. The
// bounds increase strictly inside (0, 1), as 7.10.4 asks, since a pair
// whose offsets round together is dropped.
func stopFunction(stops []ColorStop, comp func(Color) ast.PdfArray) ast.PdfObject {
	unit := ast.PdfArray{ast.PdfInt(0), ast.PdfInt(1)}
	var fns, bounds, encode ast.PdfArray
	for i := 0; i+1 < len(stops); i++ {
		a, b := stops[i], stops[i+1]
		if fine(b.Offset) <= fine(a.Offset) {
			continue
		}
		if len(fns) > 0 {
			bounds = append(bounds, fine(a.Offset))
		}
		fns = append(fns, ast.PdfDict{
			"FunctionType": ast.PdfInt(2),
			"Domain":       unit,
			"C0":           comp(a.Color),
			"C1":           comp(b.Color),
			"N":            ast.PdfInt(1),
		})
		encode = append(encode, ast.PdfInt(0), ast.PdfInt(1))
	}
	if len(fns) == 1 {
		return fns[0]
	}
	if bounds == nil {
		bounds = ast.PdfArray{}
	}
	return ast.PdfDict{
		"FunctionType": ast.PdfInt(3),
		"Domain":       unit,
		"Functions":    fns,
		"Bounds":       bounds,
		"Encode":       encode,
	}
}

// shadingFill fills the path pa with its shading: the path becomes the
// clip, the shading's transform the CTM, and sh paints the clip (ISO
// 32000-1, 8.7.4.2). A shading with translucent stops paints under a soft
// mask (11.6.5.2): a luminosity mask whose group paints the grey shading
// of the stops' alpha in the same coordinates, the mask's coordinate
// system being the one in force when gs selects it.
func (p *pageWriter) shadingFill(ctx context.Context, pa *Path, opacity float64) error {
	sh, m := placedShading(pa.Fill.Shading, p.off)
	var domain [4]float64
	if sh.Kind == Conic {
		domain = conicDomain(sh, inverseBBox(pa.Ops, p.off, m))
	}
	refs := p.w.shading(sh, domain)
	p.mark(pa.Parent, pa.Order)
	p.buf.WriteString("q\n")
	p.alpha = append(p.alpha, p.currentAlpha())
	if err := p.ops(ctx, pa.Ops); err != nil {
		return err
	}
	if pa.Fill.Rule == EvenOdd {
		p.buf.WriteString("W* n\n")
	} else {
		p.buf.WriteString("W n\n")
	}
	if m != Identity {
		fmt.Fprintf(&p.buf, "%s %s %s %s %s %s cm\n", num(m.A), num(m.B), num(m.C), num(m.D), num(m.E), num(m.F))
	}
	cur := p.currentAlpha()
	if refs.translucent {
		form := p.w.maskForm(refs.alpha, inverseBBox(pa.Ops, p.off, m))
		name := fmt.Sprintf("GM%d", len(p.masks)+1)
		p.masks = append(p.masks, maskUse{name: name, alpha: alphaState{fill: round(opacity), stroke: cur.stroke}, smask: form})
		fmt.Fprintf(&p.buf, "/%s gs\n", name)
	} else {
		p.setAlpha(alphaState{fill: opacity, stroke: cur.stroke})
	}
	fmt.Fprintf(&p.buf, "/%s sh\nQ\n", p.shadingName(refs.color))
	p.alpha = p.alpha[:len(p.alpha)-1]
	return nil
}

// conicDomain returns the domain [x0 x1 y0 y1] of the conic shading sh
// that covers box, [x0 y0 x1 y1] in the shading's coordinates: box
// mapped into the coordinates whose origin is From and whose x axis runs
// toward To.
func conicDomain(sh *Shading, box [4]float64) [4]float64 {
	ux, uy := sh.To.X-sh.From.X, sh.To.Y-sh.From.Y
	n := detmath.Hypot(ux, uy)
	ux, uy = ux/n, uy/n
	out := [4]float64{math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)}
	for _, c := range [4][2]float64{{box[0], box[1]}, {box[2], box[1]}, {box[0], box[3]}, {box[2], box[3]}} {
		dx, dy := c[0]-sh.From.X, c[1]-sh.From.Y
		x, y := float64(dx*ux)+float64(dy*uy), float64(-dx*uy)+float64(dy*ux)
		out = [4]float64{min(out[0], x), max(out[1], x), min(out[2], y), max(out[3], y)}
	}
	return out
}

// placedShading returns the shading sh paints with once the folded
// translation off of the groups (see [pageWriter.group]) joins it, and the
// matrix that places it in the coordinates written. A shading with no
// transform of its own takes the translation into its coordinates, so its
// dictionary holds numbers near the page's (6.1.12 of ISO 19005-1 bounds
// every real of a PDF/A-1 file, a dictionary's too); one with a transform
// takes it into the matrix, its coordinates being its own space's.
func placedShading(sh *Shading, off Point) (*Shading, Matrix) {
	m := shadingMatrix(sh)
	if off == (Point{}) {
		return sh, m
	}
	if m == Identity {
		c := *sh
		c.From = Point{X: sh.From.X + off.X, Y: sh.From.Y + off.Y}
		c.To = Point{X: sh.To.X + off.X, Y: sh.To.Y + off.Y}
		return &c, m
	}
	m.E += off.X
	m.F += off.Y
	return sh, m
}

// shadingName returns the page's resource name of the shading ref.
func (p *pageWriter) shadingName(ref ast.PdfIndirectRef) string {
	for _, s := range p.shadings {
		if s.ref == ref {
			return s.name
		}
	}
	name := fmt.Sprintf("Sh%d", len(p.shadings)+1)
	p.shadings = append(p.shadings, shUse{name: name, ref: ref})
	return name
}

// maskForm writes the transparency group of a luminosity soft mask that
// paints the grey shading alpha over bbox (ISO 32000-1, 11.6.5.2: the group
// is a form XObject with a /Group of subtype /Transparency, here in
// DeviceGray, 11.6.6) and returns its reference. Outside bbox the mask's
// backdrop, black by default, masks everything out; bbox covers the clip.
func (w *docWriter) maskForm(alpha ast.PdfIndirectRef, bbox [4]float64) ast.PdfIndirectRef {
	form := ast.NewStream(ast.PdfDict{
		"Type":    ast.PdfName("XObject"),
		"Subtype": ast.PdfName("Form"),
		"BBox":    ast.PdfArray{fine(bbox[0]), fine(bbox[1]), fine(bbox[2]), fine(bbox[3])},
		"Group": ast.PdfDict{
			"Type": ast.PdfName("Group"),
			"S":    ast.PdfName("Transparency"),
			"CS":   ast.PdfName("DeviceGray"),
		},
		"Resources": ast.PdfDict{"Shading": ast.PdfDict{"Sh1": alpha}},
	}, []byte("/Sh1 sh\n"))
	formRef := w.alloc.Alloc(form)
	return w.alloc.Alloc(ast.PdfDict{
		"Type": ast.PdfName("Mask"),
		"S":    ast.PdfName("Luminosity"),
		"G":    formRef,
	})
}

// inverseBBox returns the bounding box [x0 y0 x1 y1] of the points of ops
// moved by off and mapped through the inverse of m, grown by one unit a
// side: the control points of a curve hold it in their hull, so the box
// covers the path in the shading's coordinates. Validate has checked m
// invertible.
func inverseBBox(ops []PathOp, off Point, m Matrix) [4]float64 {
	det := float64(m.A*m.D) - float64(m.B*m.C)
	inv := func(pt Point) (float64, float64) {
		x, y := pt.X+off.X-m.E, pt.Y+off.Y-m.F
		return (float64(m.D*x) - float64(m.C*y)) / det, (float64(-m.B*x) + float64(m.A*y)) / det
	}
	box := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, op := range ops {
		n := 0
		switch op.Kind {
		case MoveTo, LineTo:
			n = 1
		case CurveTo:
			n = 3
		case ClosePath:
		}
		for _, pt := range op.P[:n] {
			x, y := inv(pt)
			box = [4]float64{min(box[0], x), min(box[1], y), max(box[2], x), max(box[3], y)}
		}
	}
	if !numeric.Finite(box[0]) || !numeric.Finite(box[1]) || !numeric.Finite(box[2]) || !numeric.Finite(box[3]) {
		return [4]float64{}
	}
	return [4]float64{box[0] - 1, box[1] - 1, box[2] + 1, box[3] + 1}
}
