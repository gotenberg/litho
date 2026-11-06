package compose

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
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

// maskUse is one ExtGState with a soft mask a page's resources name.
type maskUse struct {
	name  string
	alpha alphaState
	smask ast.PdfIndirectRef
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

// shading returns the objects of sh, writing them on first use.
func (w *docWriter) shading(sh *Shading) shadingRefs {
	key := shadingKey(sh)
	if r, ok := w.shadings[key]; ok {
		return r
	}
	rgb := func(c Color) ast.PdfArray { return ast.PdfArray{pdfReal(c.R), pdfReal(c.G), pdfReal(c.B)} }
	r := shadingRefs{color: w.alloc.Alloc(shadingDict(sh, ast.PdfName("DeviceRGB"), rgb))}
	for _, s := range sh.Stops {
		if round(s.Color.A) < 1 {
			r.translucent = true
			break
		}
	}
	if r.translucent {
		grey := func(c Color) ast.PdfArray { return ast.PdfArray{pdfReal(c.A)} }
		r.alpha = w.alloc.Alloc(shadingDict(sh, ast.PdfName("DeviceGray"), grey))
	}
	w.shadings[key] = r
	return r
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
	sh := pa.Fill.Shading
	refs := p.w.shading(sh)
	p.mark(pa.Parent)
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
	m := shadingMatrix(sh)
	if m != Identity {
		fmt.Fprintf(&p.buf, "%s %s %s %s %s %s cm\n", num(m.A), num(m.B), num(m.C), num(m.D), num(m.E), num(m.F))
	}
	cur := p.currentAlpha()
	if refs.translucent {
		form := p.w.maskForm(refs.alpha, inverseBBox(pa.Ops, m))
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
// mapped through the inverse of m, grown by one unit a side: the control
// points of a curve hold it in their hull, so the box covers the path in
// the shading's coordinates. Validate has checked m invertible.
func inverseBBox(ops []PathOp, m Matrix) [4]float64 {
	det := m.A*m.D - m.B*m.C
	inv := func(pt Point) (float64, float64) {
		x, y := pt.X-m.E, pt.Y-m.F
		return (m.D*x - m.C*y) / det, (-m.B*x + m.A*y) / det
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
	if !finite(box[0]) || !finite(box[1]) || !finite(box[2]) || !finite(box[3]) {
		return [4]float64{}
	}
	return [4]float64{box[0] - 1, box[1] - 1, box[2] + 1, box[3] + 1}
}
