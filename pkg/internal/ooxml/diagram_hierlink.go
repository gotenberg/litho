package ooxml

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/detmath"
)

// hierLink is a connector of a hierarchy from a root shape to one of its
// children's, drawn in container, in the style of the connector shape
// style.
type hierLink struct {
	container, style, from, to *DiagramShape
	// kind is how the line runs: linkRow down and across to the child's
	// top, turning bend above it (negative for half way); linkSide down
	// from the root's bottom centre and across to the child's near side;
	// linkColumn alike from the root's kid that marks its line.
	kind int
	bend float64
	// across and mirror turn the line with its tree (see [hierTree]);
	// straight draws it from end to end (connRout stra).
	across, mirror, straight bool
}

// The ways a connector of a hierarchy runs, by the container its child
// stands in.
const (
	linkRow = iota
	linkSide
	linkColumn
)

// compOf returns the shape a hierRoot shape shows its point in: its
// first kid that runs no hierarchy algorithm.
func (t *hierTree) compOf(root *DiagramShape) *DiagramShape {
	for _, k := range root.Kids {
		if ka := t.algOf(k); ka == nil || (ka.Alg != "hierChild" && ka.Alg != "hierRoot") {
			return k
		}
	}
	return nil
}

// drawLinks draws the connectors of the hierarchies laid out, once every
// shape has its box, as orgChart1's connectors run (21.4.2.1 conn,
// connRout bend): to a child in a row, from the bottom centre of the
// parent's shape down to the child's top centre, turning bendDist above
// it (begPts bCtr, endPts tCtr, bendPt end); to an assistant or a child of
// two columns, from the same point down to the height of the child's
// middle and across to its side that faces the line (endPts midL midR);
// to a child of a column, alike from the bottom centre of the parent
// shape's kid that holds no text, the line orgChart1 names (srcNode
// rootConnector). Each connector is a group of straight lines in the
// style of a connector shape of the layout.
//
// # Corpus evidence
//
//   - libreoffice/pptx: smartart-org-chart's Office page draws Employee2's
//     connector from Manager's bottom centre down to 17.02 points above
//     Employee2 (bendDist 0.5 of sp, 17.28), across and down, and the
//     assistant's from the same point down to the assistant's middle and
//     left to its right side; smartart-data-follow's column under A2 from
//     the bottom centre of A2's rootConnector, a fifth of A2 wide on its
//     left, to each child's left side.
func (e *engine) drawLinks() {
	for _, l := range e.hierLinks {
		from := l.from
		if l.kind == linkColumn && !l.across {
			for _, k := range from.Kids {
				if len(k.Paragraphs) == 0 && k.W > 0 && k.W < from.W {
					from = k
					break
				}
			}
		}
		fx, fy, fw, fh, ok1 := absBox(from)
		tx, ty, tw, th, ok2 := absBox(l.to)
		px, py, ok3 := absOrigin(l.container)
		if !ok1 || !ok2 || !ok3 {
			continue
		}
		// The line is found in the tree's own frame, where it grows down,
		// and turned back.
		fx, fy, fw, fh = l.toTree(fx, fy, fw, fh)
		tx, ty, tw, th = l.toTree(tx, ty, tw, th)
		bx, by := fx+fw/2, fy+fh
		var pts [][2]int64
		switch {
		case l.kind == linkSide || l.kind == linkColumn:
			ey, ex := ty+th/2, tx
			if tx+tw/2 < bx {
				ex = tx + tw
			}
			pts = [][2]int64{{bx, by}, {bx, ey}, {ex, ey}}
		case l.straight || tx+tw/2 == bx:
			pts = [][2]int64{{bx, by}, {tx + tw/2, ty}}
		default:
			ex := tx + tw/2
			turn := (by + ty) / 2
			if l.bend >= 0 {
				turn = ty - trunc(l.bend)
			}
			pts = [][2]int64{{bx, by}, {bx, turn}, {ex, turn}, {ex, ty}}
		}
		for i, p := range pts {
			pts[i][0], pts[i][1] = l.fromTree(p[0], p[1])
		}
		l.container.Kids = append(l.container.Kids, linkLines(l.style, l.container, pts, px, py))
	}
}

// toTree returns a box of the diagram in the frame its tree is laid out
// in: mirrored for a tree that grows leftward, turned for one that grows
// across.
func (l hierLink) toTree(x, y, w, h int64) (int64, int64, int64, int64) {
	if l.mirror {
		x = -x - w
	}
	if l.across {
		x, y, w, h = y, x, h, w
	}
	return x, y, w, h
}

// fromTree returns a point of the tree's frame in the diagram's.
func (l hierLink) fromTree(x, y int64) (int64, int64) {
	if l.across {
		x, y = y, x
	}
	if l.mirror {
		x = -x
	}
	return x, y
}

// linkLines returns a group of the straight lines through pts, absolute
// EMU of the diagram, in the style of the connector shape style, for a
// parent at px, py.
func linkLines(style, parent *DiagramShape, pts [][2]int64, px, py int64) *DiagramShape {
	x0, y0, x1, y1 := pts[0][0], pts[0][1], pts[0][0], pts[0][1]
	for _, p := range pts {
		x0, y0, x1, y1 = min(x0, p[0]), min(y0, p[1]), max(x1, p[0]), max(y1, p[1])
	}
	g := &DiagramShape{Name: style.Name, Group: true, parent: parent, zoff: style.zoff, X: x0 - px, Y: y0 - py, W: x1 - x0, H: y1 - y0}
	var line *Element
	if style.tmpl != nil {
		line = style.tmpl.line
	}
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		if a == b {
			continue
		}
		k := &DiagramShape{
			Name: style.Name, DataType: style.DataType, tmpl: &shapeTemplate{preset: "line", line: line}, parent: g,
			Style: style.Style, StyleColor: style.StyleColor, LineSpPr: style.LineSpPr,
			X: min(a[0], b[0]) - x0, Y: min(a[1], b[1]) - y0, W: max(a[0], b[0]) - min(a[0], b[0]), H: max(a[1], b[1]) - min(a[1], b[1]),
		}
		if dx, dy := b[0]-a[0], b[1]-a[1]; (dx < 0) != (dy < 0) && dx != 0 && dy != 0 {
			// A line rising to the right: the preset runs from the top
			// left corner down, so the line is drawn level, as long as the
			// segment, and turned about its middle.
			l := detmath.Hypot(float64(dx), float64(dy))
			deg := float64(detmath.Atan2(float64(dy), float64(dx)) * 180 / math.Pi)
			cx, cy := float64(float64(a[0]+b[0])/2), float64(float64(a[1]+b[1])/2)
			k.X, k.Y, k.W, k.H = trunc(cx-float64(l/2))-x0, trunc(cy)-y0, trunc(l), 0
			k.Rot = trunc(math.Round(float64(deg * perDegree)))
		}
		g.Kids = append(g.Kids, k)
	}
	return g
}

// absBox returns a shape's box in the diagram's coordinates.
func absBox(s *DiagramShape) (x, y, w, h int64, ok bool) {
	x, y, ok = absOrigin(s)
	return x, y, s.W, s.H, ok
}

// absOrigin returns a shape's top left corner in the diagram's coordinates,
// false when its chain of parents is longer than a diagram nests.
func absOrigin(s *DiagramShape) (int64, int64, bool) {
	var x, y int64
	for i := 0; s != nil; i++ {
		if i >= maxDiagramDepth*4 {
			return 0, 0, false
		}
		x, y = x+s.X, y+s.Y
		s = s.parent
	}
	return x, y, true
}
