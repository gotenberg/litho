package flatten

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// Rect is an axis-aligned box in default user space. A box with no area
// paints nothing and overlaps nothing.
type Rect struct {
	MinX, MinY, MaxX, MaxY float64
}

// Empty reports whether r has no area.
func (r Rect) Empty() bool {
	return !(r.MinX < r.MaxX && r.MinY < r.MaxY)
}

// Overlaps reports whether r and o share area.
func (r Rect) Overlaps(o Rect) bool {
	if r.Empty() || o.Empty() {
		return false
	}
	return r.MinX < o.MaxX && o.MinX < r.MaxX && r.MinY < o.MaxY && o.MinY < r.MaxY
}

// Union returns the smallest box holding r and o.
func (r Rect) Union(o Rect) Rect {
	switch {
	case r.Empty():
		return o
	case o.Empty():
		return r
	}
	return Rect{min(r.MinX, o.MinX), min(r.MinY, o.MinY), max(r.MaxX, o.MaxX), max(r.MaxY, o.MaxY)}
}

// Intersect returns the area r and o share.
func (r Rect) Intersect(o Rect) Rect {
	out := Rect{max(r.MinX, o.MinX), max(r.MinY, o.MinY), min(r.MaxX, o.MaxX), min(r.MaxY, o.MaxY)}
	if out.Empty() {
		return Rect{}
	}
	return out
}

// Contains reports whether o lies inside r.
func (r Rect) Contains(o Rect) bool {
	return r.MinX <= o.MinX && r.MinY <= o.MinY && o.MaxX <= r.MaxX && o.MaxY <= r.MaxY
}

// Outset returns r grown by d on every side.
func (r Rect) Outset(d float64) Rect {
	if r.Empty() {
		return r
	}
	return Rect{r.MinX - d, r.MinY - d, r.MaxX + d, r.MaxY + d}
}

// rectOf returns the box of four numbers in either corner order, empty when
// one is not finite.
func rectOf(x0, y0, x1, y1 float64) Rect {
	for _, v := range []float64{x0, y0, x1, y1} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return Rect{}
		}
	}
	return Rect{min(x0, x1), min(y0, y1), max(x0, x1), max(y0, y1)}
}

// pathBox returns the box of a path whose corners are x0, y0 and x1, y1,
// grown by reach on every side, the reach of its stroke when it is stroked.
// A stroked straight line has a path box with no area and paints the area
// its stroke covers (ISO 32000-2, 8.5.3.2), so the reach is added whatever
// the box's area, where [Rect.Outset] leaves a box with none as it is. The
// box is empty when a corner is not finite.
//
// # Corpus evidence
//
//   - pdfjs issue12579.pdf: its Ink annotations stroke straight lines at a
//     constant alpha of 0.7. Drawn into the page, each was read as painting
//     nothing and removed with no composite in its place, and page 2 lost its
//     20 lines.
func pathBox(x0, y0, x1, y1, reach float64) Rect {
	r := rectOf(x0, y0, x1, y1)
	for _, v := range []float64{x0, y0, x1, y1} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return r
		}
	}
	return Rect{r.MinX - reach, r.MinY - reach, r.MaxX + reach, r.MaxY + reach}
}

// transformRect returns the box of r's four corners through m.
func transformRect(m geom.Matrix, r Rect) Rect {
	corners := [4]geom.Point{{X: r.MinX, Y: r.MinY}, {X: r.MaxX, Y: r.MinY}, {X: r.MinX, Y: r.MaxY}, {X: r.MaxX, Y: r.MaxY}}
	out := Rect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, c := range corners {
		p := m.Apply(c)
		out.MinX, out.MinY = min(out.MinX, p.X), min(out.MinY, p.Y)
		out.MaxX, out.MaxY = max(out.MaxX, p.X), max(out.MaxY, p.Y)
	}
	return rectOf(out.MinX, out.MinY, out.MaxX, out.MaxY)
}

// edgeWidth is the width, in the units of the box it borders, given to the
// edge of a filled rectangle: an edge has no area, and a box needs some to
// overlap anything. Any width under a device pixel serves, since the pixel
// margin added in device space takes in the antialiased pixels either side.
const edgeWidth = 1e-3

// rectEdges returns the boxes holding the edges of r, the box of an
// axis-aligned rectangle painted with reach, half the width a stroke spreads
// over its edges (0 for a fill): four strips along its sides, or r whole
// when the strips would meet.
func rectEdges(r Rect, reach float64) []Rect {
	t := max(2*reach, edgeWidth)
	if 2*t >= r.MaxX-r.MinX || 2*t >= r.MaxY-r.MinY {
		return []Rect{r}
	}
	return []Rect{
		{r.MinX, r.MinY, r.MinX + t, r.MaxY},
		{r.MaxX - t, r.MinY, r.MaxX, r.MaxY},
		{r.MinX, r.MinY, r.MaxX, r.MinY + t},
		{r.MinX, r.MaxY - t, r.MaxX, r.MaxY},
	}
}

// axisAligned reports whether m maps axis-aligned rectangles onto
// axis-aligned rectangles: no shear and a turn by a multiple of 90 degrees.
func axisAligned(m geom.Matrix) bool {
	return (m[1] == 0 && m[2] == 0) || (m[0] == 0 && m[3] == 0)
}
