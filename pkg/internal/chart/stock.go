package chart

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// maxUpDownGap caps the gap width of up-down bars, in percent of a bar's
// width: ECMA-376's ST_GapAmount allows at most 500 (21.2.3.13).
const maxUpDownGap = 500

// highLow draws the high-low lines of the line group g against x and y:
// at each category where two or more series have a value, one line from
// the lowest value to the highest (ECMA-376 Part 1, 21.2.2.80
// hiLowLines). Office draws them under the up-down bars and the series.
//
// # Corpus evidence
//
//   - litho/pptx: chart-stock's high-low lines run from the low to the
//     high of each day at the category's middle in PowerPoint, in the
//     line the file states, under its up-down bars.
//   - litho/xlsx: charts_radar_stock_surface's the same in Excel.
func (rd *renderer) highLow(g *Group, x, y *dim) {
	st := g.HighLow.compose()
	if st == nil {
		return
	}
	sv := rd.stack(g)
	var ops []typeset.PathOp
	for j := 0; j < sv.n && j < x.n; j++ {
		lo, hi, k := math.Inf(1), math.Inf(-1), 0
		for i := range sv.raw {
			if v := sv.raw[i][j]; !math.IsNaN(v) {
				lo, hi, k = math.Min(lo, v), math.Max(hi, v), k+1
			}
		}
		if k < 2 {
			continue
		}
		p0 := point(x, y, float64(j), y.sc.clamp(lo))
		p1 := point(x, y, float64(j), y.sc.clamp(hi))
		ops = append(ops,
			typeset.PathOp{Kind: typeset.MoveTo, P: [3]typeset.Point{p0}},
			typeset.PathOp{Kind: typeset.LineTo, P: [3]typeset.Point{p1}})
	}
	if len(ops) > 0 {
		rd.out = append(rd.out, &typeset.Path{Ops: ops, Stroke: st})
	}
}

// upDown draws the up-down bars of the line group g against x and y: at
// each category where the first and the last series have a value, a bar
// from the first value to the last, as wide as one bar of a bar group of
// the bars' gap width, in the up bars' frame when the last value is not
// below the first and in the down bars' otherwise (ECMA-376 Part 1,
// 21.2.2.218 upDownBars).
//
// # Corpus evidence
//
//   - litho/pptx: chart-stock's bars, gap width 150, are 20.16 to 20.88 pt
//     wide on categories 51.03 pt apart in PowerPoint, the width one bar
//     of a bar group with that gap takes (20.41 pt), centred on the
//     category; green up from open to close, red down.
//   - litho/xlsx: charts_radar_stock_surface's start 8.18 pt left of the
//     middle of categories 40.88 pt apart in Excel, half of 16.35 pt.
func (rd *renderer) upDown(g *Group, x, y *dim) {
	ud := g.UpDown
	if ud == nil || len(g.Series) < 2 {
		return
	}
	sv := rd.stack(g)
	first, last := sv.raw[0], sv.raw[len(sv.raw)-1]
	gap := math.Max(0, math.Min(ud.GapWidth, maxUpDownGap)) / 100
	catW := x.length() / float64(max(x.n, 1))
	if !x.between && x.n > 1 {
		catW = x.length() / float64(x.n-1)
	}
	barW := catW / (1 + gap)
	for j := 0; j < sv.n && j < x.n; j++ {
		open, cls := first[j], last[j]
		if math.IsNaN(open) || math.IsNaN(cls) {
			continue
		}
		c := x.valuePixel(float64(j))
		p0 := c - float64(barW/2)
		v0, v1 := y.valuePixel(y.sc.clamp(open)), y.valuePixel(y.sc.clamp(cls))
		var r typeset.Rect
		if x.vertical {
			r = typeset.Rect{X: math.Min(v0, v1), Y: p0, Width: math.Abs(v1 - v0), Height: barW}
		} else {
			r = typeset.Rect{X: p0, Y: math.Min(v0, v1), Width: barW, Height: math.Abs(v1 - v0)}
		}
		f := ud.Up
		if cls < open {
			f = ud.Down
		}
		rd.paint(rectPath(r).Ops, f.Fill, f.Border)
	}
}
