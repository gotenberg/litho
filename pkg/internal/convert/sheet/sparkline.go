package sheet

import (
	"context"
	"math"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// SparkKind is the kind of a sparkline group (MS-XLSX, 2.6.92
// ST_SparklineType; ODF calcext:type).
type SparkKind uint8

// The sparkline kinds.
const (
	// SparkLine joins the points with a line.
	SparkLine SparkKind = iota
	// SparkColumn draws a column a point from the axis.
	SparkColumn
	// SparkWinLoss draws a column of the same height a point, up for a
	// positive value, down for a negative one, none for 0 (stacked).
	SparkWinLoss
)

// SparkEmpty is how a sparkline shows an empty cell (MS-XLSX, 2.6.91
// ST_DispBlanksAs).
type SparkEmpty uint8

// The ways to show an empty cell.
const (
	// SparkGap leaves the point out, breaking the line.
	SparkGap SparkEmpty = iota
	// SparkZero shows the point as 0.
	SparkZero
	// SparkSpan joins the points on either side.
	SparkSpan
)

// SparkAxis is how a group sets an end of its value axis (MS-XLSX,
// 2.6.93 ST_SparklineAxisMinMax).
type SparkAxis uint8

// The value axis ends.
const (
	// SparkIndividual takes each sparkline's own minimum or maximum.
	SparkIndividual SparkAxis = iota
	// SparkGroupAxis takes the group's.
	SparkGroupAxis
	// SparkCustom takes the group's manual value.
	SparkCustom
)

// SparkGroup is a group of sparklines that share their look (MS-XLSX,
// 2.6.90 CT_SparklineGroup; ODF calcext:sparkline-group): each draws, in
// the cell its drawing anchors, the values its Values returns.
type SparkGroup struct {
	Kind  SparkKind
	Empty SparkEmpty
	// LineWeight is a line sparkline's width in points.
	LineWeight float64
	// The colours of the series, the negative points, the axis, the
	// markers and the first, last, highest and lowest points.
	Series, Negative, Axis, Markers, First, Last, High, Low typeset.Color
	// Which points take their own colour, whether the line marks every
	// point, whether the horizontal axis is drawn where 0 is.
	ShowNegative, ShowMarkers, ShowFirst, ShowLast, ShowHigh, ShowLow, ShowAxis bool
	// RightToLeft runs the points from the right.
	RightToLeft bool
	// MinAxis and MaxAxis set the value axis ends, ManualMin and
	// ManualMax the custom ones.
	MinAxis, MaxAxis     SparkAxis
	ManualMin, ManualMax float64
	// Count is the number of sparklines; Values returns the values of
	// sparkline i, each a number or empty, resolved when it is drawn.
	Count  int
	Values func(i int) []chart.Value

	once     sync.Once
	glo, ghi float64
	gok      bool
}

// The measures of a sparkline in its cell, in points.
//
// # Corpus evidence
//
//   - litho/xlsx: sparklines prints each sparkline as a picture of its
//     cell: twelve columns of a 96.4 point cell on a pitch of (width -
//     2 x 1.44) / 12, each 0.7 of the pitch, centred in its slot; the
//     line's points at the slots' centres; the values spread over the
//     cell's height less 2.5 points at the top and the bottom; a win/loss
//     column from the middle to that inset; a line's markers diamonds
//     3.6 points wide, its negative points marked in the negative colour.
const (
	sparkInsetX   = 1.44
	sparkInsetY   = 2.5
	sparkBarShare = 0.7
	sparkMarker   = 3.6
	sparkAxisLine = 0.75
)

// sparkNumbers returns the values of v as numbers, NaN for an empty one
// shown as a gap or spanned, 0 for an empty one shown as zero.
func (g *SparkGroup) sparkNumbers(v []chart.Value) []float64 {
	out := make([]float64, len(v))
	for i, x := range v {
		switch {
		case x.Kind == chart.Number && numeric.Finite(x.Number):
			out[i] = x.Number
		case g.Empty == SparkZero:
			out[i] = 0
		default:
			out[i] = math.NaN()
		}
	}
	return out
}

// extent returns the least and greatest values of v, false for none.
func extent(v []float64) (float64, float64, bool) {
	lo, hi, ok := 0.0, 0.0, false
	for _, x := range v {
		if math.IsNaN(x) {
			continue
		}
		if !ok {
			lo, hi, ok = x, x, true
		}
		lo, hi = math.Min(lo, x), math.Max(hi, x)
	}
	return lo, hi, ok
}

// groupExtent returns the least and greatest values of the whole group,
// computed once.
func (g *SparkGroup) groupExtent() (float64, float64, bool) {
	g.once.Do(func() {
		first := true
		for i := range g.Count {
			lo, hi, ok := extent(g.sparkNumbers(g.Values(i)))
			if !ok {
				continue
			}
			if first {
				g.glo, g.ghi, first = lo, hi, false
			}
			g.glo, g.ghi = math.Min(g.glo, lo), math.Max(g.ghi, hi)
		}
		g.gok = !first
	})
	return g.glo, g.ghi, g.gok
}

// Picture returns the vector picture of sparkline i.
func (g *SparkGroup) Picture(i int) *layout.Picture {
	pic := &layout.Picture{Width: 1, Height: 1}
	pic.Vector = func(_ context.Context, box typeset.Rect) []typeset.Fragment {
		if g.Values == nil {
			return nil
		}
		return g.paint(g.sparkNumbers(g.Values(i)), box)
	}
	return pic
}

// paint draws the values v in box.
func (g *SparkGroup) paint(v []float64, box typeset.Rect) []typeset.Fragment {
	n := len(v)
	lo, hi, ok := extent(v)
	if n == 0 || !ok {
		return nil
	}
	if g.MinAxis == SparkGroupAxis || g.MaxAxis == SparkGroupAxis {
		glo, ghi, _ := g.groupExtent()
		if g.MinAxis == SparkGroupAxis {
			lo = glo
		}
		if g.MaxAxis == SparkGroupAxis {
			hi = ghi
		}
	}
	if g.MinAxis == SparkCustom {
		lo = g.ManualMin
	}
	if g.MaxAxis == SparkCustom {
		hi = g.ManualMax
	}
	if g.RightToLeft {
		r := make([]float64, n)
		for i := range v {
			r[n-1-i] = v[i]
		}
		v = r
	}
	top, bottom := box.Y+sparkInsetY, box.Y+box.Height-sparkInsetY
	pitch := (box.Width - 2*sparkInsetX) / float64(n)
	cx := func(i int) float64 { return box.X + sparkInsetX + float64(pitch*(float64(i)+0.5)) }
	y := func(x float64) float64 {
		if hi <= lo {
			return float64((top + bottom) / 2)
		}
		f := max(0, min(1, (x-lo)/(hi-lo)))
		return bottom - float64(f*(bottom-top))
	}
	hiIdx, loIdx, firstIdx, lastIdx := -1, -1, -1, -1
	for i, x := range v {
		if math.IsNaN(x) {
			continue
		}
		if firstIdx < 0 {
			firstIdx = i
		}
		lastIdx = i
		if hiIdx < 0 || x > v[hiIdx] {
			hiIdx = i
		}
		if loIdx < 0 || x < v[loIdx] {
			loIdx = i
		}
	}
	// colorOf returns the colour of point i and whether it is marked: the
	// series' or the markers', the negative one, then the high, low,
	// first and last ones, each over the one before; a line marks a
	// point of its own colour.
	colorOf := func(i int, base typeset.Color, marked bool) (typeset.Color, bool) {
		c := base
		if g.ShowNegative && v[i] < 0 {
			c, marked = g.Negative, true
		}
		for _, s := range []struct {
			on  bool
			idx int
			c   typeset.Color
		}{{g.ShowHigh, hiIdx, g.High}, {g.ShowLow, loIdx, g.Low}, {g.ShowFirst, firstIdx, g.First}, {g.ShowLast, lastIdx, g.Last}} {
			if s.on && s.idx == i {
				c, marked = s.c, true
			}
		}
		return c, marked
	}
	var out []typeset.Fragment
	axisY := y(0)
	if g.Kind == SparkWinLoss {
		axisY = float64((top + bottom) / 2)
	}
	if g.ShowAxis && (g.Kind == SparkWinLoss || lo < 0 && hi > 0) {
		out = append(out, &typeset.Path{
			Ops:    []typeset.PathOp{sparkPt(typeset.MoveTo, box.X, axisY), sparkPt(typeset.LineTo, box.X+box.Width, axisY)},
			Stroke: &typeset.Stroke{Color: g.Axis, Width: sparkAxisLine},
		})
	}
	switch g.Kind {
	case SparkColumn, SparkWinLoss:
		base := y(max(lo, min(hi, 0)))
		w := float64(pitch * sparkBarShare)
		for i, x := range v {
			if math.IsNaN(x) {
				continue
			}
			y0, y1 := base, y(x)
			if g.Kind == SparkWinLoss {
				switch {
				case x > 0:
					y0, y1 = axisY, top
				case x < 0:
					y0, y1 = axisY, bottom
				default:
					continue
				}
			}
			c, _ := colorOf(i, g.Series, false)
			r := typeset.Rect{X: cx(i) - float64(w/2), Y: math.Min(y0, y1), Width: w, Height: math.Abs(y1 - y0)}
			out = append(out, &typeset.Path{Ops: sparkRect(r), Fill: &typeset.Fill{Color: c}})
		}
	case SparkLine:
		var ops []typeset.PathOp
		open := false
		for i, x := range v {
			if math.IsNaN(x) {
				if g.Empty == SparkGap {
					open = false
				}
				continue
			}
			k := typeset.LineTo
			if !open {
				k = typeset.MoveTo
			}
			ops = append(ops, sparkPt(k, cx(i), y(x)))
			open = true
		}
		if len(ops) > 0 {
			out = append(out, &typeset.Path{Ops: ops, Stroke: &typeset.Stroke{Color: g.Series, Width: max(g.LineWeight, 0.25), Join: typeset.RoundJoin, Cap: typeset.RoundCap}})
		}
		for i, x := range v {
			if math.IsNaN(x) {
				continue
			}
			c, marked := colorOf(i, g.Markers, g.ShowMarkers)
			if !marked {
				continue
			}
			px, py, h := cx(i), y(x), float64(sparkMarker/2)
			diamond := []typeset.PathOp{
				sparkPt(typeset.MoveTo, px, py-h),
				sparkPt(typeset.LineTo, px+h, py),
				sparkPt(typeset.LineTo, px, py+h),
				sparkPt(typeset.LineTo, px-h, py),
				{Kind: typeset.ClosePath},
			}
			out = append(out, &typeset.Path{Ops: diamond, Fill: &typeset.Fill{Color: c}})
		}
	}
	return out
}

// sparkPt returns a move or a line to (x, y).
func sparkPt(k typeset.OpKind, x, y float64) typeset.PathOp {
	return typeset.PathOp{Kind: k, P: [3]typeset.Point{{X: x, Y: y}}}
}

// sparkRect returns the outline of r.
func sparkRect(r typeset.Rect) []typeset.PathOp {
	return []typeset.PathOp{
		sparkPt(typeset.MoveTo, r.X, r.Y), sparkPt(typeset.LineTo, r.X+r.Width, r.Y),
		sparkPt(typeset.LineTo, r.X+r.Width, r.Y+r.Height), sparkPt(typeset.LineTo, r.X, r.Y+r.Height),
		{Kind: typeset.ClosePath},
	}
}
