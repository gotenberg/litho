package chart

import (
	"math"
	"slices"
)

// waterfallSpans returns the start and end of each column of a waterfall
// series of values raw whose points of the indices subtotals are totals:
// a total stands on the axis (a NaN start) and ends at its value, which
// the running total takes; another point starts where the running total
// stands and ends that much further. A missing value draws no column and
// leaves the running total.
func waterfallSpans(raw []float64, subtotals []int) ([]float64, []float64) {
	lo, hi := make([]float64, len(raw)), make([]float64, len(raw))
	run := 0.0
	for j, v := range raw {
		switch {
		case math.IsNaN(v):
			lo[j], hi[j] = math.NaN(), math.NaN()
		case slices.Contains(subtotals, j):
			lo[j], hi[j] = math.NaN(), v
			run = v
		default:
			lo[j], hi[j] = run, run+v
			run += v
		}
	}
	return lo, hi
}

// The kinds of a waterfall's points, the order of their automatic
// colours and legend entries.
const (
	waterfallIncrease = iota
	waterfallDecrease
	waterfallTotal
)

// waterfallNames are the legend's names of the kinds of a waterfall's
// points.
//
// # Corpus evidence
//
//   - litho/docx, litho/xlsx, litho/pptx: Word, Excel and PowerPoint list
//     "Increase", "Decrease" and "Total" in the legend of charts-chartex's
//     and chartex's waterfall, whatever its points.
var waterfallNames = [...]string{"Increase", "Decrease", "Total"}

// waterfallKind returns the kind of point j of series s: a total, else an
// increase or a decrease by the sign of its value.
func (rd *renderer) waterfallKind(s *Series, j int) int {
	if slices.Contains(s.Subtotals, j) {
		return waterfallTotal
	}
	if v, ok := rd.data(s.Values).number(j); ok && v < 0 {
		return waterfallDecrease
	}
	return waterfallIncrease
}

// waterfallFill returns the fill of a waterfall point of kind k: the
// series' own fill, else the automatic colour of index k.
func (rd *renderer) waterfallFill(s *Series, k int) *Paint {
	if s.Fill != nil {
		return s.Fill
	}
	return &Paint{Color: rd.c.AutoColor(k, len(waterfallNames))}
}

// connectors draws the connector lines of a waterfall group: from the end
// of each column, across the gap, to the start of the next column, at the
// running total the column ends on.
//
// # Corpus evidence
//
//   - litho/docx: charts-chartex's waterfall of six columns 40.75 pt wide
//     joins each column's right edge to the next one's left edge, 20.25 pt
//     further, at the running total, in five lines.
func (rd *renderer) connectors(g *Group, x, y *dim) {
	sv := rd.stack(g)
	for i, s := range g.Series {
		if s.Connector == nil {
			continue
		}
		left, right := rd.columnEdges(g, x)
		for j := 0; j+1 < sv.n && j+1 < x.n; j++ {
			v := sv.hi[i][j]
			if math.IsNaN(v) || math.IsNaN(sv.hi[i][j+1]) {
				continue
			}
			p := y.valuePixel(y.sc.clamp(v))
			a, b := x.valuePixel(float64(j))+right, x.valuePixel(float64(j+1))+left
			if x.vertical {
				rd.add(line(p, a, p, b, s.Connector))
			} else {
				rd.add(line(a, p, b, p, s.Connector))
			}
		}
	}
}

// columnEdges returns the offsets of the near and far edge of a column
// of g from its category's centre along x, as [renderer.bars] places the
// one column of a single-series group.
func (rd *renderer) columnEdges(g *Group, x *dim) (float64, float64) {
	gap := math.Max(0, math.Min(g.GapWidth, 500)) / 100
	catW := x.length() / float64(max(x.n, 1))
	if !x.between && x.n > 1 {
		catW = x.length() / float64(x.n-1)
	}
	w := catW / (1 + gap)
	sign := 1.0
	if x.ax != nil && x.ax.Reverse {
		sign = -1
	}
	if x.vertical {
		sign = -sign
	}
	return float64(-sign * w / 2), float64(sign * w / 2)
}
