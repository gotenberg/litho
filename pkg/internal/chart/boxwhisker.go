package chart

import (
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// Box is how a [BoxWhisker] series draws the box of each category
// (MS-ODRAWXML, 2.24.3.85 CT_Statistics and 2.24.3.83
// CT_SeriesElementVisibilities).
type Box struct {
	// Exclusive takes the quartiles at the positions p(n+1) of the sorted
	// values, else at p(n-1)+1, both from 1 and interpolated
	// (2.24.4.19 ST_QuartileMethod exclusive and inclusive).
	Exclusive bool
	// Mean marks each box's mean with a cross; MeanLine joins the means
	// of the categories.
	Mean, MeanLine bool
	// Outliers marks the values past the whiskers; Inner the values
	// within them.
	Outliers, Inner bool
	// Line draws the whiskers, the quartile and median lines, the mean's
	// cross and line, nil for none.
	Line *Stroke
}

// boxCapShare is the width of a whisker's cap, and boxGapShare the space
// between the boxes of one category, as shares of a box's width;
// boxMeanSize is the width of the cross on a box's mean, in points.
//
// # Corpus evidence
//
//   - litho/docx: Word's caps of charts-chartex's boxes, 123 pt wide, are
//     37 pt wide; litho/pptx: PowerPoint's of chartex's, 140.4 pt wide,
//     42.48; libreoffice/xlsx: Excel's of boxWhisker, 56.28, 16.92.
//   - libreoffice/xlsx: Excel puts boxWhisker's three boxes of 56.28 pt
//     6.0 pt apart, box-whisker-gapwidth's of 32.28 pt 3.72 apart.
//   - litho/docx, libreoffice/xlsx: the cross on the mean is 6.0 pt wide.
const (
	boxCapShare = 0.3
	boxGapShare = 0.11
	boxMeanSize = 6.0
)

// maxBoxCategories caps the categories of a box and whisker series.
// Anti-DoS guard, not spec-mandated: a category is a box, and a series
// keeps at most MaxPoints values.
const maxBoxCategories = MaxPoints

// boxStats are the statistics a box draws.
type boxStats struct {
	q1, median, q3, mean float64
	// low and high are the whiskers' ends: the least and greatest values
	// within 1.5 interquartile ranges of the box.
	low, high float64
	// outliers are the values past the whiskers, inner the others.
	outliers, inner []float64
}

// quartile returns the quantile p of the sorted values v, at position
// p(n+1) when exclusive, p(n-1)+1 otherwise, both counted from 1, held
// to the values' range and interpolated between neighbours.
func quartile(v []float64, p float64, exclusive bool) float64 {
	n := float64(len(v))
	pos := float64(p*(n-1)) + 1
	if exclusive {
		pos = float64(p * (n + 1))
	}
	pos = math.Max(1, math.Min(pos, n))
	i := math.Floor(pos)
	lo := v[int(i)-1] // fits int: i is at most len(v)
	if i >= n {
		return lo
	}
	hi := v[int(i)] // fits int: i is under len(v)
	return lo + float64((pos-i)*(hi-lo))
}

// statsOf returns the box statistics of the values v, sorted ascending,
// not empty: quartiles by the method, the median, the mean, and the
// whiskers at the farthest values within 1.5 interquartile ranges of the
// box (Tukey's fences).
//
// # Corpus evidence
//
//   - litho/docx, litho/pptx: Word's and PowerPoint's boxes of
//     charts-chartex and chartex, exclusive, put the quartiles, median,
//     mean and whiskers at 5.25, 7, 8.75, 8.125, 3 and 9 for
//     3, 5, 6, 7, 7, 8, 9, 20, leaving 20 out, and at 20.25 and 20 for
//     the low quartile and whisker of 5, 20, 21, 23, 24, 26, 27, 30.
func statsOf(v []float64, exclusive bool) boxStats {
	s := boxStats{q1: quartile(v, 0.25, exclusive), median: quartile(v, 0.5, false), q3: quartile(v, 0.75, exclusive)}
	sum := 0.0
	for _, x := range v {
		sum += x
	}
	s.mean = sum / float64(len(v))
	iqr := s.q3 - s.q1
	lo, hi := s.q1-float64(1.5*iqr), s.q3+float64(1.5*iqr)
	s.low, s.high = math.Inf(1), math.Inf(-1)
	for _, x := range v {
		if x < lo || x > hi {
			s.outliers = append(s.outliers, x)
			continue
		}
		s.inner = append(s.inner, x)
		s.low, s.high = math.Min(s.low, x), math.Max(s.high, x)
	}
	if math.IsInf(s.low, 0) {
		s.low, s.high = s.q1, s.q3
	}
	return s
}

// boxed returns c with each box and whisker series replaced by a copy
// whose categories are its distinct categories, in the order they first
// appear, and whose values are their medians, its samples kept apart:
// the values of each category, sorted. c itself returns when no series
// draws boxes, so c stays read-only. A series without categories is one
// category of all its values.
func boxed(c *Chart) *Chart {
	has := false
	for _, g := range c.Groups {
		has = has || g.Kind == BoxWhisker
	}
	if !has {
		return c
	}
	cc := *c
	cc.Groups = make([]*Group, len(c.Groups))
	rd := &renderer{c: c}
	for i, g := range c.Groups {
		if g.Kind != BoxWhisker {
			cc.Groups[i] = g
			continue
		}
		gg := *g
		gg.Series = make([]*Series, len(g.Series))
		for j, s := range g.Series {
			ss := *s
			cats, vals := rd.data(s.Categories), rd.data(s.Values)
			index := map[string]int{}
			ss.Categories, ss.Values = Data{Cached: true}, Data{Cached: true, Format: vals.Format}
			ss.samples = nil
			for k := range min(vals.Len(), MaxPoints) {
				v, ok := vals.number(k)
				if !ok {
					continue
				}
				key := ""
				if cats.Len() > 0 {
					key = cats.label(k, "")
				}
				at, seen := index[key]
				if !seen {
					if len(ss.samples) >= maxBoxCategories {
						continue
					}
					at = len(ss.samples)
					index[key] = at
					ss.samples = append(ss.samples, nil)
					ss.Categories.Values = append(ss.Categories.Values, Str(key))
				}
				ss.samples[at] = append(ss.samples[at], v)
			}
			for k := range ss.samples {
				slices.Sort(ss.samples[k])
				ss.Values.Values = append(ss.Values.Values, Num(quartile(ss.samples[k], 0.5, false)))
			}
			if cats.Len() == 0 {
				ss.Categories = Data{}
			}
			gg.Series[j] = &ss
		}
		cc.Groups[i] = &gg
	}
	return &cc
}

// boxRange returns the least and greatest values of g's samples.
func boxRange(g *Group) (float64, float64) {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, s := range g.Series {
		for _, v := range s.samples {
			if len(v) > 0 {
				lo, hi = math.Min(lo, v[0]), math.Max(hi, v[len(v)-1])
			}
		}
	}
	return lo, hi
}

// boxes draws a box and whisker group: per category, a box from the low
// to the high quartile for each series side by side, the median and
// quartile lines across it, whiskers with caps to the farthest values
// within the fences, the mean's cross, the outliers and inner points as
// markers, and the line through the means.
func (rd *renderer) boxes(g *Group, x, y *dim) {
	k := len(g.Series)
	if k == 0 {
		return
	}
	gap := math.Max(0, math.Min(g.GapWidth, 500)) / 100
	catW := x.length() / float64(max(x.n, 1))
	w := catW / float64((1+gap)*(float64(k)+float64(float64(k-1)*boxGapShare)))
	for i, s := range g.Series {
		if rd.ctx.Err() != nil {
			return
		}
		b := s.Box
		if b == nil {
			b = &Box{}
		}
		half := float64(w / 2)
		span := float64(float64(k)*w) + float64(float64(k-1)*float64(w*boxGapShare))
		offset := float64(float64(i)*float64(w*(1+boxGapShare))) - float64(span/2) + half
		var means []typeset.Point
		for j, v := range s.samples {
			if j >= x.n || len(v) == 0 {
				continue
			}
			st := statsOf(v, b.Exclusive)
			c := x.valuePixel(float64(j)) + offset
			at := func(v float64) typeset.Point {
				p := y.valuePixel(y.sc.clamp(v))
				if x.vertical {
					return typeset.Point{X: p, Y: c}
				}
				return typeset.Point{X: c, Y: p}
			}
			box := rect(at(st.q1), at(st.q3), x.vertical, c-half, c+half)
			rd.paint(rectPath(box).Ops, rd.fillOf(g, s, j), rd.borderOf(g, s, j))
			across := func(v, half float64) {
				p := y.valuePixel(y.sc.clamp(v))
				rd.add(segment(x.vertical, p, c-half, c+half, b.Line))
			}
			along := func(v0, v1 float64) {
				p0, p1 := y.valuePixel(y.sc.clamp(v0)), y.valuePixel(y.sc.clamp(v1))
				rd.add(segment(!x.vertical, c, p0, p1, b.Line))
			}
			along(st.q3, st.high)
			along(st.q1, st.low)
			across(st.high, float64(boxCapShare*w)/2)
			across(st.low, float64(boxCapShare*w)/2)
			across(st.q1, w/2)
			across(st.q3, w/2)
			across(st.median, w/2)
			mp := at(st.mean)
			if b.Mean {
				h := boxMeanSize / 2
				rd.add(line(mp.X-h, mp.Y+h, mp.X+h, mp.Y-h, b.Line), line(mp.X-h, mp.Y-h, mp.X+h, mp.Y+h, b.Line))
			}
			means = append(means, mp)
			m := rd.markerOf(g, s, j)
			if b.Outliers {
				for _, o := range st.outliers {
					p := at(o)
					rd.marker(m, p.X, p.Y)
				}
			}
			if b.Inner {
				for _, o := range st.inner {
					p := at(o)
					rd.marker(m, p.X, p.Y)
				}
			}
		}
		if b.MeanLine && len(means) > 1 && b.Line != nil {
			rd.add(&typeset.Path{Ops: polyline(means, false), Stroke: b.Line.compose()})
		}
	}
}

// rect returns the rectangle between positions a and b across a category
// and from p0 to p1 along the values, turned when the categories run up
// the page.
func rect(p0, p1 typeset.Point, vertical bool, a, b float64) typeset.Rect {
	if vertical {
		return typeset.Rect{X: math.Min(p0.X, p1.X), Y: math.Min(a, b), Width: math.Abs(p1.X - p0.X), Height: math.Abs(b - a)}
	}
	return typeset.Rect{X: math.Min(a, b), Y: math.Min(p0.Y, p1.Y), Width: math.Abs(b - a), Height: math.Abs(p1.Y - p0.Y)}
}

// segment returns the line at p along one direction from a to b in the
// other: horizontal at y = p when across is false, vertical at x = p
// otherwise.
func segment(across bool, p, a, b float64, s *Stroke) *typeset.Path {
	if across {
		return line(p, a, p, b, s)
	}
	return line(a, p, b, p, s)
}
