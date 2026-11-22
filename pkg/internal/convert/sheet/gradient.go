package sheet

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// gradientPaths returns the paths that paint the gradient g over the cell
// box x, y, w, h, as Excel prints a gradient fill (ECMA-376 Part 1,
// 18.8.24): the gradient laid out on the cell taken as a unit square,
// colours blended in sRGB. A linear gradient runs along its degree in
// that square, from the corner it leaves to the corner it reaches, its
// lines of one colour joining the cell's corners at 45 degrees. A path
// gradient takes its first colour in the inner rectangle and reaches its
// last at the cell's edges, each point coloured by the larger of its
// distances across and down from the inner rectangle, as a share of the
// room on that side; it is painted as four shaded trapezoids around the
// inner rectangle.
//
// # Corpus evidence
//
//   - litho/xlsx: fills_gradient, cells of 124.22 by 38.76 points and a
//     merge of 248.33 by 53.42, prints a 45-degree gradient from white to
//     4472C4 with its top-right and bottom-left corners alike at
//     (161, 184, 226), the halfway blend; 135 degrees from 70AD47 at the
//     top-right to FFC000 at the bottom-left; a merged cell's gradient
//     over the whole merge; a path gradient whose inner rectangle is the
//     centre white there and 4472C4 at the edges, one whose rectangle is
//     the top-left corner red at the top-right and the bottom-left and
//     halfway at the centre, one whose rectangle is the bottom-right
//     corner yellow there and purple at the top-left.
func gradientPaths(x, y, w, h float64, g *Gradient) []typeset.Fragment {
	if w <= 0 || h <= 0 || len(g.Stops) < 2 {
		return nil
	}
	stops := make([]typeset.ColorStop, len(g.Stops))
	for i, s := range g.Stops {
		stops[i] = typeset.ColorStop{Offset: s.Position, Color: colorOf(s.Color)}
	}
	unit := typeset.Matrix{A: w, D: h, E: x, F: y}
	at := func(u, v float64) typeset.Point {
		return typeset.Point{X: x + float64(u*w), Y: y + float64(v*h)}
	}
	poly := func(pts ...[2]float64) []typeset.PathOp {
		ops := make([]typeset.PathOp, 0, len(pts)+1)
		for i, p := range pts {
			k := typeset.LineTo
			if i == 0 {
				k = typeset.MoveTo
			}
			ops = append(ops, typeset.PathOp{Kind: k, P: [3]typeset.Point{at(p[0], p[1])}})
		}
		return append(ops, typeset.PathOp{Kind: typeset.ClosePath})
	}
	shaded := func(ops []typeset.PathOp, from, to [2]float64) *typeset.Path {
		return &typeset.Path{Ops: ops, Fill: &typeset.Fill{Shading: &typeset.Shading{
			Kind: typeset.Axial, From: typeset.Point{X: from[0], Y: from[1]}, To: typeset.Point{X: to[0], Y: to[1]},
			Stops: stops, Transform: unit,
		}}}
	}
	if !g.Path {
		a := g.Degree * math.Pi / 180
		dx, dy := detmath.Cos(a), detmath.Sin(a)
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, c := range [4][2]float64{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
			p := float64(c[0]*dx) + float64(c[1]*dy)
			lo, hi = min(lo, p), max(hi, p)
		}
		if !numeric.Finite(hi-lo) || hi-lo <= epsilon {
			return nil
		}
		return []typeset.Fragment{shaded(rectOps(x, y, w, h), [2]float64{lo * dx, lo * dy}, [2]float64{hi * dx, hi * dy})}
	}
	l, r, t, b := g.Left, g.Right, g.Top, g.Bottom
	// The top and bottom trapezoids, painted last, reach gradientSeam
	// points past their slanted sides over the left and right ones, where
	// both shadings give nearly one colour, so that no seam of the page's
	// colour shows between them.
	su, sv := min(gradientSeam/w, 1), min(gradientSeam/h, 1)
	var out []typeset.Fragment
	if r > l+epsilon && b > t+epsilon {
		out = append(out, &typeset.Path{Ops: poly([2]float64{l, t}, [2]float64{r, t}, [2]float64{r, b}, [2]float64{l, b}), Fill: &typeset.Fill{Color: stops[0].Color}})
	}
	if l > epsilon {
		out = append(out, shaded(poly([2]float64{0, 0}, [2]float64{l, t}, [2]float64{l, b}, [2]float64{0, 1}), [2]float64{l, 0}, [2]float64{0, 0}))
	}
	if r < 1-epsilon {
		out = append(out, shaded(poly([2]float64{1, 0}, [2]float64{r, t}, [2]float64{r, b}, [2]float64{1, 1}), [2]float64{r, 0}, [2]float64{1, 0}))
	}
	if t > epsilon {
		out = append(out, shaded(poly([2]float64{0, 0}, [2]float64{1, 0}, [2]float64{min(r+su, 1), min(t+sv, b)}, [2]float64{max(l-su, 0), min(t+sv, b)}), [2]float64{0, t}, [2]float64{0, 0}))
	}
	if b < 1-epsilon {
		out = append(out, shaded(poly([2]float64{0, 1}, [2]float64{1, 1}, [2]float64{min(r+su, 1), max(b-sv, t)}, [2]float64{max(l-su, 0), max(b-sv, t)}), [2]float64{0, b}, [2]float64{0, 1}))
	}
	return out
}

// gradientSeam is how far, in points, the top and bottom trapezoids of a
// path gradient reach over the side ones ([gradientPaths]).
const gradientSeam = 0.5
