package chart

import (
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// Hierarchy is what a treemap or sunburst series holds past its values
// and the names of its points (MS-ODRAWXML, 2.24.4.21 ST_SeriesLayout
// treemap and sunburst): each point is a leaf, named by
// [Series.Categories], under the parents its levels name.
type Hierarchy struct {
	// Parents holds the category levels above the series' categories,
	// the nearest first: Parents[0] names each point's parent,
	// Parents[1] its parent's parent. An empty name skips its level.
	Parents []Data
	// Banner keeps a strip over the cells of each parent of a treemap,
	// where Office sets the parent's label (MS-ODRAWXML, 2.24.4.17
	// ST_ParentLabelLayout banner).
	Banner bool
	// LineWidth is the width of the line each cell or sector is outlined
	// with in its own colour, 0 for none.
	LineWidth float64
}

// MaxLevels caps the category levels of a [Hierarchy]. Anti-DoS guard,
// not spec-mandated: Office offers a few levels, and each costs a pass
// over the points.
const MaxLevels = 8

// node is a parent or a leaf of a hierarchy: its name, the sum of the
// values under it, its children in the order the data first names them,
// the point it is for a leaf, and the top-level parent whose colour it
// takes.
type node struct {
	name     string
	value    float64
	children []*node
	index    map[string]*node
	point    int
	top      int
}

// tree returns the top-level nodes of series s, a treemap or sunburst
// series, in the order the data first names them, and the depth of its
// deepest leaf. A point whose value is not positive is left out, as are
// the parents left empty by it.
func (rd *renderer) tree(s *Series) ([]*node, int) {
	vals := rd.data(s.Values)
	cats := rd.data(s.Categories)
	var levels []Data
	if h := s.Hierarchy; h != nil {
		n := min(len(h.Parents), MaxLevels)
		for k := n - 1; k >= 0; k-- {
			levels = append(levels, rd.data(h.Parents[k]))
		}
	}
	root := &node{point: -1}
	depth := 0
	for j := range vals.Len() {
		if rd.ctx.Err() != nil {
			return nil, 0
		}
		v, ok := vals.number(j)
		if !ok || !numeric.PositiveFinite(v) {
			continue
		}
		at := root
		d := 0
		for _, l := range levels {
			name := l.label(j, rd.c.Locale)
			if name == "" {
				continue
			}
			at = at.child(name)
			at.value += v
			d++
		}
		leaf := &node{name: cats.label(j, rd.c.Locale), value: v, point: j}
		at.children = append(at.children, leaf)
		depth = max(depth, d+1)
	}
	for i, t := range root.children {
		t.paint(i)
	}
	return root.children, depth
}

// child returns the parent named name under n, made on first use.
func (n *node) child(name string) *node {
	if c, ok := n.index[name]; ok {
		return c
	}
	if n.index == nil {
		n.index = map[string]*node{}
	}
	c := &node{name: name, point: -1}
	n.index[name] = c
	n.children = append(n.children, c)
	return c
}

// paint gives n and every node under it the colour of the top-level
// parent i.
func (n *node) paint(i int) {
	n.top = i
	for _, c := range n.children {
		c.paint(i)
	}
}

// largestFirst returns ns ordered by value, the largest first, ties in
// the data's order.
func largestFirst(ns []*node) []*node {
	out := slices.Clone(ns)
	slices.SortStableFunc(out, func(a, b *node) int {
		switch {
		case a.value > b.value:
			return -1
		case a.value < b.value:
			return 1
		}
		return 0
	})
	return out
}

// hierarchyFill returns the fill of the cells and sectors under the
// top-level parent i of n parents: the series' own fill, else the
// automatic colour of the parent.
func (rd *renderer) hierarchyFill(s *Series, i, n int) *Paint {
	if s.Fill != nil {
		return s.Fill
	}
	return &Paint{Color: rd.c.AutoColor(i, n)}
}

// hierarchyLine returns the outline of a cell or sector filled with fill,
// nil for none.
func hierarchyLine(s *Series, fill *Paint) *Stroke {
	if s.Hierarchy == nil || !numeric.PositiveFinite(s.Hierarchy.LineWidth) || fill == nil || fill.None {
		return nil
	}
	return &Stroke{Color: fill.Color, Width: s.Hierarchy.LineWidth}
}

// hierarchy draws a treemap or sunburst group into plot, its first series.
func (rd *renderer) hierarchy(plot typeset.Rect) {
	g := rd.c.Groups[0]
	if len(g.Series) == 0 {
		return
	}
	s := g.Series[0]
	tops, depth := rd.tree(s)
	if len(tops) == 0 {
		return
	}
	if g.Kind == Sunburst {
		rd.sunburst(g, s, tops, depth, plot)
		return
	}
	rd.treemap(g, s, tops, plot)
}

// treemapBanner is the height of the strip a treemap keeps over the
// cells of each parent whose label stands in a banner, in points.
//
// # Corpus evidence
//
//   - probe, docx: Word lowers the cells of every parent of a treemap
//     whose parent labels stand in banners by 18.75 pt, 25 pixels, the
//     strip left blank where the data labels show values alone, under
//     9 pt labels. Other label sizes are not measured.
const treemapBanner = 18.75

// treemapLabelInset is the distance from a treemap cell's left edge to
// its label, and treemapLabelBase from the cell's bottom edge up to the
// baseline of the label's last line, in points.
//
// # Corpus evidence
//
//   - probe, docx: Word sets the 9 pt value labels of a treemap's cells
//     3.70 to 3.89 pt right of each cell's left edge, their baselines
//     6.00 to 6.25 pt over its bottom edge, in cells 42 to 113 pt high.
const (
	treemapLabelInset = 3.8
	treemapLabelBase  = 6.1
)

// treemap draws the nodes tops as a squarified treemap in plot (Bruls,
// Huizing and van Wijk, "Squarified Treemaps", 2000): the nodes, the
// largest first, are laid in rows along the shorter side of the room left,
// each row kept while adding a node does not make its worst aspect ratio
// worse; a row across a room wider than tall is a column at its left,
// its nodes from the top down, else a strip at its top, its nodes from
// the left. A parent's children fill its cell the same way, under a
// banner when [Hierarchy.Banner] asks for one; leaves are filled and
// labelled at their bottom left.
//
// # Corpus evidence
//
//   - probe, docx: Word lays Grain (40 and 15), Fruit (30 and 20) and Veg
//     (25 and 10) out in a plot area 384.26 by 154.75 pt as three columns
//     151.0, 137.25 and 96.0 pt wide from the left, the largest first,
//     each split from the top down, 40 over 15 in 112.75 and 42.0 pt;
//     under banners, Grain's cell 151 by 136 pt is split from the left
//     into 109.75 and 41.25 pt, as this rule orders and splits them.
//     Every cell is filled in its top-level parent's colour, accent1 to
//     accent3 in the data's order, and outlined 0.75 pt in it.
func (rd *renderer) treemap(g *Group, s *Series, tops []*node, plot typeset.Rect) {
	var labels []func()
	var lay func(ns []*node, r typeset.Rect)
	lay = func(ns []*node, r typeset.Rect) {
		if rd.ctx.Err() != nil {
			return
		}
		squarify(largestFirst(ns), r, func(n *node, cell typeset.Rect) {
			if len(n.children) > 0 {
				in := cell
				if s.Hierarchy != nil && s.Hierarchy.Banner {
					b := math.Min(treemapBanner, in.Height)
					in.Y += b
					in.Height -= b
				}
				lay(n.children, in)
				return
			}
			fill := rd.hierarchyFill(s, n.top, len(tops))
			rd.paint(rectPath(cell).Ops, fill, hierarchyLine(s, fill))
			lb := rd.labelsAt(g, s, n.point)
			if !lb.Any() {
				return
			}
			v, _ := rd.data(s.Values).number(n.point)
			labels = append(labels, func() {
				b := rd.measure(rd.labelText(g, s, n.point, v, lb, math.NaN()), lb.Style, 0)
				if b == nil || len(b.lines) == 0 {
					return
				}
				last := len(b.lines) - 1
				base := 0.0
				for i := range last {
					base += b.lineH[i]
				}
				l := b.lines[last]
				base += float64((b.lineH[last]-l.Ascent-l.Descent)/2) + l.Ascent
				top := cell.Y + cell.Height - treemapLabelBase - base
				rd.out = append(rd.out, b.draw(cell.X+treemapLabelInset+float64(b.w/2), top+float64(b.h/2), alignLeft)...)
			})
		})
	}
	lay(tops, plot)
	for _, l := range labels {
		l()
	}
}

// squarify lays the nodes ns, the largest first, out in r by the
// squarified treemap rule ([renderer.treemap]) and calls place with each
// node and its cell.
func squarify(ns []*node, r typeset.Rect, place func(*node, typeset.Rect)) {
	total := 0.0
	for _, n := range ns {
		total += n.value
	}
	if !numeric.PositiveFinite(total) || !(r.Width > 0) || !(r.Height > 0) {
		return
	}
	scale := float64(r.Width*r.Height) / total
	for i := 0; i < len(ns); {
		side := math.Min(r.Width, r.Height)
		sum, lo, hi := 0.0, math.Inf(1), math.Inf(-1)
		worst := math.Inf(1)
		j := i
		for ; j < len(ns); j++ {
			a := float64(ns[j].value * scale)
			s2, lo2, hi2 := sum+a, math.Min(lo, a), math.Max(hi, a)
			w := aspectWorst(s2, lo2, hi2, side)
			if j > i && w > worst {
				break
			}
			sum, lo, hi, worst = s2, lo2, hi2, w
		}
		thick := sum / side
		at := 0.0
		for _, n := range ns[i:j] {
			length := float64(n.value*scale) / thick
			if r.Width >= r.Height {
				place(n, typeset.Rect{X: r.X, Y: r.Y + at, Width: thick, Height: length})
			} else {
				place(n, typeset.Rect{X: r.X + at, Y: r.Y, Width: length, Height: thick})
			}
			at += length
		}
		if r.Width >= r.Height {
			r.X += thick
			r.Width = math.Max(0, r.Width-thick)
		} else {
			r.Y += thick
			r.Height = math.Max(0, r.Height-thick)
		}
		i = j
	}
}

// aspectWorst returns the worst aspect ratio of a row of cells of areas
// summing to sum, the least lo and the greatest hi, laid along a side of
// length side (the worst function of Bruls et al., 2000).
func aspectWorst(sum, lo, hi, side float64) float64 {
	s2, w2 := float64(sum*sum), float64(side*side)
	return math.Max(float64(w2*hi)/s2, s2/float64(w2*lo))
}

// sunburst draws the nodes tops as rings around a hole in plot: the
// outer radius half the plot area's lesser side, centred in it, the
// hole and each level a ring of equal width, the top-level parents
// innermost; at each level the nodes run clockwise from twelve o'clock,
// the largest first, each over the angle its value takes of its
// parent's. Leaves are filled and labelled at their middle, along the
// radius, turned to read from the left.
//
// # Corpus evidence
//
//   - probe, docx: Word draws Grain (40 and 15), Fruit (30 and 20) and Veg
//     (25 and 10) in a plot area 169 pt high as a hole of radius 28.2 pt
//     and two rings 28.2 pt wide, Grain from twelve o'clock to 141.43
//     degrees clockwise, then Fruit and Veg, each parent's leaves the
//     largest first; each sector filled and outlined 0.75 pt in its
//     top-level parent's colour; the leaves' value labels at radius 70.5
//     pt, in the middle of their sectors, in 254,254,254, their text
//     along the radius, those on the left half reading inward.
//   - litho/docx: Word draws the labels of charts-chartex's sunburst,
//     whose chart style fills nothing, in 178,178,178 at the same places.
func (rd *renderer) sunburst(g *Group, s *Series, tops []*node, depth int, plot typeset.Rect) {
	cx, cy := plot.X+float64(plot.Width/2), plot.Y+float64(plot.Height/2)
	r := math.Min(plot.Width, plot.Height) / 2
	if !above(r, 1) || depth < 1 {
		return
	}
	ring := r / float64(depth+1)
	var labels []func()
	var lay func(ns []*node, level int, a0, span, total float64)
	lay = func(ns []*node, level int, a0, span, total float64) {
		if rd.ctx.Err() != nil || !numeric.PositiveFinite(total) {
			return
		}
		r0 := float64(float64(level+1) * ring)
		at := a0
		for _, n := range largestFirst(ns) {
			sweep := float64(span*n.value) / total
			fill := rd.hierarchyFill(s, n.top, len(tops))
			// Angles run counter-clockwise from three o'clock, so a
			// clockwise sweep from twelve o'clock goes down from 90
			// degrees.
			b0 := float64(90-at) * math.Pi / 180
			b1 := float64(90-at-sweep) * math.Pi / 180
			rd.paint(sector(cx, cy, r0, r0+ring, b1, b0), fill, hierarchyLine(s, fill))
			if len(n.children) > 0 {
				lay(n.children, level+1, at, sweep, n.value)
			} else if lb := rd.labelsAt(g, s, n.point); lb.Any() {
				mid := float64(at + float64(sweep/2))
				rm := float64(r0 + float64(ring/2))
				ink := onFillLabelInk
				if fill == nil || fill.None {
					ink = unfilledLabelInk
				}
				labels = append(labels, func() { rd.sunburstLabel(g, s, n, lb, ink, cx, cy, rm, mid) })
			}
			at += sweep
		}
	}
	total := 0.0
	for _, t := range tops {
		total += t.value
	}
	lay(tops, 0, 0, 360, total)
	for _, l := range labels {
		l()
	}
}

// sunburstLabel draws the label lb of leaf n of a sunburst at radius rm
// from (cx, cy) and mid degrees clockwise from twelve o'clock: centred
// there in ink, along the radius, turned half a turn where it would read
// leftward.
func (rd *renderer) sunburstLabel(g *Group, s *Series, n *node, lb Labels, ink typeset.Color, cx, cy, rm, mid float64) {
	v, _ := rd.data(s.Values).number(n.point)
	st := lb.Style
	st.Color = ink
	turn := 90 - mid
	for turn <= -180 {
		turn += 360
	}
	if turn < -90-1e-9 || turn > 90+1e-9 {
		turn += 180
		if turn > 180 {
			turn -= 360
		}
	}
	st.Rotation = turn
	b := rd.measure(rd.labelText(g, s, n.point, v, lb, math.NaN()), st, 0)
	if b == nil {
		return
	}
	a := mid * math.Pi / 180
	x := cx + float64(rm*detmath.Sin(a))
	y := cy - float64(rm*detmath.Cos(a))
	rd.out = append(rd.out, b.draw(x, y, alignCenter)...)
}
