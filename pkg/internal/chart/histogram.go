package chart

import (
	"math"
	"sort"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// binned returns c with its histogram series counted into bins: each
// series with [Series.Bins] replaced by a copy whose categories are the
// bins' ranges and whose values are their counts. It returns c itself
// when no series asks for bins, so c stays read-only.
func binned(c *Chart) *Chart {
	has := false
	for _, g := range c.Groups {
		for _, s := range g.Series {
			has = has || s.Bins != nil
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
			if c.Office2016 {
				labels, counts = officeHistogram(vals, *s.Bins)
			}
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
		sq += float64(v * v)
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
			sd := math.Sqrt(math.Max(0, (sq-float64(sum*mean))/(n-1)))
			width = 3.5 * sd / detmath.Cbrt(n)
			count = math.Ceil((hi - lo) / width)
		}
		if !atLeast(count, 1) || math.IsInf(count, 0) {
			count = 1
		}
		if !numeric.Positive(width) {
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
		digits = max(1, 2-int(math.Floor(detmath.Log10(w))))
	}
	scale := detmath.Pow(10, float64(digits))
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

// officeHistogram counts vals into bins as Office's histogram does: from
// the underflow bound, else the least value, to the overflow bound, else
// the greatest, bins of the fixed width, of the width that makes the
// fixed count, or of Scott's rule's width, as many as cover the range,
// each closed on its right, or its left, the first, or the last, closed
// on both sides; the values past the underflow and overflow bounds in a
// bin of their own at each end. A bin's label is its interval, "[a, b]",
// "(a, b]" or "[a, b)", an underflow bin's "< u" or "≤ u", an overflow
// bin's "> o" or "≥ o", the bounds rounded as [histogram] rounds them.
//
// # Corpus evidence
//
//   - litho/docx: Word labels charts-chartex's bins of width 2 over 1 to
//     13 "[1, 3]", "(3, 5]" to "(11, 13]".
//   - litho/pptx: PowerPoint labels chartex's five bins over 3 to 44
//     "[3, 11.2]" to "(35.8, 44]", and its bins of width 10 closed on the
//     left with an underflow at 5 "< 5", "[5, 15)", "[15, 25)",
//     "[25, 35)", "[35, 45]", counting 1, 5, 7, 4 and 3 of its 20 values.
func officeHistogram(vals []float64, b Binning) ([]string, []float64) {
	if len(vals) == 0 {
		return nil, nil
	}
	lo, hi, sum, sq := vals[0], vals[0], 0.0, 0.0
	for _, v := range vals {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
		sum += v
		sq += float64(v * v)
	}
	start, end := lo, hi
	if b.HasUnderflow && numeric.Finite(b.Underflow) {
		start = b.Underflow
	}
	if b.HasOverflow && numeric.Finite(b.Overflow) {
		end = b.Overflow
	}
	if end < start {
		end = start
	}
	n := float64(len(vals))
	width := 0.0
	switch {
	case b.Width > 0:
		width = b.Width
	case b.Count > 0:
		width = (end - start) / float64(b.Count)
	case len(vals) > 1:
		mean := sum / n
		sd := math.Sqrt(math.Max(0, (sq-float64(sum*mean))/(n-1)))
		width = 3.5 * sd / detmath.Cbrt(n)
	}
	count := 1.0
	if numeric.Positive(width) && end > start {
		count = math.Ceil((end - start) / width)
	}
	if !numeric.Positive(width) {
		width = math.Max(end-start, 1)
	}
	count = math.Max(1, math.Min(count, maxBins))
	k := int(count) // fits int: at most maxBins
	digits := max(1, 2-int(math.Floor(detmath.Log10(width))))
	scale := detmath.Pow(10, float64(digits))
	bound := func(v float64) string {
		return strconv.FormatFloat(math.Round(v*scale)/scale, 'f', -1, 64)
	}
	var labels []string
	var counts []float64
	under, over := b.HasUnderflow && numeric.Finite(b.Underflow), b.HasOverflow && numeric.Finite(b.Overflow)
	if under {
		mark := "≤ "
		if b.ClosedLeft {
			mark = "< "
		}
		labels, counts = append(labels, mark+bound(start)), append(counts, 0)
	}
	first := len(labels)
	for i := range k {
		a, z := start+float64(float64(i)*width), start+float64(float64(i+1)*width)
		open, shut := "(", "]"
		if b.ClosedLeft {
			open, shut = "[", ")"
		}
		if !b.ClosedLeft && i == 0 {
			open = "["
		}
		if b.ClosedLeft && i == k-1 {
			shut = "]"
		}
		labels, counts = append(labels, open+bound(a)+", "+bound(z)+shut), append(counts, 0)
	}
	if over {
		mark := "> "
		if b.ClosedLeft {
			mark = "≥ "
		}
		labels, counts = append(labels, mark+bound(end)), append(counts, 0)
	}
	for _, v := range vals {
		switch {
		case under && (v < start || !b.ClosedLeft && v == start):
			counts[0]++
		case over && (v > end || b.ClosedLeft && v == end):
			counts[len(counts)-1]++
		default:
			i := (v - start) / width
			j := int(math.Floor(math.Max(0, math.Min(i, count)))) // fits int: at most maxBins
			if !b.ClosedLeft && i == math.Floor(i) && j > 0 {
				j--
			}
			counts[first+min(j, k-1)]++
		}
	}
	return labels, counts
}
