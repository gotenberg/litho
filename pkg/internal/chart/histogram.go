package chart

import (
	"math"
	"sort"
	"strconv"
)

// binned returns c with its histogram series counted into bins: each
// series with [Series.Bins] replaced by a copy whose categories are the
// bins' ranges and whose values are their counts. It returns c itself
// when no series asks for bins, so c stays read-only.
func binned(c *Chart) *Chart {
	any := false
	for _, g := range c.Groups {
		for _, s := range g.Series {
			any = any || s.Bins != nil
		}
	}
	if !any {
		return c
	}
	cc := *c
	cc.Groups = make([]*Group, len(c.Groups))
	rd := &renderer{c: c}
	for i, g := range c.Groups {
		gg := *g
		gg.Series = make([]*Series, len(g.Series))
		for j, s := range g.Series {
			if s.Bins == nil {
				gg.Series[j] = s
				continue
			}
			ss := *s
			var vals []float64
			d := rd.data(s.Values)
			for k := range min(d.Len(), MaxPoints) {
				if v, ok := d.number(k); ok {
					vals = append(vals, v)
				}
			}
			labels, counts := histogram(vals, *s.Bins)
			ss.Categories = Data{Cached: true}
			ss.Values = Data{Cached: true}
			for k := range labels {
				ss.Categories.Values = append(ss.Categories.Values, Str(labels[k]))
				ss.Values.Values = append(ss.Values.Values, Num(counts[k]))
			}
			ss.Bins = nil
			gg.Series[j] = &ss
		}
		cc.Groups[i] = &gg
	}
	return &cc
}

// histogram counts vals into bins as LibreOffice's histogram chart does
// (chart2's HistogramCalculator): from the least value, bins of the fixed
// width, of the width that makes the fixed count, or of Scott's rule's
// width; the first bin closed on both sides, the others open on the left;
// the last one stretched to the greatest value. One value, or values all
// equal, make one bin from the value's floor to the ceiling of one more.
// Each bin's label is its range, "[a-b]" for the first and "(a-b]" for the
// others, its bounds rounded to about three significant figures of the
// first bin's width (chart2's HistogramDataSequence).
func histogram(vals []float64, b Binning) ([]string, []float64) {
	if len(vals) == 0 {
		return nil, nil
	}
	lo, hi, sum, sq := vals[0], vals[0], 0.0, 0.0
	for _, v := range vals {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
		sum += v
		sq += v * v
	}
	n := float64(len(vals))
	type bin struct{ a, b float64 }
	var bins []bin
	if len(vals) < 2 || lo == hi {
		bins = []bin{{math.Floor(lo), math.Ceil(lo + 1)}}
	} else {
		var width, count float64
		switch {
		case b.Width > 0:
			width = b.Width
			count = math.Ceil((hi - lo) / width)
		case b.Count > 0:
			count = float64(b.Count)
			width = (hi - lo) / count
		default:
			mean := sum / n
			sd := math.Sqrt(math.Max(0, (sq-sum*mean)/(n-1)))
			width = 3.5 * sd / math.Cbrt(n)
			count = math.Ceil((hi - lo) / width)
		}
		if !atLeast(count, 1) || math.IsInf(count, 0) {
			count = 1
		}
		if !positive(width) {
			width = hi - lo
		}
		count = math.Min(count, maxBins)
		start := lo
		for range int(count) { // fits int: at most maxBins
			bins = append(bins, bin{start, start + width})
			start += width
		}
		bins[0].a = lo
		bins[len(bins)-1].b = math.Max(bins[len(bins)-1].b, hi)
	}
	counts := make([]float64, len(bins))
	for _, v := range vals {
		// The bins are contiguous and ascending: the first whose upper
		// bound holds v is v's, when v passes its lower bound.
		i := sort.Search(len(bins), func(i int) bool { return v <= bins[i].b })
		if i < len(bins) && (v > bins[i].a || i == 0 && v >= bins[i].a) {
			counts[i]++
		}
	}
	digits := 2
	if w := bins[0].b - bins[0].a; w > 0 {
		digits = max(1, 2-int(math.Floor(math.Log10(w))))
	}
	scale := math.Pow(10, float64(digits))
	bound := func(v float64) string {
		return strconv.FormatFloat(math.Round(v*scale)/scale, 'f', -1, 64)
	}
	labels := make([]string, len(bins))
	for i, bn := range bins {
		open := "("
		if i == 0 {
			open = "["
		}
		labels[i] = open + bound(bn.a) + "-" + bound(bn.b) + "]"
	}
	return labels, counts
}

// maxBins caps the bins of a histogram. Anti-DoS guard, not spec-mandated:
// a bin is a bar and a label, and a histogram of more bars than a page has
// points is unreadable; it matches the points a series keeps.
const maxBins = MaxPoints
