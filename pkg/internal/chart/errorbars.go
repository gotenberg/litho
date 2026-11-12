package chart

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// errorCap is half the length of the line across an error bar's end: 100
// hundredths of a millimetre.
//
// # Corpus evidence
//
//   - libreoffice/ods: chart2/error_bar.ods prints its caps 5.67 points long.
const errorCap = 100 * mm100

// errorStats is what the length of an error bar reads of its whole
// series: the largest magnitude, and the count, sum and sum of squares of
// the values.
type errorStats struct {
	max, n, sum, sq float64
}

// seriesErrorStats returns the statistics bars e read of the series vals,
// once for all its points.
func seriesErrorStats(e ErrorBars, vals Data) errorStats {
	var st errorStats
	switch e.Kind {
	case ErrorMargin:
		for i := range min(vals.Len(), MaxPoints) {
			if x, ok := vals.number(i); ok {
				st.max = math.Max(st.max, math.Abs(x))
			}
		}
	case ErrorStdDev, ErrorStdErr, ErrorVariance:
		for i := range min(vals.Len(), MaxPoints) {
			if x, ok := vals.number(i); ok {
				st.n, st.sum, st.sq = st.n+1, st.sum+x, st.sq+x*x
			}
		}
	case ErrorFixed, ErrorPercent, ErrorCustom:
	}
	return st
}

// errorAmounts returns the error above and below point j of a series of
// vals for bars e, st the series' statistics: chart2's
// VSeriesPlotter::getErrorBarLength, NaN for none.
func (rd *renderer) errorAmounts(e ErrorBars, vals Data, st errorStats, j int) (float64, float64) {
	v, ok := vals.number(j)
	if !ok {
		return math.NaN(), math.NaN()
	}
	switch e.Kind {
	case ErrorFixed:
		return e.Plus, e.Minus
	case ErrorPercent:
		a := math.Abs(v) * e.Plus / 100
		return a, a
	case ErrorMargin:
		a := st.max * e.Plus / 100
		return a, a
	case ErrorCustom:
		up, down := math.NaN(), math.NaN()
		if x, ok := rd.data(e.PlusData).number(j); ok {
			up = x
		}
		if x, ok := rd.data(e.MinusData).number(j); ok {
			down = x
		}
		return up, down
	case ErrorStdDev, ErrorStdErr, ErrorVariance:
		n, sum, sq := st.n, st.sum, st.sq
		if n < 2 {
			return math.NaN(), math.NaN()
		}
		variance := (sq - sum*sum/n) / (n - 1)
		a := math.Sqrt(math.Max(0, variance))
		switch e.Kind {
		case ErrorStdErr:
			a /= math.Sqrt(n)
		case ErrorVariance:
			a = variance
		case ErrorFixed, ErrorPercent, ErrorMargin, ErrorCustom, ErrorStdDev:
		}
		a *= e.Plus
		return a, a
	}
	return math.NaN(), math.NaN()
}

// errorBars draws the error bars of series s of g against x and y, its
// points at the category or x value of each index and their value.
func (rd *renderer) errorBars(g *Group, s *Series, x, y *dim, at func(j int) (float64, float64, bool)) {
	for _, e := range s.ErrorBars {
		data := s.Values
		if e.X {
			data = s.Categories
		}
		data = rd.data(data)
		st := e.Line
		if st == nil {
			st = &Stroke{Color: compose.Black}
		}
		cs := st.compose()
		if cs == nil {
			continue
		}
		stats := seriesErrorStats(e, data)
		for j := range min(rd.data(s.Values).Len(), MaxPoints) {
			cx, cy, ok := at(j)
			if !ok {
				continue
			}
			up, down := rd.errorAmounts(e, data, stats, j)
			pos := func(xv, yv float64) compose.Point {
				if g.Kind == Scatter || g.Kind == Bubble {
					return compose.Point{X: x.valuePixel(xv), Y: y.valuePixel(yv)}
				}
				return point(x, y, xv, yv)
			}
			c := pos(cx, cy)
			var ends []compose.Point
			if e.Up && !math.IsNaN(up) {
				if e.X {
					ends = append(ends, pos(cx+up, cy))
				} else {
					ends = append(ends, pos(cx, cy+up))
				}
			}
			if e.Down && !math.IsNaN(down) {
				if e.X {
					ends = append(ends, pos(cx-down, cy))
				} else {
					ends = append(ends, pos(cx, cy-down))
				}
			}
			for _, p := range ends {
				rd.out = append(rd.out, &compose.Path{Ops: polyline([]compose.Point{c, p}, false), Stroke: cs})
				if e.NoCap {
					continue
				}
				// The cap runs across the bar.
				dx, dy := p.X-c.X, p.Y-c.Y
				l := math.Hypot(dx, dy)
				if l == 0 {
					continue
				}
				nx, ny := -dy/l*errorCap, dx/l*errorCap
				rd.out = append(rd.out, &compose.Path{Ops: polyline([]compose.Point{{X: p.X - nx, Y: p.Y - ny}, {X: p.X + nx, Y: p.Y + ny}}, false), Stroke: cs})
			}
		}
	}
}

// groupErrorBars draws the error bars of g's series: at each point's
// plotted value, the top of its stack in a stacked group.
func (rd *renderer) groupErrorBars(g *Group, x, y *dim) {
	any := false
	for _, s := range g.Series {
		any = any || len(s.ErrorBars) > 0
	}
	if !any {
		return
	}
	if g.Kind == Scatter || g.Kind == Bubble {
		for _, s := range g.Series {
			cats, vals := rd.data(s.Categories), rd.data(s.Values)
			rd.errorBars(g, s, x, y, func(j int) (float64, float64, bool) {
				yv, ok := vals.number(j)
				if !ok {
					return 0, 0, false
				}
				xv, ok := xValue(cats, j)
				return xv, yv, ok
			})
		}
		return
	}
	sv := rd.stack(g)
	for i, s := range g.Series {
		rd.errorBars(g, s, x, y, func(j int) (float64, float64, bool) {
			if i >= len(sv.hi) || j >= len(sv.hi[i]) || j >= x.n || math.IsNaN(sv.hi[i][j]) {
				return 0, 0, false
			}
			return float64(j), sv.hi[i][j], true
		})
	}
}
