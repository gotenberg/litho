package sheet

import (
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// gridWidth is the width of the grid lines Calc prints, in points, and
// they are black (ScPrintFunc::PrintArea, aGridColor COL_BLACK).
//
// # Corpus evidence
//
//   - probe, ods: a flat ODS sheet printed with its grid by Gotenberg
//     9.0.0 strokes the grid lines at 0.1 point in black, after the cell text,
//     and frames the printed cells.
const gridWidth = 0.1

// hairline is the width a border line thinner than it draws at.
const hairline = 0.05

// edgeKey names one cell edge of a page: horizontal edges by the row
// whose top they are and a column, vertical ones by the column whose left
// they are and a row.
type edgeKey struct {
	vertical  bool
	at, along int
}

// edge is a border line to draw.
type edge struct {
	b          Border
	x1, y1, x2 float64
	y2         float64
}

// stronger reports whether a draws over b on a shared edge, as Calc keeps
// the heavier line: the wider, then the double, then the solid one.
func stronger(a, b Border) bool {
	if a.Line == LineNone {
		return false
	}
	if b.Line == LineNone {
		return true
	}
	if a.Width != b.Width {
		return a.Width > b.Width
	}
	rank := func(l Line) int {
		switch l {
		case LineDouble:
			return 3
		case LineSolid:
			return 2
		case LineDashed, LineDashDot, LineDashDotDot:
			return 1
		case LineDotted, LineNone:
		}
		return 0
	}
	return rank(a.Line) > rank(b.Line)
}

// borders collects the border edges of the units of a region, a shared
// edge drawn once with the stronger line.
type borders struct {
	edges map[edgeKey]edge
	order []edgeKey
}

func newBorders() *borders { return &borders{edges: map[edgeKey]edge{}} }

// add records the border b along an edge.
func (bs *borders) add(k edgeKey, b Border, x1, y1, x2, y2 float64) {
	if b.Line == LineNone {
		return
	}
	cur, ok := bs.edges[k]
	if ok && !stronger(b, cur.b) {
		return
	}
	if !ok {
		bs.order = append(bs.order, k)
	}
	bs.edges[k] = edge{b: b, x1: x1, y1: y1, x2: x2, y2: y2}
}

// unitBorders records the four borders of u, one edge a row or a column
// it spans.
func (b *pageBuilder) unitBorders(bs *borders, rg *region, u *unit) {
	g := b.g
	st := u.st
	for c := u.c; c <= u.lastC; c++ {
		if g.colHidden(c) {
			continue
		}
		x1, x2 := rg.x(g, c), rg.x(g, c+1)
		bs.add(edgeKey{at: u.r, along: c}, st.Border[css.Top], x1, u.y, x2, u.y)
		next := b.nextRow(u.lastR)
		bs.add(edgeKey{at: next, along: c}, st.Border[css.Bottom], x1, u.y+u.h, x2, u.y+u.h)
	}
	for r := u.r; r <= u.lastR; r++ {
		if g.rowHidden(r) {
			continue
		}
		y1, y2 := rg.y(g, r), rg.y(g, r+1)
		bs.add(edgeKey{vertical: true, at: u.c, along: r}, st.Border[css.Left], u.x, y1, u.x, y2)
		next := b.nextCol(u.lastC)
		bs.add(edgeKey{vertical: true, at: next, along: r}, st.Border[css.Right], u.x+u.w, y1, u.x+u.w, y2)
	}
	if (st.DiagonalUp || st.DiagonalDown) && st.Diagonal.Line != LineNone {
		if st.DiagonalDown {
			b.borderPaths = append(b.borderPaths, linePaths(st.Diagonal, u.x, u.y, u.x+u.w, u.y+u.h, false)...)
		}
		if st.DiagonalUp {
			b.borderPaths = append(b.borderPaths, linePaths(st.Diagonal, u.x, u.y+u.h, u.x+u.w, u.y, false)...)
		}
	}
}

// nextRow returns the first row after r that is not hidden.
func (b *pageBuilder) nextRow(r int) int {
	for i := r + 1; i < maxRows && i <= r+maxOverflowScan; i++ {
		if !b.g.rowHidden(i) {
			return i
		}
	}
	return r + 1
}

// nextCol returns the first column after c that is not hidden.
func (b *pageBuilder) nextCol(c int) int {
	for i := c + 1; i < maxColumns && i <= c+maxOverflowScan; i++ {
		if !b.g.colHidden(i) {
			return i
		}
	}
	return c + 1
}

// paths returns the border lines, in edge order.
func (bs *borders) paths() []compose.Fragment {
	out := make([]compose.Fragment, 0, len(bs.order))
	for _, k := range bs.order {
		e := bs.edges[k]
		out = append(out, linePaths(e.b, e.x1, e.y1, e.x2, e.y2, true)...)
	}
	return out
}

// linePaths returns the strokes of a border line from (x1, y1) to (x2,
// y2): one line, or two for a double one, extended by half its width at
// both ends when extend is set so that the lines of a corner meet, as Calc
// draws a cell frame.
func linePaths(b Border, x1, y1, x2, y2 float64, extend bool) []compose.Fragment {
	w := b.Width
	if !(w > 0) {
		w = hairline
	}
	w = min(w, maxBorderWidth)
	dx, dy := x2-x1, y2-y1
	horizontal := dy == 0
	vertical := dx == 0
	if extend && (horizontal || vertical) {
		if horizontal {
			x1, x2 = min(x1, x2)-w/2, max(x1, x2)+w/2
		} else {
			y1, y2 = min(y1, y2)-w/2, max(y1, y2)+w/2
		}
	}
	stroke := func(lw, off float64) compose.Fragment {
		ox, oy := 0.0, 0.0
		switch {
		case horizontal:
			oy = off
		case vertical:
			ox = off
		}
		s := &compose.Stroke{Color: colorOf(b.Color), Width: lw, Cap: compose.ButtCap}
		switch b.Line {
		case LineDashed:
			s.Dash = []float64{3 * lw, lw}
		case LineDotted:
			s.Dash = []float64{lw, lw}
		case LineDashDot:
			s.Dash = []float64{3 * lw, lw, lw, lw}
		case LineDashDotDot:
			s.Dash = []float64{3 * lw, lw, lw, lw, lw, lw}
		case LineSolid, LineDouble, LineNone:
		}
		if b.Color.A == 0 {
			s.Color = compose.Black
		}
		return &compose.Path{Ops: lineOps(x1+ox, y1+oy, x2+ox, y2+oy), Stroke: s}
	}
	if b.Line == LineDouble {
		gap := b.Gap
		if !(gap > 0) {
			gap = w / 3
		}
		gap = min(gap, w)
		lw := max((w-gap)/2, hairline)
		off := gap/2 + lw/2
		return []compose.Fragment{stroke(lw, -off), stroke(lw, off)}
	}
	return []compose.Fragment{stroke(w, 0)}
}

// maxBorderWidth caps a border line's width, in points. Anti-DoS guard,
// not spec-mandated: Calc's thickest line is 9 points.
const maxBorderWidth = 36
