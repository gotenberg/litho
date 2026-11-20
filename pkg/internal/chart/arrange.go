package chart

import (
	"math"
	"sort"
)

// Arrangement is how a series' points are gathered and ordered before they
// are drawn (MS-ODRAWXML, 2.24.3.2 CT_Aggregation; 2.24.4.21
// ST_SeriesLayout paretoLine).
type Arrangement struct {
	// Aggregate sums the values of the points of one category into one
	// point, the categories in the order they first appear.
	Aggregate bool
	// Descending orders the points by value, the greatest first, points
	// of equal value in their order.
	Descending bool
	// Cumulative replaces each point's value, once ordered, by the share
	// of the series' total its value and those before it reach, from 0
	// to 1: a Pareto line.
	Cumulative bool
}

// arranged returns c with the series that ask for an [Arrangement]
// replaced by arranged copies, c itself when none asks, so c stays
// read-only.
func arranged(c *Chart) *Chart {
	has := false
	for _, g := range c.Groups {
		for _, s := range g.Series {
			has = has || s.Arrange != (Arrangement{})
		}
	}
	if !has {
		return c
	}
	cc := *c
	cc.Groups = make([]*Group, len(c.Groups))
	rd := &renderer{c: c}
	for i, g := range c.Groups {
		gg := *g
		gg.Series = make([]*Series, len(g.Series))
		for j, s := range g.Series {
			if s.Arrange == (Arrangement{}) {
				gg.Series[j] = s
				continue
			}
			ss := *s
			ss.Categories, ss.Values = arrange(rd.data(s.Categories), rd.data(s.Values), s.Arrange)
			ss.Arrange = Arrangement{}
			gg.Series[j] = &ss
		}
		cc.Groups[i] = &gg
	}
	return &cc
}

// arrange returns the categories and values of a series arranged as a
// says. A point without a number counts as 0.
func arrange(cats, vals Data, a Arrangement) (Data, Data) {
	n := min(max(cats.Len(), vals.Len()), MaxPoints)
	type point struct {
		cat Value
		v   float64
	}
	pts := make([]point, 0, n)
	seen := map[string]int{}
	for i := range n {
		v, ok := vals.number(i)
		if !ok {
			v = 0
		}
		var cat Value
		if i < cats.Len() {
			cat = cats.Values[i]
		} else {
			cat = Num(float64(i + 1))
		}
		if a.Aggregate {
			key := cats.label(i, "")
			if k, dup := seen[key]; dup && i < cats.Len() {
				pts[k].v += v
				continue
			}
			seen[key] = len(pts)
		}
		pts = append(pts, point{cat, v})
	}
	if a.Descending {
		sort.SliceStable(pts, func(i, j int) bool { return pts[i].v > pts[j].v })
	}
	if a.Cumulative {
		total := 0.0
		for _, p := range pts {
			total += p.v
		}
		run := 0.0
		for i := range pts {
			run += pts[i].v
			pts[i].v = 0
			if total != 0 && !math.IsInf(total, 0) {
				pts[i].v = run / total
			}
		}
	}
	oc, ov := Data{Cached: true, Format: cats.Format}, Data{Cached: true, Format: vals.Format}
	if a.Cumulative {
		ov.Format = ""
	}
	for _, p := range pts {
		oc.Values = append(oc.Values, p.cat)
		ov.Values = append(ov.Values, Num(p.v))
	}
	return oc, ov
}
