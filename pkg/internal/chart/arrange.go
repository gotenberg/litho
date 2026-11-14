package chart

import (
	"github.com/gotenberg/litho/pkg/internal/numfmt"
)

// Arrangement is how a series' points are gathered before they are drawn
// (MS-ODRAWXML, 2.24.3.2 CT_Aggregation).
type Arrangement struct {
	// Aggregate sums the values of the points of one category into one
	// point, the categories in the order they first appear.
	Aggregate bool
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
			key := cats.label(i, numfmt.Locale{})
			if k, dup := seen[key]; dup && i < cats.Len() {
				pts[k].v += v
				continue
			}
			seen[key] = len(pts)
		}
		pts = append(pts, point{cat, v})
	}
	oc, ov := Data{Cached: true, Format: cats.Format}, Data{Cached: true, Format: vals.Format}
	for _, p := range pts {
		oc.Values = append(oc.Values, p.cat)
		ov.Values = append(ov.Values, Num(p.v))
	}
	return oc, ov
}
