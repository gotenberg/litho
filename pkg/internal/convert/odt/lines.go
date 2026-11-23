package odt

import (
	"context"
	"math"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/svg"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// defaultMarkerWidth is the width of a line end whose style names a
// draw:marker without its draw:marker-start-width or -end-width, which
// ODF 1.3 Part 3, 20.143 and 20.146 leave to the application.
//
// # Corpus evidence
//
//   - probe, odt: on gotenberg/gotenberg:9.4.1 an Arrow marker of no
//     stated width on a hairline prints 10 points wide.
const defaultMarkerWidth = 10.0

// lineDrawing is a draw:line as Writer draws it (ODF 1.3 Part 3, 10.3.3):
// a stroke between its ends, svg:stroke-width wide with butt caps, and
// the outlines of its draw:marker-start and draw:marker-end filled in the
// stroke's colour. Box is the bound of all of it in the coordinates of
// svg:x1 to svg:y2, the rectangle Writer wraps text around.
type lineDrawing struct {
	box   typeset.Rect
	ends  [2]typeset.Point
	width float64
	color typeset.Color
	heads [][]typeset.PathOp
}

// lineDrawing reads the line c and its graphic properties p. A line end
// carries the marker's outline scaled to draw:marker-*-width across the
// line and in proportion along it, its svg:viewBox top at the line's end
// and its bottom towards the line, or its centre at the end under
// draw:marker-*-center (20.144, 20.147); the stroke stops where the
// marker's bottom lies. A line of draw:stroke none draws nothing.
//
// # Corpus evidence
//
//   - libreoffice/odt: odfexport/shapes-hyperlink.odt prints its 1 cm
//     line with a 1.676 cm Arrow marker, the tip at svg:x2 and the stroke
//     ending at the marker's base, and wraps the next paragraph below the
//     marker's lowest point.
//   - probe, odt: on gotenberg/gotenberg:9.4.1 a 0.3 cm Square marker at a
//     line's start lies along the line from the start inwards; a centred
//     Arrow end has its centre on the line's end, the stroke stopping at
//     the marker's base.
func (b *builder) lineDrawing(c *odf.Element, p odf.Props) lineDrawing {
	x1, _ := odf.Length(c.A(odf.NSSVG, "x1"))
	y1, _ := odf.Length(c.A(odf.NSSVG, "y1"))
	x2, _ := odf.Length(c.A(odf.NSSVG, "x2"))
	y2, _ := odf.Length(c.A(odf.NSSVG, "y2"))
	d := lineDrawing{ends: [2]typeset.Point{{X: x1, Y: y1}, {X: x2, Y: y2}}}
	d.color = typeset.Color{R: 0x34 / 255.0, G: 0x65 / 255.0, B: 0xa4 / 255.0, A: 1}
	if col, ok := odf.ParseColor(p.Get("svg:stroke-color")); ok {
		d.color = typeset.Color{R: float64(col.R) / 255, G: float64(col.G) / 255, B: float64(col.B) / 255, A: 1}
	}
	if w, ok := odf.Length(p.Get("svg:stroke-width")); ok && numeric.PositiveFinite(w) {
		d.width = w
	}
	stroked := p.Get("draw:stroke") != "none"
	dx, dy := x2-x1, y2-y1
	length := math.Sqrt(float64(dx*dx) + float64(dy*dy))
	if stroked && length > 0 {
		ux, uy := dx/length, dy/length
		start, end := d.ends[0], d.ends[1]
		if ops, back, ok := b.marker(p, "start", start, typeset.Point{X: -ux, Y: -uy}); ok {
			d.heads = append(d.heads, ops)
			start = typeset.Point{X: start.X + float64(back*ux), Y: start.Y + float64(back*uy)}
		}
		if ops, back, ok := b.marker(p, "end", end, typeset.Point{X: ux, Y: uy}); ok {
			d.heads = append(d.heads, ops)
			end = typeset.Point{X: end.X - float64(back*ux), Y: end.Y - float64(back*uy)}
		}
		d.ends = [2]typeset.Point{start, end}
	}
	minX, minY := math.Min(x1, x2), math.Min(y1, y2)
	maxX, maxY := math.Max(x1, x2), math.Max(y1, y2)
	grow := func(q typeset.Point) {
		minX, maxX = math.Min(minX, q.X), math.Max(maxX, q.X)
		minY, maxY = math.Min(minY, q.Y), math.Max(maxY, q.Y)
	}
	if stroked && d.width > 0 && length > 0 {
		// The butt ends of the stroke reach half its width either side.
		nx, ny := float64(-dy/length*d.width/2), float64(dx/length*d.width/2)
		for _, e := range d.ends {
			grow(typeset.Point{X: e.X + nx, Y: e.Y + ny})
			grow(typeset.Point{X: e.X - nx, Y: e.Y - ny})
		}
	}
	for _, h := range d.heads {
		for _, op := range h {
			n := 1
			switch op.Kind {
			case typeset.CurveTo:
				n = 3
			case typeset.ClosePath:
				n = 0
			case typeset.MoveTo, typeset.LineTo:
			}
			for _, q := range op.P[:n] {
				grow(q)
			}
		}
	}
	if !stroked {
		d.width = -1
	}
	d.box = typeset.Rect{X: minX, Y: minY, Width: maxX - minX, Height: maxY - minY}
	return d
}

// lineOf returns the drawing of the line c placed by g. A line anchored
// to a paragraph and placed from it keeps its box below the paragraph's
// top, what it draws above that line overflowing the box: Writer wraps
// the lines before a paragraph around a frame or a shape that reaches
// above it, never around a line.
//
// # Corpus evidence
//
//   - libreoffice/odt: odfexport/shapes-hyperlink.odt draws its arrow 2.2
//     points into the line before the arrow's paragraph, which stays.
//   - probe, odt: on gotenberg/gotenberg:9.4.1 lines whose stroke or
//     marker reaches 0.1 and 0.15 cm above their paragraph, and one whose
//     ends lie 0.5 cm above it, leave the line before the paragraph in
//     place, where a text frame or a rectangle 0.1 cm above it moves that
//     line below itself.
func (b *builder) lineOf(c *odf.Element, g frameGeom) lineDrawing {
	d := b.lineDrawing(c, g.props)
	if (g.anchor == "paragraph" || g.anchor == "char") && (g.vrel == "" || g.vrel == "paragraph" || g.vrel == "paragraph-content") && d.box.Y < 0 {
		d.box.Height = math.Max(0, d.box.Y+d.box.Height)
		d.box.Y = 0
	}
	return d
}

// marker returns the outline of the line end named by draw:marker-<end>
// at the point at, the line leaving the marker in the unit direction out
// (pointing away from the line), and how far back along the line the
// stroke stops; ok is false when the end has no marker.
func (b *builder) marker(p odf.Props, end string, at, out typeset.Point) ([]typeset.PathOp, float64, bool) {
	name := p.Get("draw:marker-" + end)
	if name == "" {
		return nil, 0, false
	}
	m := b.draws().Markers[name]
	if m == nil || !(m.ViewBox[2] > 0) || !(m.ViewBox[3] > 0) {
		return nil, 0, false
	}
	ops, _ := svg.PathData(m.D)
	if len(ops) == 0 {
		return nil, 0, false
	}
	w, ok := odf.Length(p.Get("draw:marker-" + end + "-width"))
	if !ok || !numeric.PositiveFinite(w) {
		w = defaultMarkerWidth
	}
	h := float64(w * m.ViewBox[3] / m.ViewBox[2])
	// The viewBox's top lies on the line's end and its bottom h back
	// along the line; a centred marker moves forward by half of h.
	shift := 0.0
	if p.Get("draw:marker-"+end+"-center") == "true" {
		shift = float64(h / 2)
	}
	// across is the line's left normal of out.
	nx, ny := -out.Y, out.X
	place := func(q typeset.Point) typeset.Point {
		across := float64((q.X-m.ViewBox[0])/m.ViewBox[2]-0.5) * w
		along := shift - float64((q.Y-m.ViewBox[1])/m.ViewBox[3]*h)
		return typeset.Point{X: at.X + float64(along*out.X) + float64(across*nx), Y: at.Y + float64(along*out.Y) + float64(across*ny)}
	}
	placed := make([]typeset.PathOp, len(ops))
	for i, op := range ops {
		placed[i] = op
		for j := range op.P {
			placed[i].P[j] = place(op.P[j])
		}
	}
	return placed, h - shift, true
}

// picture returns the line's picture, its box d.box.
func (d lineDrawing) picture() *layout.Picture {
	w, h := math.Max(d.box.Width, 1), math.Max(d.box.Height, 1)
	pic := &layout.Picture{Width: w, Height: h}
	pic.Vector = func(_ context.Context, r typeset.Rect) []typeset.Fragment {
		if d.width < 0 {
			return nil
		}
		ox, oy := r.X-d.box.X, r.Y-d.box.Y
		at := func(q typeset.Point) typeset.Point { return typeset.Point{X: q.X + ox, Y: q.Y + oy} }
		out := []typeset.Fragment{&typeset.Path{
			Ops:    []typeset.PathOp{{Kind: typeset.MoveTo, P: [3]typeset.Point{at(d.ends[0])}}, {Kind: typeset.LineTo, P: [3]typeset.Point{at(d.ends[1])}}},
			Stroke: &typeset.Stroke{Color: d.color, Width: d.width, Cap: typeset.ButtCap, Join: typeset.RoundJoin},
		}}
		for _, head := range d.heads {
			ops := make([]typeset.PathOp, len(head))
			for i, op := range head {
				ops[i] = op
				for j := range op.P {
					ops[i].P[j] = at(op.P[j])
				}
			}
			out = append(out, &typeset.Path{Ops: ops, Fill: &typeset.Fill{Color: d.color}})
		}
		return out
	}
	return pic
}
