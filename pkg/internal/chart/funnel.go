package chart

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// funnelLabelInk is the colour Office draws the data labels of a funnel's
// bars in when the bars are not filled.
//
// # Corpus evidence
//
//   - litho/docx, litho/pptx: Word and PowerPoint draw the value labels of
//     charts-chartex's and chartex's funnels, whose bars their chart
//     style leaves unfilled, in 178,178,178, where the style gives data
//     labels the text colour, black.
var funnelLabelInk = typeset.Color{R: 178.0 / 255, G: 178.0 / 255, B: 178.0 / 255, A: 1}

// funnel draws a funnel group: per category, from the top down, a bar
// centred across the plot area as wide as its value is of the greatest
// value, the greatest as wide as the plot area, its data label at its
// centre (MS-ODRAWXML, 2.24.4.21 ST_SeriesLayout funnel).
//
// # Corpus evidence
//
//   - libreoffice/pptx: PowerPoint's bars of funnel-pp1 for 4.3, 2.5, 3.5
//     and 4.5 are 558.72, 324.0, 455.04 and 584.64 pt wide, 129.9 pt a
//     unit, centred on 500.4, the widest from the plot area's left edge
//     to its right; 27.36 pt high, 88.56 pt apart at gapWidth 2.19.
//   - litho/docx, litho/pptx: Word and PowerPoint centre the labels of
//     charts-chartex's and chartex's funnels on the plot area whatever
//     their position says.
func (rd *renderer) funnel(g *Group, x, y *dim) []func() {
	sv := rd.stack(g)
	if len(g.Series) == 0 {
		return nil
	}
	top := 0.0
	for j := range sv.n {
		if v := sv.raw[0][j]; !math.IsNaN(v) {
			top = math.Max(top, math.Abs(v))
		}
	}
	if top <= 0 {
		return nil
	}
	s := g.Series[0]
	plot := y.inner
	cx := plot.X + float64(plot.Width/2)
	catW := x.length() / float64(max(x.n, 1))
	h := catW / (1 + math.Max(0, math.Min(g.GapWidth, 500))/100)
	var labels []func()
	for j := 0; j < sv.n && j < x.n; j++ {
		v := sv.raw[0][j]
		if math.IsNaN(v) {
			continue
		}
		w := float64(plot.Width * math.Abs(v) / top)
		cy := x.valuePixel(float64(j))
		r := typeset.Rect{X: cx - float64(w/2), Y: cy - float64(h/2), Width: w, Height: h}
		fill := rd.fillOf(g, s, j)
		rd.paint(rectPath(r).Ops, fill, rd.borderOf(g, s, j))
		lb := rd.labelsAt(g, s, j)
		if !lb.Any() {
			continue
		}
		lb.Placement = LabelCenter
		if fill == nil || fill.None {
			lb.Style.Color = funnelLabelInk
		}
		labels = append(labels, func() { rd.barLabel(g, s, j, v, lb, r, true, false, false) })
	}
	return labels
}
