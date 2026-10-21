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
